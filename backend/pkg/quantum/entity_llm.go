package quantum

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ghshhf/quantum-platform/backend/pkg/llm/providers"
)

// LLMEntity 是"一个 AI 大模型"本身作为 Entity。
//
// 它的 Match() 总是返回一个较高的基础分（因为 LLM 什么都能聊），
// 但在有更匹配的"专才 Entity"（比如文档、天气 API）时，
// Bridge 也会先调专才，再让 LLM 做总结。
//
// 它的 Execute() 直接调用 LLM Chat，让 AI 回答用户问题。
// 这样就实现了"免费 AI 综合体"的核心：Bridge 里只要有一个 LLMEntity，
// 用户就总是能得到 AI 回答。
type LLMEntity struct {
	name        string
	label       string
	provider    providers.Provider
	defaultHint string  // 给用户看的提示，如"无需 API Key"、"免费额度"
	alwaysOK    bool    // 如果 true，Match 永远返回正分；默认 true
	model       string  // 可选：覆盖 provider 默认模型
}

// NewLLMEntity 创建一个 AI 模型 Entity。
func NewLLMEntity(name, label string, provider providers.Provider, defaultModel string, hint string) *LLMEntity {
	return &LLMEntity{
		name:        name,
		label:       label,
		provider:    provider,
		defaultHint: hint,
		alwaysOK:    true,
		model:       defaultModel,
	}
}

func (l *LLMEntity) Profile() EntityProfile {
	desc := "一个大语言模型，能回答自然语言问题、写代码、做翻译、总结和推理。"
	if l.defaultHint != "" {
		desc += "（" + l.defaultHint + "）"
	}
	return EntityProfile{
		Name:        l.name,
		Label:       l.label,
		Kind:        KindModel,
		Description: desc,
		Keywords:    []string{"llm", "chat", "ai", "回答", "翻译", "代码", "code", "总结", "写作", "推理"},
		CreatedAt:   timeNow(),
	}
}

// Match 对 LLM Entity 永远返回一个"中庸"的分数。
// 逻辑：用户的问题可能同时匹配"文档"和"AI"，
// 我们让文档/connector 有更高的分数（它们更专业），
// LLM 作为"兜底"—— 当没有更匹配的 Entity 时由 LLM 兜底。
func (l *LLMEntity) Match(question string) float64 {
	q := strings.TrimSpace(question)
	if q == "" {
		return 0
	}
	// LLM 的基础匹配分：任何非空问题都给 0.18 保底
	// （低于一般文档关键词命中的分数），
	// 但问题本身像闲聊/问代码/总结时稍微加分。
	score := 0.18
	qLower := strings.ToLower(q)
	switch {
	case strings.Contains(qLower, "代码") || strings.Contains(qLower, "写个") ||
		strings.Contains(qLower, "function") || strings.Contains(qLower, "code"):
		score += 0.05
	case strings.Contains(qLower, "翻译") || strings.Contains(qLower, "translate"):
		score += 0.03
	case strings.Contains(qLower, "总结") || strings.Contains(qLower, "概述"):
		score += 0.03
	case strings.Contains(qLower, "你好") || strings.Contains(qLower, "hello") ||
		strings.Contains(qLower, "hi"):
		score += 0.02
	}
	return clamp01(score)
}

// Execute 调用底层 LLM Provider 回答问题。
// 我们把 Bridge 传过来的上下文(Entity fragments)一起塞给 LLM 做"引用来源"。
func (l *LLMEntity) Execute(ctx context.Context, query EntityQuery) EntityResult {
	profile := l.Profile()
	if l.provider == nil {
		return EntityResult{
			Profile: profile,
			Error:   "provider 未配置（未传入可用的 LLM Provider）",
		}
	}

	// 构造发给 LLM 的消息：
	//   system: 你的身份 + 引用格式要求
	//   context（可选）: 之前的对话
	//   user: 原始问题 + 可选"其他 Entity 提供的片段"
	content := query.Question
	if context := strings.TrimSpace(query.Context); context != "" {
		content = "（对话上下文）\n" + context + "\n\n（当前问题）\n" + query.Question
	}

	systemMsg := "你是一个多源信息整合助手。当收到问题时，回答请：\n" +
		"1) 简洁明了，直奔主题；\n" +
		"2) 如果调用了外部数据源（已在上下文中给出），请在回答末尾用 [来源: x] 的形式标注；\n" +
		"3) 对代码类问题提供完整可运行的代码片段（不要只写一半）。"

	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: content},
		},
		MaxTokens:   2048,
		Temperature: 0.5,
	}
	if l.model != "" {
		req.Model = l.model
	}

	resp, err := l.provider.Chat(ctx, req)
	if err != nil {
		return EntityResult{
			Profile: profile,
			Error:   err.Error(),
		}
	}

	frag := EntityFragment{
		EntityName: l.name,
		SourceRef:  profile.Label,
		Content:    resp.Content,
		Confidence: 0.9,
	}
	return EntityResult{
		Profile:   profile,
		Fragments: []EntityFragment{frag},
	}
}

// timeNow 方便测试：返回当前时间。
func timeNow() time.Time {
	return time.Now()
}

// ErrNoProvider 表示当前 Quantum 平台没配置可用的 LLM。
var ErrNoProvider = errors.New("no LLM provider configured")

// ProviderFromConfig 从一段极简的 "name/baseURL/apiKey/model" 配置生成一个 providers.Provider。
//
// 这是"免费 AI 接入层"的关键：所有 OpenAI-compatible 接口都用同一套 client，
// 只需要改 baseURL + apiKey + model 即可。
//
// 目前支持的"免费/可免费试用"的 OpenAI-compatible 接口形式：
//
//   1. 直接使用官方 OpenAI（需要 key）
//   2. 兼容 OpenAI 格式的第三方：
//      - DeepSeek: https://api.deepseek.com/v1
//      - SiliconFlow: https://api.siliconflow.cn/v1
//      - 零一万物/百川等，只要声明是 OpenAI Compatible，都能走这里
//   3. 本地 ollama（走 /v1 兼容端点）
//      - http://127.0.0.1:11434/v1
//
// 返回的 provider 直接可以注入 Bridge / LLMEntity。
func ProviderFromConfig(name, baseURL, apiKey, defaultModel string, extra map[string]string) (providers.Provider, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL 不能为空 (provider %q)", name)
	}
	cfg := providers.Config{
		Provider: name,
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Model:    defaultModel,
		Extra:    extra,
	}
	// 走 providers 注册表。如果 name 不在注册表中，
	// 用 "openai" 作为兜底（所有 OpenAI-compatible 都走它）。
	if providers.IsRegistered(name) {
		return providers.New(cfg)
	}
	// Fallback：用 openai 客户端发请求
	cfg.Provider = "openai"
	p, err := providers.New(cfg)
	if err != nil {
		return nil, err
	}
	// 给它一个自定义 Name()：让 Bridge 知道它是什么模型
	return withName{Provider: p, name: name}, nil
}

// withName 包装一个 Provider，把它的 Name() 覆写为自定义 name。
// （用于"不是标准 openai 但是 openai-compatible"的那些接口。）
type withName struct {
	providers.Provider
	name string
}

func (w withName) Name() string { return w.name }
