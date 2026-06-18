// ─── 传输层心跳保持与自动重连 ─────────────────────────────────────────────────
//
// 参考 cloudflared connection/connection.go + retry/ 包的设计思路：
//   1. 定期发送 MsgPing 检测连接健康状态
//   2. 连接断开时自动重建，带指数退避重试
//   3. 配置化：心跳间隔、超时、退避参数可调
//
// 用法：
//   Keepalive := NewKeepalive(transport, KeepaliveOpts{
//       Interval: 10 * time.Second,
//       Timeout:  3 * time.Second,
//   })
//   keepalive.Start(ctx)
//   defer keepalive.Stop()

package p2p

import (
	"context"
	"log/slog"
	"math"
	"net"
	"sync"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// KeepaliveOpts 心跳与重连配置
// ─────────────────────────────────────────────────────────────────────────────

// KeepaliveOpts 心跳与重连配置
type KeepaliveOpts struct {
	// Interval 心跳间隔（默认 10s）
	Interval time.Duration

	// Timeout 单次 Ping 超时（默认 3s）
	Timeout time.Duration

	// MaxRetries 单连接最大重试次数（默认 3，-1=无限）
	MaxRetries int

	// BaseBackoff 初始退避时长（默认 1s）
	BaseBackoff time.Duration

	// MaxBackoff 最大退避时长（默认 30s）
	MaxBackoff time.Duration

	// OnPeerLost 回调：当某个 peer 连续重试失败后触发（可选）
	OnPeerLost func(peerAddr string, peerID string)
}

// DefaultKeepaliveOpts 默认配置
func DefaultKeepaliveOpts() KeepaliveOpts {
	return KeepaliveOpts{
		Interval:    10 * time.Second,
		Timeout:     3 * time.Second,
		MaxRetries:  3,
		BaseBackoff: 1 * time.Second,
		MaxBackoff:  30 * time.Second,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Keepalive 心跳管理器
// ─────────────────────────────────────────────────────────────────────────────

// Keepalive 为 Transport 添加心跳检测与自动重连能力。
type Keepalive struct {
	tr    *Transport
	opts  KeepaliveOpts
	log   *slog.Logger
	done  chan struct{}

	mu     sync.Mutex
	running bool
	// peerHealth 跟踪每个对端的健康状态
	peerHealth map[string]*peerHealthState
}

// peerHealthState 单个 peer 的健康与重试状态
type peerHealthState struct {
	addr       string
	id         string
	failures   int
	backoff    time.Duration
	lastPingOK bool
}

// NewKeepalive 为 Transport 创建一个心跳管理器。
// 注意：Keepalive 只管理 Transport 的出站连接池（outbound map）。
func NewKeepalive(tr *Transport, opts KeepaliveOpts) *Keepalive {
	if opts.Interval <= 0 {
		opts.Interval = 10 * time.Second
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Second
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = 1 * time.Second
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 30 * time.Second
	}

	return &Keepalive{
		tr:         tr,
		opts:       opts,
		log:        slog.Default().With("module", "p2p.keepalive"),
		done:       make(chan struct{}),
		peerHealth: make(map[string]*peerHealthState),
	}
}

// Start 启动心跳循环（阻塞式 goroutine，在独立 goroutine 中调用）
func (k *Keepalive) Start(ctx context.Context) {
	k.mu.Lock()
	if k.running {
		k.mu.Unlock()
		return
	}
	k.running = true
	k.mu.Unlock()

	k.log.Info("keepalive started",
		"interval", k.opts.Interval,
		"timeout", k.opts.Timeout,
		"max_retries", k.opts.MaxRetries)

	ticker := time.NewTicker(k.opts.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			k.stop()
			return
		case <-ticker.C:
			k.healthCheck(ctx)
		}
	}
}

// Stop 停止心跳循环
func (k *Keepalive) Stop() {
	k.stop()
}

func (k *Keepalive) stop() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.running {
		close(k.done)
		k.running = false
	}
	k.peerHealth = make(map[string]*peerHealthState)
}

// ─────────────────────────────────────────────────────────────────────────────
// 心跳检测
// ─────────────────────────────────────────────────────────────────────────────

func (k *Keepalive) healthCheck(ctx context.Context) {
	// 获取当前出站连接池快照
	k.tr.mu.Lock()
	addrs := make(map[string]string) // addr → id
	for id, p := range k.tr.outbound {
		addr := p.conn.RemoteAddr().String()
		addrs[addr] = id
	}
	k.tr.mu.Unlock()

	for addr, peerID := range addrs {
		k.pingPeer(ctx, addr, peerID)
	}
}

func (k *Keepalive) pingPeer(ctx context.Context, addr, peerID string) {
	state, ok := k.peerHealth[addr]
	if !ok {
		state = &peerHealthState{
			addr: addr,
			id:   peerID,
		}
		k.peerHealth[addr] = state
	}

	// 发送 MsgPing
	msg := &Message{
		Type: MsgPing,
		From: k.tr.self.ID,
		To:   peerID,
	}
	_, err := k.tr.SendWithResponse(addr, msg, k.opts.Timeout)
	if err == nil {
		// 心跳成功 → 重置失败计数
		state.failures = 0
		state.lastPingOK = true
		state.backoff = 0
		return
	}

	// 心跳失败
	state.failures++
	state.lastPingOK = false
	k.log.Warn("heartbeat failed",
		"peer", peerID,
		"addr", addr,
		"failures", state.failures,
		"error", err)

	// 判断是否达到最大重试次数
	maxRetries := k.opts.MaxRetries
	if maxRetries < 0 {
		maxRetries = math.MaxInt32
	}

	if state.failures >= maxRetries {
		k.log.Warn("peer lost, giving up",
			"peer", peerID,
			"addr", addr,
			"failures", state.failures)
		// 从连接池移除
		k.tr.mu.Lock()
		if p, ok := k.tr.outbound[peerID]; ok {
			p.conn.Close()
			delete(k.tr.outbound, peerID)
		}
		k.tr.mu.Unlock()
		delete(k.peerHealth, addr)

		// 回调通知
		if k.opts.OnPeerLost != nil {
			k.opts.OnPeerLost(addr, peerID)
		}
		return
	}

	// 未达上限 → 尝试重连（指数退避）
	go k.reconnectWithBackoff(ctx, addr, peerID, state)
}

// ─────────────────────────────────────────────────────────────────────────────
// 自动重连（指数退避）
// ─────────────────────────────────────────────────────────────────────────────

func (k *Keepalive) reconnectWithBackoff(ctx context.Context, addr, peerID string, state *peerHealthState) {
	// 计算退避时长
	if state.backoff <= 0 {
		state.backoff = k.opts.BaseBackoff
	} else {
		state.backoff = time.Duration(
			math.Min(
				float64(state.backoff*2),
				float64(k.opts.MaxBackoff),
			),
		)
	}

	k.log.Info("reconnecting",
		"peer", peerID,
		"addr", addr,
		"backoff", state.backoff)

	// 等待退避
	select {
	case <-ctx.Done():
		return
	case <-time.After(state.backoff):
	}

	// 尝试重连
	conn, err := k.dialWithTimeout(addr, k.opts.Timeout)
	if err != nil {
		k.log.Warn("reconnect failed",
			"peer", peerID,
			"addr", addr,
			"error", err,
			"next_backoff", state.backoff)
		return
	}

	// 重连成功 → 更新连接池
	k.tr.mu.Lock()
	if old, ok := k.tr.outbound[peerID]; ok {
		old.conn.Close()
	}
	k.tr.outbound[peerID] = &peerConn{conn: conn}
	k.tr.mu.Unlock()

	state.failures = 0
	state.backoff = 0
	state.lastPingOK = true

	k.log.Info("reconnect success", "peer", peerID, "addr", addr)
}

func (k *Keepalive) dialWithTimeout(addr string, timeout time.Duration) (net.Conn, error) {
	// 注意：这里使用 net.Dial 而不是 Transport.getOutbound 的 DialTCP，
	// 因为 Dial 支持更多协议类型（后面可以扩展 QUIC/WebSocket）
	d := net.Dialer{Timeout: timeout}
	return d.Dial("tcp", addr)
}

// ─────────────────────────────────────────────────────────────────────────────
// 状态查询
// ─────────────────────────────────────────────────────────────────────────────

// PeerHealth 返回指定 peer 的健康状态
func (k *Keepalive) PeerHealth(addr string) (ok bool, failures int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	state, exists := k.peerHealth[addr]
	if !exists {
		return false, 0
	}
	return state.lastPingOK, state.failures
}

// Stats 返回心跳统计
func (k *Keepalive) Stats() map[string]struct {
	Online   bool `json:"online"`
	Failures int  `json:"failures"`
} {
	k.mu.Lock()
	defer k.mu.Unlock()

	stats := make(map[string]struct {
		Online   bool `json:"online"`
		Failures int  `json:"failures"`
	})
	for addr, state := range k.peerHealth {
		stats[addr] = struct {
			Online   bool `json:"online"`
			Failures int  `json:"failures"`
		}{
			Online:   state.lastPingOK,
			Failures: state.failures,
		}
	}
	return stats
}

// ─────────────────────────────────────────────────────────────────────────────
// 便捷集成：为 Node 添加 Keepalive 支持
// ─────────────────────────────────────────────────────────────────────────────

// EnableKeepalive 为已有的 Node 实例添加心跳与自动重连。
// 需要在 Node.Start() 之后调用。
func EnableKeepalive(node *Node, opts KeepaliveOpts) *Keepalive {
	if opts.Interval == 0 {
		opts.Interval = 10 * time.Second
	}
	if opts.Timeout == 0 {
		opts.Timeout = 3 * time.Second
	}

	ka := NewKeepalive(node.tr, opts)
	ka.log = slog.Default().With("module", "p2p.keepalive", "node", node.identity.ID)

	go ka.Start(node.ctx)

	return ka
}

// compile-time interface check
var _ = (*Keepalive)(nil)
