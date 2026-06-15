package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

// ====== 调度器 =====
//
// 按三个因素加权路由 LLM 请求：
//   - 延迟（latency_ms）：越低权重越高
//   - 负载（load_pct）：越低权重越高
//   - 是否拥有目标模型：有 → 权重 × 10；无 → 权重 = 0
//
// 策略：加权随机（Weighted Random），对 top-N 候选随机选一个
// 对失败节点做熔断（10秒内不重试）

const (
	circuitBreakDur = 10 * time.Second
	maxTopPeers     = 3
	minWeight       = 1
)

// Scheduler 调度器
type Scheduler struct {
	transport *Transport
	mu        sync.RWMutex
	peers     map[string]*NodePeer // 全局已知 peer（由 manager 填）
	failed    map[string]time.Time  // 失败节点 → 下一次可重试时间
}

// NewScheduler 创建调度器
func NewScheduler(tr *Transport) *Scheduler {
	return &Scheduler{
		transport: tr,
		peers:     make(map[string]*NodePeer),
		failed:    make(map[string]time.Time),
	}
}

// UpdatePeers 更新节点列表（manager 调用）
func (s *Scheduler) UpdatePeers(peers []NodePeer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range peers {
		cp := p
		s.peers[p.ID] = &cp
	}
}

// MarkFailed 标记节点失败（触发熔断）
func (s *Scheduler) MarkFailed(peerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failed[peerID] = time.Now().Add(circuitBreakDur)
}

// PickBestNode 选择最佳节点来处理 model 类型请求
func (s *Scheduler) PickBestNode(model string) (*NodePeer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scoreEntry struct {
		peer  *NodePeer
		score float64
	}
	entries := make([]scoreEntry, 0, len(s.peers))
	now := time.Now()

	for _, p := range s.peers {
		// 跳过离线与熔断
		if !p.Online {
			continue
		}
		if failAt, ok := s.failed[p.ID]; ok && now.Before(failAt) {
			continue
		}
		// 是否有目标模型
		hasModel := false
		if model != "" {
			for _, m := range p.Models {
				if m == model {
					hasModel = true
					break
				}
			}
		}
		// 权重计算：
		// score = (1000 - latency) × (100 - load) × modelBoost
		latency := p.LatencyMs
		if latency <= 0 {
			latency = 100 // 未知延迟给 100ms 悲观值
		}
		if latency >= 1000 {
			latency = 999
		}
		load := p.LoadPct
		if load < 0 || load > 100 {
			load = 50
		}
		score := float64(1000-latency) * float64(100-load)
		if hasModel || model == "" {
			score *= 10
		} else {
			// 无目标模型，但仍可选作为通用候选（权重较低）
			score *= 1
		}
		if p.HasGPU {
			score *= 2
		}
		score += float64(minWeight)
		entries = append(entries, scoreEntry{peer: p, score: score})
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no available peer nodes")
	}

	// 取 top-N 中随机一个（避免永远命中同一节点）
	// 按分数排序，取前 maxTopPeers，然后加权随机
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].score > entries[i].score {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}
	top := entries
	if len(top) > maxTopPeers {
		top = top[:maxTopPeers]
	}
	var total float64
	for _, e := range top {
		total += e.score
	}
	r := rand.Float64() * total
	var acc float64
	for _, e := range top {
		acc += e.score
		if r <= acc {
			return e.peer, nil
		}
	}
	return top[0].peer, nil
}

// Chat 对远端节点发起 LLM 聊天请求（路由+传输+响应）
// model: 请求的模型名；如果目标节点没有该模型，会返回错误
// providers 接口留给上层替换：调度器只负责选节点与转发
func (s *Scheduler) Chat(ctx context.Context, req ChatRequest, timeout time.Duration) (*ChatResponse, error) {
	peer, err := s.PickBestNode(req.Model)
	if err != nil {
		return nil, err
	}

	// 构造消息
	payload, _ := json.Marshal(req)
	msg := &Message{
		Type:    MsgChatRequest,
		To:      peer.ID,
		Payload: payload,
	}
	// 发起请求；在等待时间内测量延迟
	start := time.Now()
	resp, err := s.transport.SendWithResponse(peer.Addr, msg, timeout)
	latency := int(time.Since(start).Milliseconds())
	s.mu.Lock()
	if p, ok := s.peers[peer.ID]; ok {
		// EWMA 平滑延迟
		if p.LatencyMs == 0 {
			p.LatencyMs = latency
		} else {
			p.LatencyMs = (p.LatencyMs*3 + latency) / 4
		}
	}
	s.mu.Unlock()

	if err != nil {
		s.MarkFailed(peer.ID)
		return nil, fmt.Errorf("peer %s: %w", peer.ID, err)
	}

	var cr ChatResponse
	if len(resp.Payload) > 0 {
		_ = json.Unmarshal(resp.Payload, &cr)
	}
	if cr.Error != "" {
		return nil, fmt.Errorf("peer %s: %s", peer.ID, cr.Error)
	}
	return &cr, nil
}

// ListPeerModels 列出某节点的模型
func (s *Scheduler) ListPeerModels(ctx context.Context, peerID string) ([]string, error) {
	s.mu.RLock()
	p, ok := s.peers[peerID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("peer %s not found", peerID)
	}
	msg := &Message{Type: MsgModelListRequest, To: peerID}
	resp, err := s.transport.SendWithResponse(p.Addr, msg, 5*time.Second)
	if err != nil {
		s.MarkFailed(peerID)
		return nil, err
	}
	var models []string
	_ = json.Unmarshal(resp.Payload, &models)
	return models, nil
}
