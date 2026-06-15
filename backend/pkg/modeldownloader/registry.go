package modeldownloader

// gb 返回 GB 到字节的转换（1024^3），整数化
func gb(v int64) int64 { return v * 1024 * 1024 * 1024 }

// builtinCatalog 内置常用模型目录
var builtinCatalog = []ModelCatalogEntry{
    {
        ID: "qwen2.5:7b", Name: "Qwen 2.5 (7B, q4_0)",
        Description: "阿里通义千问开源版本，中英文表现均衡，适合本地推理",
        Source: SourceOllama, Ref: "qwen2.5:7b",
        Size: gb(5), Quant: "q4_0", Family: "qwen", ParamCount: "7B",
        Files: []string{"qwen2.5:7b"},
    },
    {
        ID: "qwen2.5:3b", Name: "Qwen 2.5 (3B, q4_0)",
        Description: "轻量级本地模型，CPU 也能跑",
        Source: SourceOllama, Ref: "qwen2.5:3b",
        Size: gb(2), Quant: "q4_0", Family: "qwen", ParamCount: "3B",
        Files: []string{"qwen2.5:3b"},
    },
    {
        ID: "qwen2.5:14b", Name: "Qwen 2.5 (14B, q4_0)",
        Description: "强推理能力，适合复杂代码与长文本任务（需 8GB 显存）",
        Source: SourceOllama, Ref: "qwen2.5:14b",
        Size: gb(8), Quant: "q4_0", Family: "qwen", ParamCount: "14B",
        Files: []string{"qwen2.5:14b"},
    },
    {
        ID: "llama3.1:8b", Name: "Llama 3.1 (8B, q4_0)",
        Description: "Meta 发布的最新 Llama 系列模型，代码与推理能力出色",
        Source: SourceOllama, Ref: "llama3.1:8b",
        Size: gb(5), Quant: "q4_0", Family: "llama", ParamCount: "8B",
        Files: []string{"llama3.1:8b"},
    },
    {
        ID: "deepseek-coder:6.7b", Name: "DeepSeek Coder (6.7B)",
        Description: "专为代码而生，代码补全、解释、重构都很强",
        Source: SourceOllama, Ref: "deepseek-coder:6.7b",
        Size: gb(4), Quant: "q4_0", Family: "deepseek", ParamCount: "6.7B",
        Files: []string{"deepseek-coder:6.7b"},
    },
    {
        ID: "phi3:14b", Name: "Phi-3 Medium (14B)",
        Description: "微软出品的小模型王者，体积小但推理能力强",
        Source: SourceOllama, Ref: "phi3:14b",
        Size: gb(8), Quant: "q4_0", Family: "phi", ParamCount: "14B",
        Files: []string{"phi3:14b"},
    },
    {
        ID: "mistral:7b", Name: "Mistral (7B)",
        Description: "Mistral AI 开源的高效小模型",
        Source: SourceOllama, Ref: "mistral:7b",
        Size: gb(4), Quant: "q4_0", Family: "mistral", ParamCount: "7B",
        Files: []string{"mistral:7b"},
    },
    {
        ID: "gemma2:9b", Name: "Gemma 2 (9B)",
        Description: "Google 开源的 Gemma 系列，对话风格温和",
        Source: SourceOllama, Ref: "gemma2:9b",
        Size: gb(6), Quant: "q4_0", Family: "gemma", ParamCount: "9B",
        Files: []string{"gemma2:9b"},
    },
}

// builtinCatalogIndex 按 ID 索引（由 init 构建）
var builtinCatalogIndex = make(map[string]ModelCatalogEntry)

func init() {
    for _, e := range builtinCatalog {
        builtinCatalogIndex[e.ID] = e
    }
}

// catalogIDs 返回所有已注册模型 ID
func catalogIDs() []string {
    ids := make([]string, 0, len(builtinCatalog))
    for _, e := range builtinCatalog { ids = append(ids, e.ID) }
    return ids
}
