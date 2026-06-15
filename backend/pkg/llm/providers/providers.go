package providers

import "context"

// Provider 定义统一的 LLM Provider 接口
// 所有具体厂商（OpenAI/Anthropic/Gemini/DeepSeek/Ollama/自定义）都实现此接口
type Provider interface {
	// Name 返回 provider 标识，如 "openai"、"anthropic"、"gemini"、"deepseek"、"ollama"、"custom"
	Name() string

	// Chat 发送聊天请求，返回统一格式响应
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)

	// StreamChat 流式聊天（SSE），通过 channel 返回流式 chunks
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)

	// ListModels 返回该 provider 可用模型列表
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// ChatRequest 统一请求格式
type ChatRequest struct {
	Messages    []Message `json:"messages"`
	Model       string    `json:"model,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float32   `json:"temperature,omitempty"`
	System      string    `json:"system,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatResponse 统一响应
type ChatResponse struct {
	Content string `json:"content"`
	Usage   Usage  `json:"usage"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk 流式响应的一个片段
type StreamChunk struct {
	Delta string `json:"delta"`
	Done  bool   `json:"done"`
	Error string `json:"error,omitempty"`
}

// ModelInfo 模型元信息
type ModelInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Context int    `json:"context_length"`
	Owner   string `json:"owner,omitempty"`
}

// Config 构造 Provider 的配置
type Config struct {
	Provider string // "openai" | "anthropic" | "gemini" | "deepseek" | "ollama" | "custom"
	BaseURL  string
	APIKey   string
	Model    string // 默认模型
	Extra    map[string]string
}
