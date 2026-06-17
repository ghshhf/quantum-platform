package quantum

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ghshhf/MonkeyCode/backend/pkg/llm/providers"
)

// Bridge 是"量子超距交互"的中枢。
//
// 它负责：
//  1. 持有一个 Session：当前激活的 Entity 列表 + 对话历史
//  2. 对用户每句话做"意图理解"，挑选最合适的 Entity 子集
//  3. 并行调用这些 Entity（无视格式/接口差异 —— 它们都实现了统一接口）
//  4. 把各 Entity 返回的片段汇总，交给 LLM 生成最终回答
//
// 这就是"无视格式、接口差异，任意产生数据交互的主体可瞬间双向调用"的工程体现：
// Bridge 看到的 Entity 列表 = 当前"量子态"，用户的问题 = "观测"，
// Bridge 从这些 Entity 中挑选"最相关"的若干个并行"坍缩" —— 即调用其 Execute。
type Bridge struct {
	llm       providers.Provider
	config    SessionConfig
}

// NewBridge 创建一个 Bridge。llm 可以为 nil（此时只做 Entity 结果的简单拼接）。
func NewBridge(llm providers.Provider, cfg SessionConfig) *Bridge {
	return &Bridge{
		llm:    llm,
		config: cfg,
	}
}

// NewSession 创建一个新会话。
func (b *Bridge) NewSession(userID uuid.UUID, entities []Entity) *Session {
	return &Session{
		ID:        uuid.New(),
		UserID:    userID,
		Entities:  entities,
		Messages:  []Message{},
		CreatedAt: time.Now(),
	}
}

// Answer 是 Bridge 对一句话的完整响应
type Answer struct {
	Answered        bool             // 是否给出了有信息的回答
	Content         string           // 最终回答（LLM 汇总后）
	InvokedEntities []string         // 本次调用了哪些 Entity
	EntityResults   []EntityResult   // 各 Entity 的原始结果（方便前端展示"引用来源"）
	LatencyMs       int64            // 总耗时
}

// Ask 对一句话生成回答。
//
// 核心流程（超距交互）：
//   1. 对当前会话的每个 Entity 调用 Match(question) 打分
//   2. 取分数最高的 Top N 个（由 config.MaxEntitiesPerQuery 控制）
//   3. 并行调用它们的 Execute
//   4. 把所有 Entity 返回的片段交给 LLM 做"汇总成自然语言回答"
func (b *Bridge) Ask(ctx context.Context, session *Session, question string) (*Answer, error) {
	start := time.Now()

	if session == nil || len(session.Entities) == 0 {
		return &Answer{
			Answered:  false,
			Content:   "当前量子平台上没有可交互的主体，请先注册一些 Entity（文档、API 服务、数据源…）。",
			LatencyMs: time.Since(start).Milliseconds(),
		}, nil
	}

	// —— 步骤 1：对每个 Entity 打分 ——
	type scored struct {
		entity Entity
		score  float64
	}
	scores := make([]scored, 0, len(session.Entities))
	for _, e := range session.Entities {
		if b.isDisabled(e.Profile().Name) {
			continue
		}
		s := e.Match(question)
		if s >= b.config.MinMatchScore {
			scores = append(scores, scored{e, s})
		}
	}

	// 按得分降序取 top N（冒泡：小数据集，简单直接）
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[i].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}
	limit := b.config.MaxEntitiesPerQuery
	if limit <= 0 {
		limit = 3
	}
	if len(scores) > limit {
		scores = scores[:limit]
	}

	if len(scores) == 0 {
		return &Answer{
			Answered:  false,
			Content:   "没有找到与当前问题直接相关的主体。你可以添加更多的 Entity（文档、API、数据源），或者换一种问法。",
			LatencyMs: time.Since(start).Milliseconds(),
		}, nil
	}

	// —— 步骤 2：并行调用选中的 Entity ——
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make([]EntityResult, 0, len(scores))
	)
	for _, sc := range scores {
		wg.Add(1)
		go func(e Entity, sc float64) {
			defer wg.Done()
			_ = sc
			q := EntityQuery{
				Question: question,
				Intent:   extractIntent(question),
				Context:  buildContext(session),
				TopK:     3,
			}
			res := e.Execute(ctx, q)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(sc.entity, sc.score)
	}
	wg.Wait()

	// —— 步骤 3：汇总 Entity 片段，生成最终回答 ——
	invoked := make([]string, 0, len(results))
	for _, r := range results {
		invoked = append(invoked, r.Profile.Name)
	}

	content, err := b.synthesizeAnswer(ctx, question, results, session)
	if err != nil {
		// LLM 失败时，降级为原始片段拼接，避免整个系统挂
		content = synthesizeFallback(question, results)
	}

	// 写入对话历史
	session.Messages = append(session.Messages, Message{
		Role:    "user",
		Content: question,
		At:      time.Now(),
	})
	session.Messages = append(session.Messages, Message{
		Role:    "assistant",
		Content: content,
		At:      time.Now(),
	})

	return &Answer{
		Answered:        true,
		Content:         content,
		InvokedEntities: invoked,
		EntityResults:   results,
		LatencyMs:       time.Since(start).Milliseconds(),
	}, nil
}

// ---------- 内部辅助 ----------

func (b *Bridge) isDisabled(name string) bool {
	for _, d := range b.config.DisableEntities {
		if d == name {
			return true
		}
	}
	return false
}

// extractIntent 从问题中抽取出一个粗粒度的意图关键词。
// 目前用非常简单的启发式（关键词识别），未来可以替换成 LLM 的 intent classification。
func extractIntent(q string) string {
	q = strings.ToLower(q)
	switch {
	case strings.Contains(q, "总结") || strings.Contains(q, "summary") || strings.Contains(q, "概述"):
		return "summarize"
	case strings.Contains(q, "查") || strings.Contains(q, "找") || strings.Contains(q, "search"):
		return "search"
	case strings.Contains(q, "对比") || strings.Contains(q, "compare") || strings.Contains(q, "区别"):
		return "compare"
	case strings.Contains(q, "价格") || strings.Contains(q, "多少钱") || strings.Contains(q, "price"):
		return "query:price"
	case strings.Contains(q, "代码") || strings.Contains(q, "code") || strings.Contains(q, "函数"):
		return "query:code"
	default:
		return "general"
	}
}

// buildContext 把最近的对话历史拼成一小段文本，供各 Entity 做上下文感知。
func buildContext(s *Session) string {
	if len(s.Messages) == 0 {
		return ""
	}
	// 取最近 3 轮
	start := 0
	if len(s.Messages) > 6 {
		start = len(s.Messages) - 6
	}
	var sb strings.Builder
	for _, m := range s.Messages[start:] {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	return sb.String()
}

// synthesizeAnswer 汇总多来源信息，生成自然语言回答。
//
// 优先级：
//  1. 如果 session 里注册了 LLMEntity（用户自己添加的免费 AI）→ 用它做总结
//  2. 否则如果 Bridge 初始化时带了全局 provider → 用它
//  3. 否则 fallback：纯片段拼接（不依赖任何 AI）
func (b *Bridge) synthesizeAnswer(ctx context.Context, question string, results []EntityResult, session *Session) (string, error) {
	// 1) 从 session 里找第一个 LLMEntity（用户自己注册的 AI）
	for _, e := range session.Entities {
		if llmE, ok := e.(*LLMEntity); ok {
			// 让它做"信息整合"：把所有 Entity 的结果当上下文
			frags := buildFragments(results)
			query := EntityQuery{
				Question: "请根据以下信息，回答我的问题：" + question,
				Context:  frags,
			}
			res := llmE.Execute(ctx, query)
			if len(res.Fragments) > 0 {
				return res.Fragments[0].Content, nil
			}
			if res.Error != "" {
				return "", fmtError("llm(%s): %s", llmE.name, res.Error)
			}
		}
	}
	// 2) 全局 provider（老方式）
	if b.llm != nil {
		var sb strings.Builder
		sb.WriteString("以下是多个数据源（Entity）针对问题返回的信息片段。\n\n")
		sb.WriteString("用户问题: ")
		sb.WriteString(question)
		sb.WriteString("\n\n各数据源返回的内容:\n\n")
		for i, r := range results {
			sb.WriteString(fmt.Sprintf("【来源 %d：%s (%s)】\n", i+1, r.Profile.Label, r.Profile.Name))
			if r.Error != "" {
				sb.WriteString("  (失败: ")
				sb.WriteString(r.Error)
				sb.WriteString(")\n")
				continue
			}
			for _, frag := range r.Fragments {
				sb.WriteString("  - ")
				if frag.SourceRef != "" {
					sb.WriteString("[")
					sb.WriteString(frag.SourceRef)
					sb.WriteString("] ")
				}
				sb.WriteString(frag.Content)
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("请基于以上信息，以自然语言的方式回答用户的问题。")
		sb.WriteString("如果多个来源的信息有冲突，请在回答中明确指出。")
		sb.WriteString("请保持回答简洁，并尽量引用来源标识。\n")
		req := providers.ChatRequest{
			Messages: []providers.Message{
				{Role: "system", Content: "你是一个数据整合助手，能综合多个来源的信息生成连贯的回答。"},
				{Role: "user", Content: sb.String()},
			},
			MaxTokens:   1500,
			Temperature: 0.3,
		}
		resp, err := b.llm.Chat(ctx, req)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
	// 3) 无 AI：降级为纯片段拼接
	return synthesizeFallback(question, results), nil
}

func fmtError(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

// buildFragments 把 EntityResults 整理成一段给 LLM 看的上下文文字。
func buildFragments(results []EntityResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("（已调用的信息源：\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("  [%d] %s / %s\n", i+1, r.Profile.Label, r.Profile.Name))
		if r.Error != "" {
			sb.WriteString("      错误: ")
			sb.WriteString(r.Error)
			sb.WriteString("\n")
			continue
		}
		for _, frag := range r.Fragments {
			sb.WriteString("      ")
			if frag.SourceRef != "" {
				sb.WriteString("[")
				sb.WriteString(frag.SourceRef)
				sb.WriteString("] ")
			}
			sb.WriteString(frag.Content)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("）\n")
	return sb.String()
}

// synthesizeFallback 当 LLM 不可用时的降级回答：直接拼接各 Entity 返回的片段。
func synthesizeFallback(question string, results []EntityResult) string {
	var sb strings.Builder
	sb.WriteString("针对你的问题：\"")
	sb.WriteString(question)
	sb.WriteString("\"，以下是各主体返回的信息：\n\n")
	for _, r := range results {
		sb.WriteString("◆ ")
		sb.WriteString(r.Profile.Label)
		sb.WriteString(" (")
		sb.WriteString(r.Profile.Name)
		sb.WriteString(")\n")
		if r.Error != "" {
			sb.WriteString("  (调用失败: ")
			sb.WriteString(r.Error)
			sb.WriteString(")\n")
			continue
		}
		if len(r.Fragments) == 0 {
			sb.WriteString("  (无相关信息)\n")
			continue
		}
		for _, frag := range r.Fragments {
			sb.WriteString("  · ")
			if frag.SourceRef != "" {
				sb.WriteString("[")
				sb.WriteString(frag.SourceRef)
				sb.WriteString("] ")
			}
			sb.WriteString(frag.Content)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
