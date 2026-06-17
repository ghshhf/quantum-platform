package p2p

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// NodeOption 启动参数
type NodeOption struct {
	StorageDir string   // 存储目录（密钥 + manifest）
	Name       string   // 节点显示名（默认主机名）
	ListenAddr string   // TCP 监听地址（默认 0.0.0.0:0 随机端口）
	Models     []string // 已安装的模型（用于宣告）
}

// Node 节点
type Node struct {
	mu            sync.RWMutex
	identity      *Identity
	opt           NodeOption
	disco         *Discovery
	tr            *Transport
	sched         *Scheduler
	share         *FileShare
	route         *Router       // PRP 距离向量路由表
	pex           *PexSet       // PRP 节点交换候选集合
	isHotspot     bool          // 是否扮演"个人热点/AP"角色（影响是否主动向外广播）
	started       bool
	ctx           context.Context
	cancel        context.CancelFunc
}

// localChatHandler 包级变量：本地 LLM 处理器（由 biz 层注入）
var localChatHandler func(context.Context, ChatRequest) (*ChatResponse, error)

// SetLocalChat 注入本地 chat 处理器
func SetLocalChat(fn func(context.Context, ChatRequest) (*ChatResponse, error)) {
	localChatHandler = fn
}

// NewNode 创建节点
func NewNode(opt NodeOption) (*Node, error) {
	if opt.StorageDir == "" {
		home, _ := os.UserHomeDir()
		opt.StorageDir = home + "/.quantum-platform/p2p"
	}
	if opt.Name == "" {
		opt.Name, _ = os.Hostname()
	}
	if opt.ListenAddr == "" {
		opt.ListenAddr = "0.0.0.0:0"
	}

	id, err := LoadOrCreateIdentity(opt.StorageDir, opt.Name)
	if err != nil {
		return nil, err
	}
	tr := NewTransport(id, opt.ListenAddr)
	disco := NewDiscovery(id.ID, id.Name, "", opt.Models)
	sched := NewScheduler(tr)
	share, err := NewFileShare(opt.StorageDir+"/files", tr)
	if err != nil {
		return nil, err
	}
	route := NewRouter(id.ID, tr.Addr())
	pex := NewPexSet(id.ID)

	return &Node{
		identity: id,
		opt:      opt,
		tr:       tr,
		disco:    disco,
		sched:    sched,
		share:    share,
		route:    route,
		pex:      pex,
	}, nil
}

// Start 启动节点（非阻塞）
func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	if n.started {
		n.mu.Unlock()
		return fmt.Errorf("node already started")
	}
	n.ctx, n.cancel = context.WithCancel(ctx)

	if err := n.tr.Start(n.ctx); err != nil {
		n.mu.Unlock()
		return err
	}
	// 启动完 Transport 才能拿到真实监听地址 → 同步给 route/discovery
	addr := n.tr.Addr()
	n.route = NewRouter(n.identity.ID, addr)
	n.disco.announce.Addr = addr
	n.registerHandlers()
	if err := n.disco.Start(n.ctx); err != nil {
		n.mu.Unlock()
		n.tr.Stop()
		return err
	}
	n.disco.OnNewPeer(func(p *NodePeer) {
		// 发现新 peer → 加入路由表 distance=1
		n.route.AddDirect(p.ID, p.Addr)
		n.pex.AddDirectPeer(p.ID, p.Addr, p.NodeInfo.Name)
		go n.probePeer(p)
	})
	go n.syncPeersLoop()
	go n.prpLoop() // PRP 网状互联：路由广播 + PEX + 候选探测

	n.started = true
	n.mu.Unlock()
	return nil
}

// Stop 停止节点
func (n *Node) Stop() {
	n.mu.Lock()
	if n.cancel != nil {
		n.cancel()
	}
	n.mu.Unlock()
	n.disco.Stop()
	n.tr.Stop()
}

// ====== 对外接口 ======

// Self 返回本节点信息
func (n *Node) Self() NodeInfo {
	return NodeInfo{
		ID:        n.identity.ID,
		Name:      n.identity.Name,
		Addr:      n.tr.Addr(),
		Version:   "v1",
		Models:    append([]string(nil), n.opt.Models...),
		CPUCores:  runtime.NumCPU(),
		MemoryGB:  hostMemoryGB(),
		LoadPct:   currentLoadPct(),
		HasGPU:    false,
		UpdatedAt: time.Now(),
	}
}

// Peers 返回所有已知节点
func (n *Node) Peers() []NodePeer {
	return n.disco.Peers()
}

// Connect 手动连接到某个 peer
func (n *Node) Connect(addr string) error {
	if err := validateAddr(addr); err != nil {
		return err
	}
	n.disco.AddManualPeer(addr)
	_, err := n.tr.SendWithResponse(addr, &Message{Type: MsgPing, To: "manual"}, 3*time.Second)
	return err
}

// Disconnect 断开并移除某个 peer
func (n *Node) Disconnect(peerID string) {
	n.disco.RemovePeer(peerID)
}

// UpdateModels 更新已安装模型列表
func (n *Node) UpdateModels(models []string) {
	n.mu.Lock()
	n.opt.Models = models
	n.mu.Unlock()
	n.disco.UpdateAnnounce(currentLoadPct(), false, models)
}

// Chat 发起分布式 LLM 聊天
func (n *Node) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if n.hasModelLocal(req.Model) {
		if localChatHandler != nil {
			return localChatHandler(ctx, req)
		}
	}
	return n.sched.Chat(ctx, req, 60*time.Second)
}

// ShareFile 注册本地文件为可分享
func (n *Node) ShareFile(path string) (string, error) {
	return n.share.RegisterFile(path)
}

// ListSharedFiles 返回本地已分享文件
func (n *Node) ListSharedFiles() []FileManifest {
	return n.share.List()
}

// DownloadFile 从某 peer 下载一个文件
func (n *Node) DownloadFile(ctx context.Context, peerAddr, fileID, dest string) (*FileManifest, error) {
	return n.share.DownloadFile(ctx, peerAddr, fileID, dest)
}

// ====== 内部 ======

func (n *Node) registerHandlers() {
	// Ping-Pong: 延迟探测
	n.tr.SetHandler(MsgPing, func(ctx context.Context, from string, m *Message) (*Message, error) {
		return &Message{Type: MsgPong}, nil
	})
	// 远端模型列表查询
	n.tr.SetHandler(MsgModelListRequest, func(ctx context.Context, from string, m *Message) (*Message, error) {
		n.mu.RLock()
		models := append([]string(nil), n.opt.Models...)
		n.mu.RUnlock()
		b, _ := json.Marshal(models)
		return &Message{Type: MsgModelListResponse, Payload: b}, nil
	})
	// 远端 LLM 聊天
	n.tr.SetHandler(MsgChatRequest, func(ctx context.Context, from string, m *Message) (*Message, error) {
		var req ChatRequest
		if err := json.Unmarshal(m.Payload, &req); err != nil {
			return nil, err
		}
		if localChatHandler == nil {
			return &Message{
				Type:    MsgChatResponse,
				Payload: mustJSON(ChatResponse{Error: "local model unavailable"}),
			}, nil
		}
		resp, err := localChatHandler(ctx, req)
		if err != nil {
			return &Message{
				Type:    MsgChatResponse,
				Payload: mustJSON(ChatResponse{Error: err.Error()}),
			}, nil
		}
		return &Message{
			Type:    MsgChatResponse,
			Payload: mustJSON(*resp),
		}, nil
	})
	// 文件 manifest
	n.tr.SetHandler(MsgFileManifestRequest, func(ctx context.Context, from string, m *Message) (*Message, error) {
		fileID := strings.TrimSpace(string(m.Payload))
		man, ok := n.share.Manifest(fileID)
		if !ok {
			return nil, fmt.Errorf("file %s not found", fileID)
		}
		b, _ := json.Marshal(man)
		return &Message{Type: MsgFileManifestResponse, Payload: b}, nil
	})
	// 文件 chunk
	n.tr.SetHandler(MsgChunkRequest, func(ctx context.Context, from string, m *Message) (*Message, error) {
		var cr ChunkRequest
		if err := json.Unmarshal(m.Payload, &cr); err != nil {
			return nil, err
		}
		data, err := n.share.Chunk(cr.FileID, cr.ChunkIdx)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		payload := hex.EncodeToString(sum[:]) + "|" + hex.EncodeToString(data)
		return &Message{Type: MsgChunkResponse, Payload: []byte(payload)}, nil
	})

	// ===== PRP 消息处理器 =====
	// MsgPeerExchange: 邻居把它知道的节点名单发给我
	n.tr.SetHandler(MsgPeerExchange, func(ctx context.Context, from string, m *Message) (*Message, error) {
		pe, err := UnmarshalPRP[PeerExchange](m.Payload)
		if err != nil {
			return nil, err
		}
		n.pex.Ingest(pe)
		// 响应：把我自己的 peer 名单回发一份，加快双向扩散
		resp := n.pex.BuildExchange(n.identity.ID, 50)
		return &Message{Type: MsgPeerExchange, Payload: MarshalPRP(resp)}, nil
	})
	// MsgRouteAdvertise: 邻居广播它可达的 targets，我据此更新路由表
	n.tr.SetHandler(MsgRouteAdvertise, func(ctx context.Context, from string, m *Message) (*Message, error) {
		adv, err := UnmarshalPRP[RouteAdvertise](m.Payload)
		if err != nil {
			return nil, err
		}
		n.route.ApplyAdvertise(from, adv.FromAddr, *adv)
		return nil, nil // 不需要响应
	})
	// MsgRelay: 收到一个待转发的信封
	n.tr.SetHandler(MsgRelay, func(ctx context.Context, from string, m *Message) (*Message, error) {
		inner, err := n.tr.ProcessRelay(n.route, m)
		if err != nil {
			return nil, err
		}
		if inner == nil {
			// 已成功转发出去 → 无响应给上游
			return nil, nil
		}
		// inner.To == 自己 → 按内层消息类型找 handler
		handler, ok := n.tr.Handler(inner.Type)
		if !ok {
			return nil, fmt.Errorf("no handler for relayed type %d", inner.Type)
		}
		return handler(ctx, inner.From, inner)
	})
}

func (n *Node) probePeer(p *NodePeer) {
	start := time.Now()
	_, err := n.tr.SendWithResponse(p.Addr, &Message{Type: MsgPing, To: p.ID}, 3*time.Second)
	latency := int(time.Since(start).Milliseconds())
	n.mu.Lock()
	if err == nil {
		p.LatencyMs = latency
	} else {
		p.LatencyMs = 5000
	}
	n.mu.Unlock()
}

func (n *Node) syncPeersLoop() {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-t.C:
			peers := n.disco.Peers()
			n.sched.UpdatePeers(peers)
		}
	}
}

// prpLoop PRP 网状互联主循环：
//   - 每 5s 向所有直连 peer 广播路由公告（我可达的 targets）
//   - 每 5s 向所有直连 peer 发送 PEX（我知道的节点名单，最多 50 条）
//   - 每 30s 清理过期路由条目
//   - 每 7s 对 PEX 里尚未尝试的候选节点发起主动探测
//   - 若开启 hotspot，则更积极地广播（interval 减半）
func (n *Node) prpLoop() {
	routeT := time.NewTicker(advertiseInterval)
	pexT := time.NewTicker(7 * time.Second)
	cleanupT := time.NewTicker(30 * time.Second)
	probeT := time.NewTicker(7 * time.Second)
	defer routeT.Stop()
	defer pexT.Stop()
	defer cleanupT.Stop()
	defer probeT.Stop()

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-routeT.C:
			// 向所有直连 peer 推送路由表
			peers := n.disco.Peers()
			adv := n.route.BuildAdvertise()
			for _, p := range peers {
				if !p.Online {
					continue
				}
				n.tr.Send(p.Addr, &Message{
					Type:    MsgRouteAdvertise,
					To:      p.ID,
					Payload: MarshalPRP(adv),
				})
			}
		case <-pexT.C:
			// 向所有直连 peer 推送 PEX
			peers := n.disco.Peers()
			pe := n.pex.BuildExchange(n.identity.ID, 50)
			for _, p := range peers {
				if !p.Online {
					continue
				}
				n.tr.Send(p.Addr, &Message{
					Type:    MsgPeerExchange,
					To:      p.ID,
					Payload: MarshalPRP(pe),
				})
			}
		case <-cleanupT.C:
			n.route.CleanupExpired()
		case <-probeT.C:
			// 对尚未尝试的 PEX 候选节点发起一次 MsgPing
			cands := n.pex.PickNewCandidates(5)
			for _, c := range cands {
				go func(addr, id string) {
					_, err := n.tr.SendWithResponse(addr, &Message{
						Type: MsgPing,
						To:   id,
					}, 3*time.Second)
					if err == nil {
						// 探测成功 → 加入路由表与 discovery
						n.route.AddDirect(id, addr)
						n.disco.AddManualPeer(addr)
					}
				}(c.Addr, c.ID)
			}
		}
	}
}

// ===== PRP 对外 API =====

// Routes 返回当前路由表快照（按 distance 排序）
func (n *Node) Routes() []RouteEntry {
	if n.route == nil {
		return nil
	}
	return n.route.Snapshot()
}

// IsHotspot 当前是否热点模式
func (n *Node) IsHotspot() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isHotspot
}

// SetHotspot 切换热点模式（hotspot=true 时更积极地向外推送 PEX/路由）
func (n *Node) SetHotspot(on bool) {
	n.mu.Lock()
	n.isHotspot = on
	n.mu.Unlock()
	n.disco.UpdateAnnounce(currentLoadPct(), on, n.opt.Models)
}

// PexSize 当前 PEX 候选节点数量
func (n *Node) PexSize() int {
	if n.pex == nil {
		return 0
	}
	return n.pex.Size()
}

// AddPeerAddress 手动添加一个 peer 地址（跨网段联入时用）
func (n *Node) AddPeerAddress(addr string) error {
	if err := validateAddr(addr); err != nil {
		return err
	}
	n.disco.AddManualPeer(addr)
	return nil
}

func (n *Node) hasModelLocal(model string) bool {
	if model == "" {
		return false
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, m := range n.opt.Models {
		if m == model {
			return true
		}
	}
	return false
}

// ===== 工具函数 =====

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func validateAddr(s string) error {
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return fmt.Errorf("invalid addr %q: %w", s, err)
	}
	if host == "" {
		return fmt.Errorf("addr %q missing host", s)
	}
	if port == "" {
		return fmt.Errorf("addr %q missing port", s)
	}
	return nil
}

func currentLoadPct() int {
	g := runtime.NumGoroutine()
	if g > 1000 {
		return 100
	}
	return g / 10
}

func hostMemoryGB() int {
	return 0
}
