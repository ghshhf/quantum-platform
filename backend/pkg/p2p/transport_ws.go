// ─── WebSocket 传输层 ─────────────────────────────────────────────────────────
//
// 参考 cloudflared carrier/websocket.go 的设计模式：
//   把 WebSocket 连接当作 io.ReadWriter 封装，作为 p2p 传输层的一个备选方案。
//
// 适用场景：
//   - 浏览器/前端通过 WebSocket 加入 p2p 网络
//   - 防火墙严格环境下 WebSocket 比 TCP 更容易穿透
//   - Bridge 与前端 Entity 之间的通信通道
//
// 消息格式与 TCP Transport 兼容（JSON over 二进制帧），
// 因此 message handler / Router / Scheduler 等上层模块无需改动。
//
// 用法：
//   wsTr := NewWSTransport(id, ":8080")
//   wsTr.SetHandler(MsgPing, pingHandler)
//   wsTr.Start(ctx)       // 监听 WebSocket 连接
//   wsTr.Send(addr, msg)  // 作为客户端向远程 WS 端点发送消息

package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gorilla "github.com/gorilla/websocket"
)

// WSTransport 提供基于 WebSocket 的 p2p 传输层，与 TCP Transport 接口对齐。
type WSTransport struct {
	self     *Identity
	addr     string           // 监听地址（WS 路径: /p2p）
	upgrader gorilla.Upgrader
	server   *http.Server
	mux      *http.ServeMux

	mu        sync.Mutex
	peers     map[string]*wsPeerConn  // peerID → WebSocket 连接
	handlers  map[MessageType]HandlerFunc
	nextReqID uint64

	ctx    context.Context
	cancel context.CancelFunc

	log *slog.Logger
}

// wsPeerConn 封装一个到对端的 WebSocket 连接
type wsPeerConn struct {
	conn *gorilla.Conn
	mu   sync.Mutex // 串行化写入（gorilla 不支持并发写）
	addr string     // 远端 peer 地址（用于 re-dial）
}

// NewWSTransport 创建一个 WebSocket 传输层。
// listenAddr 格式 "host:port"，如 ":8080" 或 "0.0.0.0:8080"。
// WebSocket 路径固定为 /p2p。
func NewWSTransport(self *Identity, listenAddr string) *WSTransport {
	if listenAddr == "" {
		listenAddr = ":0"
	}
	mux := http.NewServeMux()
	t := &WSTransport{
		self:     self,
		addr:     listenAddr,
		peers:    make(map[string]*wsPeerConn),
		handlers: make(map[MessageType]HandlerFunc),
		mux:      mux,
		upgrader: gorilla.Upgrader{
			ReadBufferSize:  65536,
			WriteBufferSize: 65536,
			// 允许跨域（p2p 场景可能需要）
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		log: slog.Default().With("module", "p2p.wstransport"),
	}
	mux.HandleFunc("/p2p", t.handleWS)
	return t
}

// SetHandler 注册消息处理器（与 Transport.SetHandler 签名一致）
func (t *WSTransport) SetHandler(typ MessageType, fn HandlerFunc) {
	t.handlers[typ] = fn
}

// Handler 查询已注册的处理器
func (t *WSTransport) Handler(typ MessageType) (HandlerFunc, bool) {
	fn, ok := t.handlers[typ]
	return fn, ok
}

// Addr 返回实际监听地址（Start 后端口可能动态分配:0）
func (t *WSTransport) Addr() string {
	return t.addr
}

// WSListenAddr 返回 WebSocket 连接用的完整地址（含 ws:// 前缀）
func (t *WSTransport) WSListenAddr() string {
	parts := strings.SplitN(t.addr, ":", 2)
	host := parts[0]
	port := "8080"
	if len(parts) > 1 && parts[1] != "" && parts[1] != "0" {
		port = parts[1]
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("ws://%s:%s/p2p", host, port)
}

// Start 启动 WebSocket 服务器
func (t *WSTransport) Start(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)

	listener, err := net.Listen("tcp", t.addr)
	if err != nil {
		return fmt.Errorf("ws listen: %w", err)
	}
	// 获取实际端口（:0 情况）
	t.addr = listener.Addr().String()

	t.server = &http.Server{
		Handler: t.mux,
	}
	go func() {
		if err := t.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			t.log.Error("ws server error", "error", err)
		}
	}()
	go t.heartbeatLoop()

	t.log.Info("ws transport started", "addr", t.addr, "ws_addr", t.WSListenAddr())
	return nil
}

// Stop 停止服务器并关闭所有连接
func (t *WSTransport) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
	if t.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		t.server.Shutdown(ctx)
	}
	t.mu.Lock()
	for id, p := range t.peers {
		p.conn.Close()
		delete(t.peers, id)
	}
	t.mu.Unlock()
}

// ===== 入站连接处理 =====

func (t *WSTransport) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := t.upgrader.Upgrade(w, r, nil)
	if err != nil {
		t.log.Warn("ws upgrade failed", "error", err, "remote", r.RemoteAddr)
		return
	}
	t.log.Info("ws peer connected", "remote", r.RemoteAddr)
	t.handlePeer(conn)
}

func (t *WSTransport) handlePeer(conn *gorilla.Conn) {
	defer conn.Close()

	var peerID string
	for {
		// 读取二进制帧
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			if t.ctx.Err() == nil {
				t.log.Info("ws peer disconnected", "peer", peerID, "error", err)
			}
			// 从连接池移除
			if peerID != "" {
				t.mu.Lock()
				delete(t.peers, peerID)
				t.mu.Unlock()
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			t.log.Debug("ws invalid message", "error", err)
			continue
		}

		// 首次收到消息时就地记录 peerID
		if peerID == "" && msg.From != "" {
			peerID = msg.From
			t.mu.Lock()
			t.peers[peerID] = &wsPeerConn{conn: conn, addr: conn.RemoteAddr().String()}
			t.mu.Unlock()
		}

		msg.From = peerID

		// 分发到处理器
		if h, ok := t.handlers[msg.Type]; ok {
			go func(h HandlerFunc, m Message) {
				ctx, cancel := context.WithTimeout(t.ctx, 60*time.Second)
				defer cancel()
				resp, err := h(ctx, m.From, &m)
				if err == nil && resp != nil {
					resp.To = m.From
					resp.RequestID = m.RequestID
					resp.From = t.self.ID
					resp.Timestamp = time.Now().Unix()
					t.self.Sign(resp)
					t.sendToConn(conn, resp)
				}
			}(h, msg)
		}
	}
}

// ===== 出站消息发送 =====

// Send 向指定地址发送一条消息。
// addr 格式：ws://host:port/p2p 或 host:port（自动补全）
func (t *WSTransport) Send(addr string, msg *Message) error {
	wsURL := normalizeWSAddr(addr)
	conn, err := t.getOrDial(wsURL)
	if err != nil {
		return err
	}
	msg.From = t.self.ID
	msg.Timestamp = time.Now().Unix()
	if msg.RequestID == "" {
		msg.RequestID = fmt.Sprintf("wr%d", atomic.AddUint64(&t.nextReqID, 1))
	}
	t.self.Sign(msg)
	return t.sendToConn(conn, msg)
}

// SendWithResponse 发送消息并等待响应
func (t *WSTransport) SendWithResponse(addr string, msg *Message, timeout time.Duration) (*Message, error) {
	wsURL := normalizeWSAddr(addr)
	conn, err := t.getOrDial(wsURL)
	if err != nil {
		return nil, err
	}

	reqID := fmt.Sprintf("wr%d", atomic.AddUint64(&t.nextReqID, 1))
	msg.RequestID = reqID
	msg.From = t.self.ID
	msg.Timestamp = time.Now().Unix()
	t.self.Sign(msg)

	// 发送
	if err := t.sendToConn(conn, msg); err != nil {
		return nil, err
	}

	// 读取响应（gorilla 不支持在同一个连接上 Select 读取，所以使用 ReadMessage 同步等待）
	// 注意：这会阻塞该连接上的其他入站消息。
	// 在生产环境中应使用消息多路复用（如按 RequestID 匹配）。
	conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("ws recv: %w", err)
		}
		var resp Message
		if err := json.Unmarshal(raw, &resp); err != nil {
			continue
		}
		if resp.RequestID == reqID {
			return &resp, nil
		}
		// 非匹配消息 → 作为普通消息重新分发
		if h, ok := t.handlers[resp.Type]; ok {
			go h(t.ctx, resp.From, &resp) // 忽略返回值
		}
	}
}

// ===== 内部工具 =====

func (t *WSTransport) getOrDial(wsURL string) (*gorilla.Conn, error) {
	// 从连接池查找
	t.mu.Lock()
	for _, p := range t.peers {
		if p.addr == wsURL || strings.HasSuffix(p.addr, wsURL) {
			t.mu.Unlock()
			return p.conn, nil
		}
	}
	t.mu.Unlock()

	// 没有现成连接 → 主动拨号
	dialer := gorilla.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ws dial %s: %w", wsURL, err)
	}
	// 暂不入连接池（server 端处理后端会自行入池），返回给调用者
	return conn, nil
}

func (t *WSTransport) sendToConn(conn *gorilla.Conn, msg *Message) error {
	// 注意：gorilla 建议使用 WriteJSON，但我们需要控制序列化以便后续支持压缩等
	b, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("ws marshal: %w", err)
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(gorilla.BinaryMessage, b)
}

// heartbeatLoop 每 10s 向所有已连接的 peer 发送 MsgPing 保活
func (t *WSTransport) heartbeatLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			peers := make(map[string]*wsPeerConn)
			for id, p := range t.peers {
				peers[id] = p
			}
			t.mu.Unlock()

			for id, p := range peers {
				ping := &Message{
					Type: MsgPing,
					From: t.self.ID,
					To:   id,
				}
				p.mu.Lock()
				p.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
				err := p.conn.WriteJSON(ping)
				p.mu.Unlock()
				if err != nil {
					t.log.Warn("ws heartbeat failed, closing", "peer", id, "error", err)
					p.conn.Close()
					t.mu.Lock()
					delete(t.peers, id)
					t.mu.Unlock()
				}
			}
		}
	}
}

// ===== 工具 =====

func normalizeWSAddr(addr string) string {
	if strings.HasPrefix(addr, "ws://") || strings.HasPrefix(addr, "wss://") {
		return addr
	}
	// "host:port" → "ws://host:port/p2p"
	// "host:port/path" → 保持原样加 ws:// 前缀
	path := "/p2p"
	if strings.Contains(addr, "/") {
		path = ""
	}
	return fmt.Sprintf("ws://%s%s", addr, path)
}

// WSTransportViaRelay 中继支持（简化版）
// 复用 Transport.SendVia 的逻辑，但使用 WebSocket 发送
func (t *WSTransport) SendVia(route *Router, targetID, targetMaybeAddr string, msg *Message) error {
	var nextHop string
	if route != nil && targetID != "" {
		nextHop, _ = route.NextHop(targetID)
	}
	if nextHop == "" {
		addr := targetMaybeAddr
		if addr == "" {
			return fmt.Errorf("no route and no direct address for target %s", targetID)
		}
		return t.Send(addr, msg)
	}
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

// compile-time interface check
var _ = (*WSTransport)(nil)
