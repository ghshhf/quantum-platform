package p2p

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ===== PRP 路由表（极简距离向量） =====
//
// 设计要点：
//   - 路由表 = map[targetNodeID] -> RouteEntry (nextHop, distance)
//   - 每个节点周期性地把自己可达的 targets 发给所有直连邻居（MsgRouteAdvertise）
//   - 邻居收到后：对每个 entry，若 "entry.distance + 1 < 我当前到 entry.target 的 distance"，
//     则把下一跳设为该邻居，更新跳数 = entry.distance + 1
//   - 每过 routeExpire 若没有更新，则删除该条目（避免无效路由）
//   - 到"我自己"的 distance = 0，始终存在
//
// 典型成长曲线：
//   t=0: 只有自己（routes=1）
//   t=5s: 局域网内 peer 互相宣告 → 每个节点有 N 条距离=1 的邻居
//   t=15s: 个人热点节点（跨 2 个子网）把两边的 peer 名单相互中继
//          → 距离=2 的条目开始出现，节点总数快速增长
//   t=30s: 网状覆盖，每个节点可路由到全网 ~80% 节点，彻底脱离公网。

const (
	defaultMaxDistance = 8   // 最大跳数（与 TTL 默认值一致）
	routeExpire        = 30 * time.Second // 路由条目存活时间
	advertiseInterval  = 5  * time.Second // 路由广播周期
)

// Router PRP 路由表
type Router struct {
	selfID   string
	selfAddr string
	mu       sync.RWMutex
	routes   map[string]RouteEntry // targetID -> entry
}

// NewRouter 创建路由表，并把"到自己"的路由放进去（distance=0）
func NewRouter(selfID, selfAddr string) *Router {
	r := &Router{
		selfID:   selfID,
		selfAddr: selfAddr,
		routes:   make(map[string]RouteEntry),
	}
	r.routes[selfID] = RouteEntry{
		Target:   selfID,
		NextHop:  selfAddr,
		Distance: 0,
		Updated:  time.Now().Unix(),
	}
	return r
}

// AddDirect 把一个"直连邻居"加入路由表（distance=1）
func (r *Router) AddDirect(peerID, peerAddr string) {
	if peerID == "" || peerAddr == "" {
		return
	}
	if peerID == r.selfID {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.routes[peerID]
	if !ok || existing.Distance > 1 {
		r.routes[peerID] = RouteEntry{
			Target:   peerID,
			NextHop:  peerAddr,
			Distance: 1,
			Updated:  time.Now().Unix(),
		}
	} else {
		// 无论如何，刷新 Updated 时间（直连节点理应最稳）
		existing.NextHop = peerAddr
		existing.Updated = time.Now().Unix()
		r.routes[peerID] = existing
	}
}

// ApplyAdvertise 从邻居处接收一条路由广播，按距离向量规则更新路由表
// advertiserAddr: 广播者的 TCP 地址；advertiserID: 广播者 NodeID
func (r *Router) ApplyAdvertise(advertiserID, advertiserAddr string, adv RouteAdvertise) int {
	if advertiserID == r.selfID {
		return 0
	}
	now := time.Now().Unix()
	updated := 0
	r.mu.Lock()
	for _, e := range adv.Entries {
		if e.Target == r.selfID {
			continue // 不维护到自己的转发路径
		}
		candidate := e.Distance + 1
		if candidate > defaultMaxDistance {
			continue // 超过最大跳数，丢弃
		}
		cur, ok := r.routes[e.Target]
		if !ok || candidate < cur.Distance {
			r.routes[e.Target] = RouteEntry{
				Target:   e.Target,
				NextHop:  advertiserAddr,
				Distance: candidate,
				Updated:  now,
			}
			updated++
		}
	}
	r.mu.Unlock()
	return updated
}

// NextHop 查询到 target 的下一跳 TCP 地址；distance=0 表示"是我自己"
// 若找不到返回空串
func (r *Router) NextHop(target string) (string, int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.routes[target]
	if !ok {
		return "", -1
	}
	return e.NextHop, e.Distance
}

// Snapshot 返回当前路由表快照（按 distance 排序），用于调试/UI
func (r *Router) Snapshot() []RouteEntry {
	r.mu.RLock()
	out := make([]RouteEntry, 0, len(r.routes))
	for _, e := range r.routes {
		out = append(out, e)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance < out[j].Distance
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// BuildAdvertise 生成发给邻居的路由广播：我可达的所有 targets（含到自己 distance=1）
// 对邻居而言，到这些 targets 的 distance 将是 "我的 distance + 1"
func (r *Router) BuildAdvertise() RouteAdvertise {
	r.mu.RLock()
	entries := make([]RouteEntry, 0, len(r.routes))
	for _, e := range r.routes {
		// 发给邻居时，邻居会 +1；对我自己这个条目 distance=0 → 邻居看到 1
		entries = append(entries, RouteEntry{
			Target:   e.Target,
			NextHop:  r.selfAddr,
			Distance: e.Distance,
			Updated:  time.Now().Unix(),
		})
	}
	r.mu.RUnlock()
	return RouteAdvertise{
		From:     r.selfID,
		FromAddr: r.selfAddr,
		Entries:  entries,
	}
}

// CleanupExpired 周期性清理过期路由条目（distance=0 的自己永远保留）
func (r *Router) CleanupExpired() int {
	now := time.Now().Unix()
	cutoff := int64(routeExpire / time.Second)
	r.mu.Lock()
	deleted := 0
	for id, e := range r.routes {
		if e.Distance == 0 {
			continue
		}
		if now-e.Updated > cutoff {
			delete(r.routes, id)
			deleted++
		}
	}
	r.mu.Unlock()
	return deleted
}

// ===== PEX 候选节点集合 =====
//
// 设计很简单：收到 PeerExchange 后，把其中 (id, addr) 对存进一个候选集合
// Node 会周期性地对"新出现的、未尝试过的 candidate"做一次主动探测（MsgPing）
// 探测成功 → 加入路由表（AddDirect），并进入 Discovery 的 peer 列表

// PexSet 候选节点集合
type PexSet struct {
	selfID string
	mu     sync.RWMutex
	cands  map[string]PeerExchangeItem // id -> item
	tried  map[string]bool             // 已尝试探测过的节点
}

// NewPexSet 创建 PEX 候选集合
func NewPexSet(selfID string) *PexSet {
	return &PexSet{
		selfID: selfID,
		cands:  make(map[string]PeerExchangeItem),
		tried:  make(map[string]bool),
	}
}

// Ingest 吃入一条 PeerExchange 消息，返回新增了多少个候选
func (p *PexSet) Ingest(pe *PeerExchange) int {
	if pe == nil || pe.From == p.selfID {
		return 0
	}
	added := 0
	p.mu.Lock()
	for _, item := range pe.Peers {
		if item.ID == p.selfID || item.ID == "" || item.Addr == "" {
			continue
		}
		if _, exists := p.cands[item.ID]; !exists {
			p.cands[item.ID] = item
			added++
		}
	}
	p.mu.Unlock()
	return added
}

// AddDirectPeer 把一个已知直连节点加入 PEX（用于组装对外广播）
func (p *PexSet) AddDirectPeer(id, addr, name string) {
	if id == p.selfID || id == "" || addr == "" {
		return
	}
	p.mu.Lock()
	p.cands[id] = PeerExchangeItem{ID: id, Addr: addr, Name: name}
	p.mu.Unlock()
}

// BuildExchange 组装对外的 PeerExchange 消息（最多 N 条，按加入顺序）
func (p *PexSet) BuildExchange(selfID string, maxItems int) PeerExchange {
	p.mu.RLock()
	items := make([]PeerExchangeItem, 0, len(p.cands))
	for _, it := range p.cands {
		items = append(items, it)
	}
	p.mu.RUnlock()
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	return PeerExchange{From: selfID, Peers: items}
}

// PickNewCandidates 取最多 N 个尚未尝试过的候选，用于主动探测
// 返回后会在 tried 中标记为已尝试
func (p *PexSet) PickNewCandidates(n int) []PeerExchangeItem {
	p.mu.Lock()
	out := make([]PeerExchangeItem, 0, n)
	for id, it := range p.cands {
		if p.tried[id] {
			continue
		}
		out = append(out, it)
		p.tried[id] = true
		if len(out) >= n {
			break
		}
	}
	p.mu.Unlock()
	return out
}

// MarkTried 把某节点标记为已尝试（供外部调用）
func (p *PexSet) MarkTried(id string) {
	p.mu.Lock()
	p.tried[id] = true
	p.mu.Unlock()
}

// Size 返回候选集合大小
func (p *PexSet) Size() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.cands)
}

// ===== 工具函数 =====

// MarshalPRP 把 PRP 相关结构体编码成 Message.Payload
func MarshalPRP(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// UnmarshalPRP 从 Message.Payload 解析 PRP 结构体
func UnmarshalPRP[T any](payload []byte) (*T, error) {
	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("unmarshal PRP payload: %w", err)
	}
	return &out, nil
}
