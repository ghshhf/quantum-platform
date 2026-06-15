package p2p

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// ===== TCP 传输层 =====
//
// 协议：
//   ┌─────────────────┬─────────────────────┐
//   │  4 字节长度前缀│       JSON 消息       │
//   │  (大端 uint32) │     (变长)          │
//   └─────────────────┴─────────────────────┘

// Transport 管理本机的 TCP 服务器与出站连接
type Transport struct {
	self      *Identity
	listener  *net.TCPListener
	addr      string           // 本机监听的 TCP 地址，含端口
	mu        sync.Mutex
	outbound  map[string]*peerConn  // 到远程节点的已建连接
	handlers  map[MessageType]HandlerFunc
	ctx       context.Context
	cancel    context.CancelFunc
	nextReqID uint64
}

type HandlerFunc func(ctx context.Context, from string, msg *Message) (*Message, error)
type peerConn struct {
	conn   net.Conn
	encMu  sync.Mutex
	decMu  sync.Mutex
}

// NewTransport 创建传输层
func NewTransport(self *Identity, listenAddr string) *Transport {
	t := &Transport{
		self:     self,
		addr:     listenAddr,
		outbound: make(map[string]*peerConn),
		handlers: make(map[MessageType]HandlerFunc),
	}
	return t
}

// SetHandler 设置某类型消息的处理器
func (t *Transport) SetHandler(typ MessageType, fn HandlerFunc) {
	t.handlers[typ] = fn
}

// Handler 查询已注册的处理器（用于中继消息拆包后二次分发）
func (t *Transport) Handler(typ MessageType) (HandlerFunc, bool) {
	fn, ok := t.handlers[typ]
	return fn, ok
}

// Addr 返回实际监听地址（Start 后端口可能动态分配）
func (t *Transport) Addr() string {
	return t.addr
}

// Start 启动监听
func (t *Transport) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)
	la, err := net.ResolveTCPAddr("tcp", t.addr)
	if err != nil {
		return fmt.Errorf("resolve tcp: %w", err)
	}
	ln, err := net.ListenTCP("tcp", la)
	if err != nil {
		return fmt.Errorf("listen tcp: %w", err)
	}
	t.listener = ln
	// 动态端口（:0）→ 获取实际地址
	if la.Port == 0 {
		t.addr = ln.Addr().String()
	}
	go t.acceptLoop()
	return nil
}

// Stop 停止监听并关闭所有出站连接
func (t *Transport) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	if t.listener != nil {
		t.listener.Close()
	}
	t.mu.Lock()
	for _, p := range t.outbound {
		p.conn.Close()
	}
	t.outbound = make(map[string]*peerConn)
	t.mu.Unlock()
}

func (t *Transport) acceptLoop() {
	for {
		conn, err := t.listener.AcceptTCP()
		if err != nil {
			if t.ctx.Err() != nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		go t.handleConn(conn)
	}
}

func (t *Transport) handleConn(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Time{}) // 不超时，上层控制

	// 每条连接各自独立的解码循环
	for {
		// 1) 读 4 字节长度
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return
		}
		length := binary.BigEndian.Uint32(lenBuf)
		if length > 1024*1024*1024 { // 1GB 上限（chunk 可能比较大）
			return
		}
		// 2) 读 payload
		payload := make([]byte, length)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return
		}
		var msg Message
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}

		// 3) 分发到处理器（有则处理）
		if h, ok := t.handlers[msg.Type]; ok {
			go func(h HandlerFunc, m Message) {
				ctx, cancel := context.WithTimeout(t.ctx, 60*time.Second)
				defer cancel()
				resp, err := h(ctx, m.From, &m)
				if err == nil && resp != nil {
					// 发回响应
					resp.To = m.From
					resp.RequestID = m.RequestID
					resp.From = t.self.ID
					resp.Timestamp = time.Now().Unix()
					t.self.Sign(resp)
					b, _ := EncodeMessage(resp)
					conn.Write(b)
				}
			}(h, msg)
		}
	}
}

// Send 向 addr 发送一条消息，等待响应（如果有处理器并返回响应的话）
// 这里我们简单实现一个无等待的 Send + SendRequest（带请求匹配）
func (t *Transport) Send(addr string, msg *Message) error {
	conn, err := t.getOutbound(addr)
	if err != nil {
		return err
	}
	conn.encMu.Lock()
	defer conn.encMu.Unlock()
	msg.From = t.self.ID
	msg.Timestamp = time.Now().Unix()
	if msg.RequestID == "" {
		msg.RequestID = fmt.Sprintf("r%d", atomic.AddUint64(&t.nextReqID, 1))
	}
	t.self.Sign(msg)
	b, err := EncodeMessage(msg)
	if err != nil {
		return err
	}
	conn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.conn.Write(b)
	return err
}

// SendWithResponse 发送一条消息并等待响应，响应由 requestID 匹配
// timeout: 最长等待时间
func (t *Transport) SendWithResponse(addr string, msg *Message, timeout time.Duration) (*Message, error) {
	respCh := make(chan *Message, 1)
	reqID := fmt.Sprintf("r%d", atomic.AddUint64(&t.nextReqID, 1))
	msg.RequestID = reqID

	conn, err := t.getOutbound(addr)
	if err != nil {
		return nil, err
	}

	conn.encMu.Lock()
	msg.From = t.self.ID
	msg.Timestamp = time.Now().Unix()
	t.self.Sign(msg)
	b, err := EncodeMessage(msg)
	if err != nil {
		conn.encMu.Unlock()
		return nil, err
	}
	conn.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.conn.Write(b); err != nil {
		conn.encMu.Unlock()
		return nil, err
	}
	conn.encMu.Unlock()

	// 在该连接上等待一条响应
	go func() {
		conn.decMu.Lock()
		defer conn.decMu.Unlock()
		lenBuf := make([]byte, 4)
		conn.conn.SetReadDeadline(time.Now().Add(timeout))
		if _, err := io.ReadFull(conn.conn, lenBuf); err != nil {
			close(respCh)
			return
		}
		length := binary.BigEndian.Uint32(lenBuf)
		payload := make([]byte, length)
		if _, err := io.ReadFull(conn.conn, payload); err != nil {
			close(respCh)
			return
		}
		var m Message
		if err := json.Unmarshal(payload, &m); err != nil {
			close(respCh)
			return
		}
		respCh <- &m
		close(respCh)
	}()

	select {
	case resp := <-respCh:
		if resp == nil {
			return nil, fmt.Errorf("no response (connection closed or timeout)")
		}
		return resp, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout after %v", timeout)
	}
}

func (t *Transport) getOutbound(addr string) (*peerConn, error) {
	t.mu.Lock()
	if p, ok := t.outbound[addr]; ok {
		t.mu.Unlock()
		return p, nil
	}
	t.mu.Unlock()

	// 建新连接
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", addr, err)
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	p := &peerConn{conn: conn}
	t.mu.Lock()
	t.outbound[addr] = p
	t.mu.Unlock()
	return p, nil
}

// ====== PRP 中继支持 =====

// SendVia 路由到 target 发送一条消息：
//  - 如果 route 能给出 nextHop → 发 MsgRelay 到该 nextHop
//  - 否则退化为直接到 target 的 Send
// SendVia 的目标节点收到后会在它自己的路由表内再路由。
func (t *Transport) SendVia(route *Router, targetID, targetMaybeAddr string, msg *Message) error {
	var nextHop string
	if route != nil && targetID != "" {
		nextHop, _ = route.NextHop(targetID)
	}
	if nextHop == "" {
		// 路由表不可达 → 回退为直接投递（需要调用者负责填 targetMaybeAddr）
		addr := targetMaybeAddr
		if addr == "" {
			return fmt.Errorf("no route and no direct address for target %s", targetID)
		}
		return t.Send(addr, msg)
	}
	// nextHop 既可能是下一跳中继节点，也可能是目标自己（distance=1）
	// 无论哪种情形，都包成 RelayEnvelope 转发，目标节点会拆开处理。
	// 简单起见：如果 nextHop 就是目标，也包成信封，目标会在 handleConn 内拆开。
	env := RelayEnvelope{
		OriginalTo:   targetID,
		OriginalFrom: t.self.ID,
		TTL:          defaultMaxDistance,
		HopCount:     1,
		Via:          t.self.ID,
		InnerType:    msg.Type,
		InnerPayload: msg.Payload,
		Path:         []string{t.self.ID},
	}
	relay := &Message{
		Type:      MsgRelay,
		To:        targetID,
		RequestID: msg.RequestID,
		Payload:   MarshalPRP(env),
	}
	return t.Send(nextHop, relay)
}

// SendViaWithResponse 发送中继 + 等待响应
// 路由表把目标响应回落到直接响应。目标收到后处理。
func (t *Transport) SendViaWithResponse(route *Router, targetID, targetMaybeAddr string, msg *Message, timeout time.Duration) (*Message, error) {
	nextHop := ""
	distance := -1
	if route != nil && targetID != "" {
		nextHop, distance = route.NextHop(targetID)
	}
	if nextHop == "" {
		// 没有路由表 → 直接尝试直连
		addr := targetMaybeAddr
		if addr == "" {
			return nil, fmt.Errorf("no route for target %s", targetID)
		}
		return t.SendWithResponse(addr, msg, timeout)
	}
	// 距离=0 是自己，返回 nil（调用者应自己处理）
	if distance == 0 {
		return nil, fmt.Errorf("target is self")
	}
	// 构建 RelayEnvelope（带 request id 便于匹配）
	reqID := fmt.Sprintf("r%d", atomic.AddUint64(&t.nextReqID, 1))
	env := RelayEnvelope{
		OriginalTo:   targetID,
		OriginalFrom: t.self.ID,
		TTL:        defaultMaxDistance,
		HopCount:   1,
		Via:        t.self.ID,
		InnerType:  msg.Type,
		InnerPayload: msg.Payload,
		Path:       []string{t.self.ID},
	}
	relayMsg := &Message{
		Type:      MsgRelay,
		To:        targetID,
		RequestID: reqID,
		Payload:   MarshalPRP(env),
	}
	// 在下一跳连接上发出去并等待响应（目标会把响应原路返回）
	return t.SendWithResponse(nextHop, relayMsg, timeout)
}

// ProcessRelay 处理一条收到的 MsgRelay：
//  - 如果目标是自己 → 返回 (decoded inner message，供上层 handler 处理
//  - 否则 → 把信封 TTL-- 并转发到下一跳
// 返回值: 该中继消息（若为目标自己)或 nil 表示“处理完成（转发完成，需要调用者不要继续处理
func (t *Transport) ProcessRelay(route *Router, raw *Message) (*Message, error) {
	env, err := UnmarshalPRP[RelayEnvelope](raw.Payload)
	if err != nil {
		return nil, err
	}
	if env.TTL <= 0 {
		return nil, fmt.Errorf("relay TTL expired")
	}
	// 目标是否是自己？
	if env.OriginalTo == t.self.ID {
		// 把内层消息解出来供上层按 InnerType 处理
		return &Message{
			Type:      env.InnerType,
			From:      env.OriginalFrom,
			To:        env.OriginalTo,
			RequestID: raw.RequestID,
			Timestamp: time.Now().Unix(),
			Payload:   env.InnerPayload,
		}, nil
	}
	// 需要转发
	env.TTL--
	env.HopCount++
	env.Via = t.self.ID
	env.Path = append(env.Path, t.self.ID)
	nextHop, _ := route.NextHop(env.OriginalTo)
	if nextHop == "" {
		return nil, fmt.Errorf("no route to forward relay target %s", env.OriginalTo)
	}
	forward := &Message{
		Type:      MsgRelay,
		To:        env.OriginalTo,
		RequestID: raw.RequestID,
		Payload:   MarshalPRP(env),
	}
	return nil, t.Send(nextHop, forward) // 返回 nil 表示本节点不继续处理内层
}
