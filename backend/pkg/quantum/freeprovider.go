package quantum

import (
	"strings"
)

// FreeProviderSpec 描述一个"免费可用/可免费试用"的 LLM 接入点。
// 所有接入点都走 OpenAI-compatible 的 HTTP 接口。
//
// 理念：用户只需要填 API Key（有的免费额度内无需 key），就能自动获得
// 一套"可对话的 LLM Entity"集合 — 不需要自己去找各个平台的 URL。
type FreeProviderSpec struct {
	Name          string   // 唯一标识，如 "deepseek-chat"
	Label         string   // 显示名，如 "DeepSeek 深度求索"
	BaseURL       string   // OpenAI-compatible 端点（/chat/completions 会追加在此后）
	DefaultModel  string   // 默认模型 ID
	AuthMode      string   // "apikey"（需要 key）| "free_no_key"（免费无需 key）| "bearer"
	FreeTier      string   // 描述免费额度，给用户看的说明
	StrengthTags  []string // 能力标签，如 "写代码"、"中文"、"推理"、"数学"
	Note          string   // 备注（如 "国内可用"、"需要申请"）
}

// FreeProviderCatalogue 是我们整理好的"免费可用"列表。
//
// 设计原则：
//   1. 全部都是 OpenAI-compatible（无需为每家厂商写特定 client）
//   2. 标注清楚"是否需要 API Key、是否免费、额度多少"
//   3. 让用户在前端 UI 里挑一个/几个、粘贴 API Key、立即能用
//
// 注意：URL 和 Model 可能会变，
// 建议用户随时可以在 UI 里编辑或者追加自己的条目。
func FreeProviderCatalogue() []FreeProviderSpec {
	return []FreeProviderSpec{
		// DeepSeek —— 注册送免费额度，Chat/代码能力强
		{
			Name:         "deepseek",
			Label:        "DeepSeek 深度求索",
			BaseURL:      "https://api.deepseek.com/v1",
			DefaultModel: "deepseek-chat",
			AuthMode:     "apikey",
			FreeTier:     "新用户赠送额度（人民币 ~500 万 token）",
			StrengthTags: []string{"写代码", "中文", "推理", "数学"},
			Note:         "国内可直接访问；代码和推理能力强",
		},

		// SiliconFlow（硅基流动）—— 兼容 OpenAI，支持大量开源模型
		{
			Name:         "siliconflow",
			Label:        "硅基流动 SiliconFlow",
			BaseURL:      "https://api.siliconflow.cn/v1",
			DefaultModel: "Qwen/Qwen2.5-7B-Instruct",
			AuthMode:     "apikey",
			FreeTier:     "新用户送免费额度，部分小模型免费",
			StrengthTags: []string{"中文", "开源模型", "多模型"},
			Note:         "可切换数百个开源模型",
		},

		// Groq —— 推理速度极快，有免费试用
		{
			Name:         "groq",
			Label:        "Groq",
			BaseURL:      "https://api.groq.com/openai/v1",
			DefaultModel: "llama-3.1-70b-versatile",
			AuthMode:     "apikey",
			FreeTier:     "免费额度限制 tokens/秒",
			StrengthTags: []string{"速度快", "英文", "通用"},
			Note:         "速度极快；主要服务英文",
		},

		// 火山方舟（字节）—— 国内可用，有免费试用
		{
			Name:         "ark",
			Label:        "字节跳动 · 火山方舟",
			BaseURL:      "https://ark.cn-beijing.volces.com/api/v3",
			DefaultModel: "doubao-lite-4k",
			AuthMode:     "apikey",
			FreeTier:     "新用户赠送 tokens",
			StrengthTags: []string{"中文", "国内可用", "多模型"},
			Note:         "doubao-lite 等豆包系列可免费试",
		},

		// 通义千问 —— 国内可用
		{
			Name:         "qwen",
			Label:        "阿里 · 通义千问",
			BaseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
			DefaultModel: "qwen-plus",
			AuthMode:     "apikey",
			FreeTier:     "新用户赠送额度",
			StrengthTags: []string{"中文", "国内可用"},
			Note:         "DashScope 兼容 OpenAI 格式",
		},

		// 智谱清言 / ChatGLM —— 国内可用
		{
			Name:         "zhipu",
			Label:        "智谱 AI · GLM",
			BaseURL:      "https://open.bigmodel.cn/api/paas/v4",
			DefaultModel: "glm-4-flash",
			AuthMode:     "apikey",
			FreeTier:     "每日免费调用，或赠送额度",
			StrengthTags: []string{"中文", "国内可用", "免费"},
			Note:         "glm-4-flash 为免费模型",
		},

		// 本地 Ollama（作为兜底/纯本地隐私模式）
		{
			Name:         "ollama-local",
			Label:        "Ollama 本地模型",
			BaseURL:      "http://127.0.0.1:11434/v1",
			DefaultModel: "llama3",
			AuthMode:     "free_no_key",
			FreeTier:     "完全本地运行，零成本",
			StrengthTags: []string{"本地", "隐私", "免费", "需要 GPU"},
			Note:         "需要本机先安装 Ollama 并 pull 模型",
		},
	}
}

// FindFreeProviderByName 在目录里按 name 查找。
func FindFreeProviderByName(name string) (FreeProviderSpec, bool) {
	for _, p := range FreeProviderCatalogue() {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return FreeProviderSpec{}, false
}
