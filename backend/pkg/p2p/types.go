// Package p2p 提供 量子平台 的本地组网、算力共享与模型分发能力
// 设计原则：零新增依赖，仅用 Go 标准库 + golang.org/x/crypto
//
// 架构概览：
//
//	Node（节点）─── UDP multicast (224.0.0.251:60101) ─── 节点发现
//	  │
//	  ├── TCP server (端口自动选择) ── 与其他节点的 RPC/文件传输
//	  │
//	  ├── Scheduler（调度器）── 按需把 LLM 请求路由到最佳节点
//	  │
//	  └── FileShare（文件分片）── 64MB chunk + manifest + 并发下载
package p2p

import "time"

// ===== 节点身份 =====

// NodeInfo 对外暴露的节点信息
type NodeInfo struct {
	ID         string   `json:"id"`          // 节点 ID = Ed25519 公钥前 16 字节 hex
	Name       string   `json:"name"`        // 显示名（默认主机名）
	Addr       string   `json:"addr"`        // TCP 地址，如 192.168.1.10:58000
	Version    string   `json:"version"`     // 协议版本，v1
	Models     []string `json:"models"`      // 已安装的模型 ID（qwen2.5:7b 等）
	CPUCores   int      `json:"cpu_cores"`   // CPU 核心数
	MemoryGB   int      `json:"memory_gb"`   // 内存（GB）
	LoadPct    int      `json:"load_pct"`    // 当前负载（0-100）
	HasGPU     bool     `json:"has_gpu"`     // 是否有 GPU
	Region     string   `json:"region"`      // 区域（local/remote）
	UpdatedAt  time.Time `json:"updated_at"`
}

// NodePeer 表示一个已连接的远程节点（含健康状态）
type NodePeer struct {
	NodeInfo
	LatencyMs  int           `json:"latency_ms"`   // 最近一次 ping 的延迟
	Online     bool          `json:"online"`       // 是否在线
	FirstSeen  time.Time     `json:"first_seen"`
	LastSeen   time.Time     `json:"last_seen"`
}

// ===== 消息协议 =====

// MessageType 消息类型
type MessageType uint8

const (
	MsgHello       MessageType = iota // 节点宣告自己存在（UDP 广播 & TCP 握手）
	MsgPing                          // 延迟探测
	MsgPong                          // 延迟应答
	MsgChatRequest                   // LLM 聊天请求（远端转发）
	MsgChatResponse                  // LLM 聊天响应
	MsgModelListRequest              // 请求对方模型列表
	MsgModelListResponse             // 返回模型列表
	MsgFileManifestRequest           // 请求某文件的 manifest
	MsgFileManifestResponse          // 返回 manifest
	MsgChunkRequest                  // 请求某 chunk
	MsgChunkResponse                 // 返回 chunk
	MsgGoodbye                       // 优雅下线

	// ===== PRP（Peer Relay Protocol）消息 =====
	MsgPeerExchange   // Peer Exchange: 把自己已知的节点列表发给邻居
	MsgRouteAdvertise // 路由广播：把自己可达的节点 (target, distance) 发给邻居
	MsgRelay          // 中继消息：把一个内层 Message 包起来，经多跳转发
)

// Message 通用消息包装
type Message struct {
	Type      MessageType `json:"type"`
	From      string      `json:"from"`       // 发送方 Node ID
	To        string      `json:"to"`         // 目标 Node ID，空=广播
	RequestID string      `json:"request_id"` // 请求 ID，用于匹配响应
	Timestamp int64       `json:"ts"`
	Payload   []byte      `json:"payload,omitempty"`
	Signature []byte      `json:"sig,omitempty"` // Ed25519(From+Type+Payload)
}

// ChatRequest 转发到远端节点的 LLM 请求（对应 providers.ChatRequest 的最小子集）
type ChatRequest struct {
	Model       string        `json:"model"`
	System      string        `json:"system,omitempty"`
	Messages    []ChatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float32       `json:"temperature,omitempty"`
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse 远端 LLM 响应
type ChatResponse struct {
	Content string `json:"content"`
	Usage   Usage  `json:"usage,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ===== 文件分片协议 =====

const (
	ChunkSize = 64 * 1024 * 1024 // 64MB
)

// FileManifest 文件元信息（包含 chunk hash 表）
type FileManifest struct {
	FileID    string   `json:"file_id"`   // 文件唯一 ID
	Name      string   `json:"name"`      // 文件名
	Size      int64    `json:"size"`      // 总大小
	ChunkSize int      `json:"chunk_size"`// 分片大小
	Chunks    []string `json:"chunks"`    // 每个 chunk 的 SHA256 hex
	Source    string   `json:"source"`    // 来源节点 ID
	CreatedAt time.Time `json:"created_at"`
}

// ChunkRequest 请求某文件的某 chunk
type ChunkRequest struct {
	FileID    string `json:"file_id"`
	ChunkIdx  int    `json:"chunk_idx"`
}

// ChunkResponse 分片数据
type ChunkResponse struct {
	FileID   string `json:"file_id"`
	ChunkIdx int    `json:"chunk_idx"`
	DataLen  int    `json:"data_len"`
	Hash     string `json:"hash"`
}

// DiscoveryAnnounce UDP 广播的宣告消息
type DiscoveryAnnounce struct {
	NodeID   string `json:"id"`
	Name     string `json:"name"`
	Addr     string `json:"addr"`      // TCP 地址（IP:Port）
	Version  string `json:"version"`
	Models   []string `json:"models,omitempty"`
	LoadPct  int    `json:"load_pct"`
	HasGPU   bool   `json:"has_gpu"`
	IsHotspot bool `json:"is_hotspot,omitempty"` // 是否扮演"个人热点/AP"
}

// ===== PRP 路由与节点交换 =====
//
// 核心思路（极简距离向量 DV）：
//   - 每个节点维护一张路由表：targetID -> (nextHopAddr, distance)
//   - 定期向邻居广播自己可达的 targets（MsgRouteAdvertise）
//   - 邻居收到后，如果发现"经过我→你→目标"比自己已知的路径更近，就更新路由表
//   - 当节点 A 想给节点 C 发消息，但直连不通时，走 A→B→C（MsgRelay）
//
// Peer Exchange（MsgPeerExchange）:
//   - 把我认识的节点（ID + Addr）发给新连上的邻居
//   - 邻居拿到后可以主动尝试直连，连不上就走路由表中继

// RouteEntry 一条路由表条目：到 target 的下一跳地址与跳数
type RouteEntry struct {
	Target   string `json:"target"`    // 目标 Node ID
	NextHop  string `json:"next_hop"`  // 下一跳 TCP 地址（IP:Port）
	Distance int    `json:"distance"`  // 跳数，1=直连
	Updated  int64  `json:"updated"`   // unix ts，用于过期
}

// RouteAdvertise 一个节点发给邻居的"我知道怎么到这些节点"
type RouteAdvertise struct {
	From    string       `json:"from"`    // 发送者 Node ID
	FromAddr string      `json:"from_addr"`// 发送者 TCP 地址
	Entries []RouteEntry `json:"entries"` // 我可达的 targets 列表（含到自己，distance=1）
}

// PeerExchange 节点把自己知道的 (NodeID, TCPAddr) 名单发给邻居
type PeerExchange struct {
	From  string          `json:"from"`
	Peers []PeerExchangeItem `json:"peers"`
}

// PeerExchangeItem PEX 中的单条记录
type PeerExchangeItem struct {
	ID   string `json:"id"`
	Addr string `json:"addr"`
	Name string `json:"name,omitempty"`
}

// RelayEnvelope 中继信封：把一条内部 Message 包裹起来，标明最终目的地
type RelayEnvelope struct {
	OriginalTo   string  `json:"original_to"`   // 最终目标 Node ID
	OriginalFrom string  `json:"original_from"` // 原始发送者
	TTL          int     `json:"ttl"`           // 剩余跳数上限，默认 8
	HopCount     int     `json:"hop_count"`     // 已经过的跳数
	Via          string  `json:"via"`           // 当前转发节点 Node ID（调试用）
	InnerType    MessageType `json:"inner_type"`
	InnerPayload []byte  `json:"inner_payload"` // 原始 Message.Payload（或完整 msg json）
	Path         []string `json:"path,omitempty"`// 经过的节点 ID 列表（调试）
}
