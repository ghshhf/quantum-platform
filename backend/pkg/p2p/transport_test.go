// ─── p2p Transport 单元测试 ───────────────────────────────────────────────────
//
// 测试策略：
//   - 使用两个 Transport 实例在 localhost 上建真实 TCP 连接
//   - 覆盖：消息编码、handler 分发、Send/SendWithResponse、中继、生命周期

package p2p

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ===== 辅助工具 =====

// testIdentity 创建一个测试用的 Identity（不写磁盘）
func testIdentity(t *testing.T, name string) *Identity {
	t.Helper()
	id, err := LoadOrCreateIdentity(t.TempDir(), name)
	if err != nil {
		t.Fatalf("testIdentity: %v", err)
	}
	return id
}

// testTransport 创建一个 Transport 并启动，返回已启动的 Transport 和它的地址
func testTransport(t *testing.T, name string) (*Transport, *Identity) {
	t.Helper()
	id := testIdentity(t, name)
	tr := NewTransport(id, "127.0.0.1:0")
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start %s: %v", name, err)
	}
	t.Cleanup(tr.Stop)
	return tr, id
}

// ===== EncodeMessage / DecodeMessage =====

func TestEncodeDecodeRoundtrip(t *testing.T) {
	msg := &Message{
		Type:      MsgPing,
		From:      "node-a",
		To:        "node-b",
		RequestID: "r1",
		Timestamp: 1234567890,
		Payload:   []byte("hello"),
	}

	encoded, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage: %v", err)
	}

	// 验证前 4 字节是长度 + 剩余为 JSON
	if len(encoded) < 4 {
		t.Fatal("encoded too short")
	}
	length := binary.BigEndian.Uint32(encoded[:4])
	if int(length) != len(encoded)-4 {
		t.Errorf("length prefix: got %d, want %d", length, len(encoded)-4)
	}

	// 验证 JSON 可反解
	decoded, err := DecodeMessage(encoded[4:])
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	if decoded.Type != MsgPing {
		t.Errorf("Type: got %d, want %d", decoded.Type, MsgPing)
	}
	if decoded.From != "node-a" {
		t.Errorf("From: got %q, want %q", decoded.From, "node-a")
	}
	if decoded.RequestID != "r1" {
		t.Errorf("RequestID: got %q, want %q", decoded.RequestID, "r1")
	}
}

func TestEncodeMessageLargePayload(t *testing.T) {
	payload := make([]byte, 10000)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	msg := &Message{Type: MsgChunkResponse, Payload: payload}
	encoded, err := EncodeMessage(msg)
	if err != nil {
		t.Fatalf("EncodeMessage large: %v", err)
	}
	length := binary.BigEndian.Uint32(encoded[:4])
	if int(length) != len(encoded)-4 {
		t.Errorf("large payload length: got %d, want %d", length, len(encoded)-4)
	}
}

// ===== SetHandler / Handler =====

func TestSetHandlerAndHandler(t *testing.T) {
	tr := NewTransport(testIdentity(t, "handler-test"), "127.0.0.1:0")

	// 注册前查询 → 不存在
	if _, ok := tr.Handler(MsgPing); ok {
		t.Error("expected MsgPing handler to not exist before registration")
	}

	// 注册
	fn := func(ctx context.Context, from string, msg *Message) (*Message, error) {
		return &Message{Type: MsgPong}, nil
	}
	tr.SetHandler(MsgPing, fn)

	// 注册后查询 → 存在且返回正确
	got, ok := tr.Handler(MsgPing)
	if !ok {
		t.Fatal("expected MsgPing handler to exist after registration")
	}
	resp, err := got(context.Background(), "test", &Message{Type: MsgPing})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if resp.Type != MsgPong {
		t.Errorf("handler resp Type: got %d, want %d", resp.Type, MsgPong)
	}

	// 其他类型 → 不存在
	if _, ok := tr.Handler(MsgHello); ok {
		t.Error("expected MsgHello handler to not exist")
	}
}

// ===== Start / Stop 生命周期 =====

func TestStartStop(t *testing.T) {
	id := testIdentity(t, "lifecycle")
	tr := NewTransport(id, "127.0.0.1:0")

	// 启动前 Addr 为原始值
	before := tr.Addr()
	if before != "127.0.0.1:0" {
		t.Errorf("before Start Addr: got %q", before)
	}

	// Start 后 Addr 变为真实地址（含端口）
	if err := tr.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	after := tr.Addr()
	if after == "127.0.0.1:0" {
		t.Error("Addr should be updated to real address after Start")
	}

	// 不能重复 Start
	if err := tr.Start(context.Background()); err == nil {
		t.Error("expected error on duplicate Start, got nil")
	}

	// Stop
	tr.Stop()

	// Stop 后不应再有 accept loop 运行（验证方式：尝试连接应该被拒绝）
	conn, err := net.DialTimeout("tcp", after, 2*time.Second)
	if err == nil {
		conn.Close()
		t.Error("expected connection to be refused after Stop")
	}
}

// ===== Send 与 handler 分发 =====

func TestSendAndHandle(t *testing.T) {
	trA, idA := testTransport(t, "send-a")
	trB, _ := testTransport(t, "send-b")

	received := make(chan *Message, 1)
	trB.SetHandler(MsgHello, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		received <- msg
		return &Message{Type: MsgGoodbye, Payload: []byte("ack")}, nil
	})

	// A 向 B 发送消息
	ping := &Message{Type: MsgHello, Payload: []byte("你好 B")}
	err := trA.Send(trB.Addr(), ping)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// B 应该收到消息
	select {
	case got := <-received:
		if got.Type != MsgHello {
			t.Errorf("received type: got %d, want %d", got.Type, MsgHello)
		}
		if string(got.Payload) != "你好 B" {
			t.Errorf("received payload: got %q, want %q", string(got.Payload), "你好 B")
		}
		if got.From != idA.ID {
			t.Errorf("received From: got %q, want %q", got.From, idA.ID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

// ===== SendWithResponse 请求-响应 =====

func TestSendWithResponse(t *testing.T) {
	trA, _ := testTransport(t, "req-a")
	trB, _ := testTransport(t, "req-b")

	trB.SetHandler(MsgChatRequest, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		return &Message{
			Type:    MsgChatResponse,
			Payload: []byte(`{"content":"你好，我是 B","usage":{"total_tokens":42}}`),
		}, nil
	})

	// A 发请求 + 等响应
	resp, err := trA.SendWithResponse(trB.Addr(), &Message{
		Type:    MsgChatRequest,
		Payload: []byte(`{"model":"qwen","messages":[{"role":"user","content":"hi"}]}`),
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("SendWithResponse: %v", err)
	}
	if resp.Type != MsgChatResponse {
		t.Errorf("response type: got %d, want %d", resp.Type, MsgChatResponse)
	}
	if string(resp.Payload) != `{"content":"你好，我是 B","usage":{"total_tokens":42}}` {
		t.Errorf("response payload: got %q", string(resp.Payload))
	}
}

func TestSendWithResponseTimeout(t *testing.T) {
	trA, _ := testTransport(t, "timeout-a")
	trB, _ := testTransport(t, "timeout-b")

	// B 注册一个永不响应的处理器
	trB.SetHandler(MsgChatRequest, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		// 故意不返回，让 A 超时
		time.Sleep(10 * time.Second)
		return nil, nil
	})

	_, err := trA.SendWithResponse(trB.Addr(), &Message{
		Type:    MsgChatRequest,
		Payload: []byte(`hello`),
	}, 500*time.Millisecond) // 500ms 超时
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}

// ===== getOutbound 连接复用 =====

func TestGetOutboundConnectionReuse(t *testing.T) {
	trA, _ := testTransport(t, "reuse-a")
	trB, _ := testTransport(t, "reuse-b")

	// 发两次消息到同一个地址 → 应复用同一条连接
	var conn1, conn2 net.Conn

	trA.mu.Lock()
	trA.Send(trB.Addr(), &Message{Type: MsgPing})
	p1 := trA.outbound[trB.Addr()]
	if p1 != nil {
		conn1 = p1.conn
	}
	trA.mu.Unlock()

	if conn1 == nil {
		t.Fatal("first Send did not create outbound connection")
	}

	trA.mu.Lock()
	trA.Send(trB.Addr(), &Message{Type: MsgPing})
	p2 := trA.outbound[trB.Addr()]
	if p2 != nil {
		conn2 = p2.conn
	}
	trA.mu.Unlock()

	if conn2 == nil {
		t.Fatal("second Send did not find outbound connection")
	}

	if conn1 != conn2 {
		t.Error("expected same connection to be reused, got different conns")
	}
}

// ===== SendVia / ProcessRelay 中继 =====

func TestSendViaDirect(t *testing.T) {
	// A → B（直连，无路由表=直接 Send）
	trA, idA := testTransport(t, "via-a")
	trB, _ := testTransport(t, "via-b")

	received := make(chan *Message, 1)
	trB.SetHandler(MsgChatRequest, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		received <- msg
		return &Message{Type: MsgChatResponse}, nil
	})

	err := trA.SendVia(nil, idA.ID, trB.Addr(), &Message{
		Type:    MsgChatRequest,
		Payload: []byte("via direct"),
	})
	if err != nil {
		t.Fatalf("SendVia direct: %v", err)
	}

	select {
	case got := <-received:
		if string(got.Payload) != "via direct" {
			t.Errorf("via payload: got %q", string(got.Payload))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for via direct")
	}
}

func TestProcessRelayTargetIsSelf(t *testing.T) {
	tr, _ := testTransport(t, "relay-self")

	env := RelayEnvelope{
		OriginalTo:   tr.self.ID, // 目标是自己
		OriginalFrom: "remote-node",
		InnerType:    MsgChatRequest,
		InnerPayload: []byte("hello via relay"),
		TTL:          8,
		HopCount:     2,
	}
	raw := &Message{
		Type:    MsgRelay,
		Payload: MarshalPRP(env),
	}

	result, err := tr.ProcessRelay(nil, raw)
	if err != nil {
		t.Fatalf("ProcessRelay: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result when target is self")
	}
	if result.Type != MsgChatRequest {
		t.Errorf("inner type: got %d, want %d", result.Type, MsgChatRequest)
	}
	if string(result.Payload) != "hello via relay" {
		t.Errorf("inner payload: got %q", string(result.Payload))
	}
}

func TestProcessRelayTTLExpired(t *testing.T) {
	tr, _ := testTransport(t, "relay-ttl")

	env := RelayEnvelope{
		OriginalTo:   "far-away-node",
		OriginalFrom: "some-node",
		TTL:          0, // 过期
		InnerType:    MsgChatRequest,
	}
	raw := &Message{
		Type:    MsgRelay,
		Payload: MarshalPRP(env),
	}

	_, err := tr.ProcessRelay(nil, raw)
	if err == nil {
		t.Error("expected error for expired TTL, got nil")
	}
}

func TestSendViaWithResponseRelay(t *testing.T) {
	// 场景：A 想发给 C，但只知道 B 知道 C 的地址
	// A → B(中继) → C


	// 创建三个节点
	trA, _ := testTransport(t, "relay-a")
	trB, _ := testTransport(t, "relay-b")
	trC, _ := testTransport(t, "relay-c")

	// C 的处理器
	cReceived := make(chan *Message, 1)
	trC.SetHandler(MsgChatRequest, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		cReceived <- msg
		return &Message{Type: MsgChatResponse, Payload: []byte("来自 C 的回复")}, nil
	})

	// B 的处理器：处理 MsgRelay（转发到 C）
	// 注意：C 是 B 的直连 peer
	routeC := NewRouter(trB.self.ID, trB.Addr())
	routeC.AddDirect(trC.self.ID, trC.Addr())

	trB.SetHandler(MsgRelay, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		inner, err := trB.ProcessRelay(routeC, msg)
		if err != nil {
			return nil, err
		}
		if inner == nil {
			return nil, nil // 已转发
		}
		// inner 目标是自己 → 分发
		if h, ok := trB.Handler(inner.Type); ok {
			return h(ctx, inner.From, inner)
		}
		return nil, fmt.Errorf("no handler for inner type %d", inner.Type)
	})

	// A 的路由表：知道到 C 需要经过 B
	routeA := NewRouter(trA.self.ID, trA.Addr())
	routeA.AddDirect(trB.self.ID, trB.Addr())
	// A 知道 C 经过 B（distance=2）
	routeA.ApplyAdvertise(trB.self.ID, trB.Addr(), RouteAdvertise{
		From:     trB.self.ID,
		FromAddr: trB.Addr(),
		Entries: []RouteEntry{{
			Target:   trC.self.ID,
			NextHop:  trB.Addr(),
			Distance: 1,
		}},
	})

	// A 通过中继发消息给 C
	_, err := trA.SendViaWithResponse(routeA, trC.self.ID, "",
		&Message{Type: MsgChatRequest, Payload: []byte("A 发给 C 的中继消息")},
		5*time.Second)
	if err != nil {
		t.Fatalf("SendViaWithResponse relay: %v", err)
	}

	select {
	case got := <-cReceived:
		if string(got.Payload) != "A 发给 C 的中继消息" {
			t.Errorf("relay payload: got %q", string(got.Payload))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout: C did not receive relayed message")
	}
}

// ===== 并发安全 =====

func TestConcurrentSend(t *testing.T) {
	trA, _ := testTransport(t, "concur-a")
	trB, _ := testTransport(t, "concur-b")

	msgCount := int32(0)
	trB.SetHandler(MsgPing, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		atomic.AddInt32(&msgCount, 1)
		return &Message{Type: MsgPong}, nil
	})

	// 并发发 10 条消息
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := trA.Send(trB.Addr(), &Message{Type: MsgPing, Payload: []byte(fmt.Sprintf("msg-%d", i))})
			if err != nil {
				t.Errorf("concurrent Send %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	// 给 B 一点时间处理所有消息
	time.Sleep(500 * time.Millisecond)

	if n := atomic.LoadInt32(&msgCount); n != 10 {
		t.Errorf("handler called %d times, want 10", n)
	}
}

// ===== 不存在的地址 =====

func TestSendToNonExistent(t *testing.T) {
	trA, _ := testTransport(t, "noexist-a")

	err := trA.Send("127.0.0.1:1", &Message{Type: MsgPing})
	if err == nil {
		t.Error("expected error when sending to non-existent address")
	}
}

// ===== 手动 TCP 帧解析 =====

func TestManualTCPSendAndReceive(t *testing.T) {
	tr, _ := testTransport(t, "manual-srv")

	gotMsg := make(chan *Message, 1)
	tr.SetHandler(MsgHello, func(ctx context.Context, from string, msg *Message) (*Message, error) {
		gotMsg <- msg
		return &Message{Type: MsgGoodbye, Payload: []byte("ok")}, nil
	})

	// 手动构造 TCP 连接并发送一条合法的帧
	conn, err := net.DialTimeout("tcp", tr.Addr(), 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := &Message{Type: MsgHello, From: "manual-client", Payload: []byte("手动测试")}
	encoded, _ := EncodeMessage(msg)
	if _, err := conn.Write(encoded); err != nil {
		t.Fatalf("write: %v", err)
	}

	// 读响应帧（4 字节长度 + JSON）
	lenBuf := make([]byte, 4)
	if _, err := conn.Read(lenBuf); err != nil {
		t.Fatalf("read len: %v", err)
	}
	length := binary.BigEndian.Uint32(lenBuf)
	respBuf := make([]byte, length)
	if _, err := conn.Read(respBuf); err != nil {
		t.Fatalf("read body: %v", err)
	}
	var resp Message
	if err := json.Unmarshal(respBuf, &resp); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if resp.Type != MsgGoodbye {
		t.Errorf("resp type: got %d, want %d", resp.Type, MsgGoodbye)
	}
	if string(resp.Payload) != "ok" {
		t.Errorf("resp payload: got %q, want %q", string(resp.Payload), "ok")
	}

	select {
	case <-gotMsg:
		// 收到即通过
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: handler did not receive message")
	}
}
