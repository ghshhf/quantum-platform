package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// ====== 节点发现 =====
// 使用 UDP multicast (224.0.0.251:60101) 做局域网自动发现
// 每个节点每秒广播一次 DiscoveryAnnounce
// 收到其他节点的广播后自动加入 Peer 列表

const (
	multicastAddr = "224.0.0.251:60101"
	discoveryInterval = 2 * time.Second
	peerTimeout       = 10 * time.Second
	maxUDPSize     = 8192
)

// Discovery 节点发现器
type Discovery struct {
	selfID    string
	announce  DiscoveryAnnounce
	conn      *net.UDPConn
	addr      *net.UDPAddr
	mu        sync.Mutex
	peers     map[string]*NodePeer
	onNewPeer func(*NodePeer)
	ctx       context.Context
	cancel    context.CancelFunc
	started   bool
}

// NewDiscovery 创建节点发现器
func NewDiscovery(selfID, name, tcpAddr string, models []string) *Discovery {
	return &Discovery{
		selfID: selfID,
		announce: DiscoveryAnnounce{
			NodeID:  selfID,
			Name:    name,
			Addr:    tcpAddr,
			Version: "v1",
			Models:  models,
		},
		peers: make(map[string]*NodePeer),
	}
}

// OnNewPeer 设置新节点发现回调
func (d *Discovery) OnNewPeer(fn func(*NodePeer)) {
	d.onNewPeer = fn
}

// UpdateAnnounce 动态更新宣告信息（模型/负载/热点模式变化时调用）
func (d *Discovery) UpdateAnnounce(loadPct int, isHotspot bool, models []string) {
	d.mu.Lock()
	d.announce.LoadPct = loadPct
	d.announce.IsHotspot = isHotspot
	d.announce.Models = models
	d.mu.Unlock()
}

// Start 启动节点发现（监听 + 广播循环
func (d *Discovery) Start(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", multicastAddr)
	if err != nil {
		return fmt.Errorf("resolve multicast: %w", err)
	}
	d.addr = addr

	// 加入 multicast 组（监听所有接口）
	iface, _ := net.InterfaceAddrs()
	_ = iface
	conn, err := net.ListenMulticastUDP("udp", nil, addr)
	if err != nil {
		// 回退到普通 UDP 监听（某些系统不支持 multicast）
		conn2, err2 := net.ListenUDP("udp", addr)
		if err2 != nil {
			return fmt.Errorf("listen udp: %w / fallback: %w", err, err2)
		}
		d.conn = conn2
	} else {
		d.conn = conn
	}

	d.ctx, d.cancel = context.WithCancel(ctx)
	d.started = true

	go d.recvLoop()
	go d.sendLoop()
	go d.cleanupLoop()
	return nil
}

// Stop 停止节点发现
func (d *Discovery) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	if d.conn != nil {
		d.conn.Close()
	}
}

// Peers 返回当前已知节点快照
func (d *Discovery) Peers() []NodePeer {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]NodePeer, 0, len(d.peers))
	for _, p := range d.peers {
		out = append(out, *p)
	}
	return out
}

// AddManualPeer 手动添加一个 peer（跨网段时用）
func (d *Discovery) AddManualPeer(addr string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// 通过 TCP 探测一下再加入
	d.peers[addr] = &NodePeer{
		NodeInfo: NodeInfo{
			ID: addr,
			Name: "manual-" + addr,
			Addr: addr,
			Version: "v1",
		},
		Online:    true,
		FirstSeen: time.Now(),
		LastSeen:  time.Now(),
	}
}

// RemovePeer 移除一个节点
func (d *Discovery) RemovePeer(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.peers, id)
}

// ===== 内部实现 =====

func (d *Discovery) sendLoop() {
	ticker := time.NewTicker(discoveryInterval)
	defer ticker.Stop()
	// 先立即发一次
	d.broadcastAnnounce()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.broadcastAnnounce()
			ticker.Reset(discoveryInterval)
		}
	}
}

func (d *Discovery) broadcastAnnounce() {
	d.mu.Lock()
	ann := d.announce
	d.mu.Unlock()
	b, _ := json.Marshal(ann)
	if d.conn != nil && d.addr != nil {
		d.conn.WriteToUDP(b, d.addr)
	}
}

func (d *Discovery) recvLoop() {
	buf := make([]byte, maxUDPSize)
	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}
		d.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			// 超时或连接关闭
			continue
		}
		var ann DiscoveryAnnounce
		if err := json.Unmarshal(buf[:n], &ann); err != nil {
			continue
		}
		if ann.NodeID == d.selfID || ann.NodeID == "" {
			continue // 忽略自己或无效广播
		}
		d.mu.Lock()
		existing, ok := d.peers[ann.NodeID]
		now := time.Now()
		if !ok {
			// 新节点
			peer := &NodePeer{
				NodeInfo: NodeInfo{
					ID:        ann.NodeID,
					Name:      ann.Name,
					Addr:      ann.Addr,
					Version:   ann.Version,
					Models:    ann.Models,
					LoadPct:  ann.LoadPct,
					HasGPU:   ann.HasGPU,
					UpdatedAt: now,
				},
				Online:    true,
				FirstSeen: now,
				LastSeen:  now,
			}
			d.peers[ann.NodeID] = peer
			if d.onNewPeer != nil {
				go d.onNewPeer(peer)
			}
		} else {
			existing.Name = ann.Name
			existing.Addr = ann.Addr
			existing.Version = ann.Version
			existing.Models = ann.Models
			existing.LoadPct = ann.LoadPct
			existing.HasGPU = ann.HasGPU
			existing.UpdatedAt = now
			existing.LastSeen = now
			existing.Online = true
		}
		d.mu.Unlock()
	}
}

func (d *Discovery) cleanupLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-t.C:
			d.mu.Lock()
			now := time.Now()
			for id, p := range d.peers {
				if now.Sub(p.LastSeen) > peerTimeout {
					p.Online = false
					if now.Sub(p.LastSeen) > 2*peerTimeout {
						delete(d.peers, id)
					}
				}
			}
			d.mu.Unlock()
		}
	}
}
