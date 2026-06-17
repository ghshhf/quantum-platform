// Package quantum 实现"量子平台"的核心概念：
//
//   - Entity（量子节点）：任何能产生数据交互的主体
//     （文档、数据库、API、代码仓库、另一个用户…）
//   - Bridge（超距交互中枢）：接受用户的一句话，
//     自动在相关 Entity 之间建立连接，
//     并行收集数据后生成最终回答
//
// 设计要点：
//  1. Entity 统一接口：无论底层是什么格式（JSON/网页/CLI/文件），
//     对上层都是"能回答一个问题的主体"
//  2. Bridge 自动调度：用户不需要"选工具"、"填参数"，
//     Bridge 会根据每个 Entity 的 Profile 决定谁应该参与此次对话
//  3. 可扩展：新增一种 Entity 类型（如"数据库"、"网页"）
//     只需要实现 Entity 接口并注册，无需修改 Bridge 逻辑
package quantum

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// EntityKind 描述 Entity 的类型（给 Bridge 做调度决策用）
type EntityKind string

const (
	KindDocument  EntityKind = "document"   // 文档/文件/知识库
	KindConnector EntityKind = "connector"  // 公网 API / 外部服务
	KindDatabase  EntityKind = "database"   // 数据库
	KindWebpage   EntityKind = "webpage"    // 网页（web agent）
	KindTerminal  EntityKind = "terminal"   // CLI / 终端
	KindUser      EntityKind = "user"       // 另一个用户 / 团队成员
	KindModel     EntityKind = "model"      // AI 模型本身（纯推理，无外部数据）
)

// EntityProfile 是一个 Entity 暴露给 Bridge 的"自我介绍"。
// Bridge 根据 profile 决定：这个 Entity 能回答什么问题？
type EntityProfile struct {
	Name        string       // 唯一标识，如 "product-manual-v2.pdf"
	Label       string       // 展示名
	Kind        EntityKind   // 类型
	Description string       // 1-2 句话，告诉 Bridge 这个主体包含什么 / 能做什么
	Keywords    []string     // 关键词，用于粗粒度匹配
	// 元数据，不同类型有不同字段
	Metadata    map[string]string
	CreatedAt   time.Time
}

// EntityQuery 描述一次对 Entity 的调用
type EntityQuery struct {
	Question string           // 用户的原始问题
	Intent   string           // Bridge 判定的意图关键词（如 "summarize" / "query:price"）
	Context  string           // 已有的对话上下文（可空）
	TopK     int              // 需要返回几段内容（文档类 Entity 用）
	Params   map[string]any   // 额外参数（不同类型 Entity 自定）
}

// EntityFragment 是 Entity 返回的"一段信息"
// Bridge 收集多个 Entity 的 fragments，拼成最终答案
type EntityFragment struct {
	EntityName  string      // 哪个 Entity 返回的
	SourceRef   string      // 在该 Entity 内部的位置标识（如页码/URL/命令名）
	Content     string      // 提取到的信息（原文/回答）
	Confidence  float64     // 0~1，这段信息与问题的相关度（由 Entity 自评估）
	Raw         any         // 原始数据（可选，调试用）
}

// EntityResult 是一次 Entity.Execute 的完整返回
type EntityResult struct {
	Profile   EntityProfile    // 哪个 Entity 响应的
	Fragments []EntityFragment // 返回的信息片段
	LatencyMs int64            // 耗时
	Error     string           // 失败原因（空 = 成功）
}

// Entity 是"量子节点"的统一接口。
//
// 无论底层是一份 PDF、一个 REST API、一个 SQL 数据库、一个 CLI 工具，
// 它对外只暴露三件事：Profile、Match、Execute。
//
// 核心抽象：所有产生数据的东西都能"回答一个问题"。
type Entity interface {
	// Profile 返回这个 Entity 的自我介绍（给 Bridge 决策用）
	Profile() EntityProfile

	// Match 判断这个 Entity 与给定问题的相关度（0~1）
	// 返回值越高，Bridge 越可能调用它
	Match(question string) float64

	// Execute 让这个 Entity 针对 query 产生信息片段
	// 返回若干片段（可能为空），供 Bridge 汇总
	Execute(ctx context.Context, query EntityQuery) EntityResult
}

// Session 表示一次"多 Entity 对话"
//
// 它持有：
//   - 对话历史（messages）
//   - 当前激活的 Entity 列表
//   - Bridge 用于判定、汇总、生成
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Entities  []Entity
	Messages  []Message
	CreatedAt time.Time
}

type Message struct {
	Role       string    // "user" / "assistant" / "entity-report"
	Content    string
	EntityName string    // 如果是某个 Entity 的报告，填其 name
	At         time.Time
}

// SessionConfig Bridge 一次会话的配置
type SessionConfig struct {
	MaxEntitiesPerQuery int      // 一次问题最多并行调用几个 Entity（默认 3）
	MinMatchScore       float64  // 低于此分数的 Entity 不参与（默认 0.1）
	DisableEntities     []string // 指定禁用某些 Entity（调试用）
}

func DefaultSessionConfig() SessionConfig {
	return SessionConfig{
		MaxEntitiesPerQuery: 3,
		MinMatchScore:       0.1,
	}
}
