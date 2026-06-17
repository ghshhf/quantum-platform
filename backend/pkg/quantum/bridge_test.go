package quantum_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ghshhf/quantum-platform/backend/pkg/connector"
	"github.com/ghshhf/quantum-platform/backend/pkg/llm/providers"
	"github.com/ghshhf/quantum-platform/backend/pkg/quantum"
)

// ---------- 测试辅助：模拟 AI provider ----------

type mockProvider struct {
	name string
}

func (m mockProvider) Name() string { return m.name }
func (m mockProvider) Chat(ctx context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	lastMsg := ""
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			lastMsg = msg.Content
		}
	}
	return &providers.ChatResponse{
		Content: "【AI综合回答】 " + lastMsg,
		Usage:   providers.Usage{PromptTokens: 10, CompletionTokens: 10, TotalTokens: 20},
	}, nil
}
func (m mockProvider) StreamChat(ctx context.Context, req providers.ChatRequest) (<-chan providers.StreamChunk, error) {
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan providers.StreamChunk, 1)
	ch <- providers.StreamChunk{Delta: resp.Content, Done: true}
	close(ch)
	return ch, nil
}
func (m mockProvider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	return []providers.ModelInfo{{Name: "mock-1"}}, nil
}

// ---------- 测试 1：Document Entity ----------

func TestBridge_DocumentEntity(t *testing.T) {
	doc := `
量子平台（Quantum Platform）是一个"无视格式接口差异，
任意产生数据交互的主体可瞬间双向调用"的系统。

它包含三个核心概念：
  1. Entity：一个可对话的主体（文档、API、终端设备）
  2. Bridge：中枢调度器，自动选择最合适的 Entity
  3. Platform：用户侧的 Entity 集合

使用方式：先注册几个 Entity 到 Platform，然后向 Platform 提问。
`
	docEntity := quantum.NewDocumentEntity(
		"doc-quantum-intro",
		"量子平台介绍文档",
		"介绍量子平台核心概念的文档",
		doc,
		200,
		50,
	)

	bridge := quantum.NewBridge(nil, quantum.DefaultSessionConfig())
	sess := bridge.NewSession(uuid.Nil, []quantum.Entity{docEntity})

	ans, err := bridge.Ask(context.Background(), sess, "量子平台的 Entity Bridge Platform 核心概念是什么？")
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	if !ans.Answered {
		t.Fatalf("Expected Answered=true")
	}
	t.Logf("Answer: %s", ans.Content)

	if len(ans.InvokedEntities) == 0 {
		t.Errorf("Expected at least 1 invoked entity")
	}
}

// ---------- 测试 2：多 Entity 智能选择（文档 vs API） ----------

func TestBridge_MultipleEntities(t *testing.T) {
	docEntity := quantum.NewDocumentEntity(
		"product-manual",
		"产品手册",
		"产品定价、功能介绍",
		"产品定价：基础版 99 元/月，专业版 299 元/月。支持 API 调用。",
		200, 50,
	)

	connRT := connector.NewRuntime(nil)
	weatherSpec := &connector.ConnectorSpec{
		Name:       "weather-api",
		Label:      "天气查询 API",
		Type:       "http",
		BaseURL:    "https://example.invalid",
		Auth:       connector.AuthScheme{Type: "none"},
		Actions: []connector.ActionSpec{
			{
				Action: "get_weather",
				Label:  "查询天气",
				Method: "GET",
				Path:   "/weather",
				Params: []connector.ParamSchema{
					{Name: "city", Label: "城市", Type: "string", In: "query", Required: true},
				},
			},
		},
	}
	connEntity := quantum.NewConnectorEntity(connRT, weatherSpec, nil)

	bridge := quantum.NewBridge(nil, quantum.DefaultSessionConfig())
	sess := bridge.NewSession(uuid.Nil, []quantum.Entity{docEntity, connEntity})

	// Q1：价格问题 → 命中 product-manual
	ans1, err := bridge.Ask(context.Background(), sess, "产品的专业版价格是多少？")
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	t.Logf("Price Q invoked: %v, answer: %s", ans1.InvokedEntities, ans1.Content)
	foundPrice := false
	for _, n := range ans1.InvokedEntities {
		if n == "product-manual" {
			foundPrice = true
			break
		}
	}
	if !foundPrice {
		t.Errorf("Expected product-manual to be invoked for pricing question, got %v", ans1.InvokedEntities)
	}

	// Q2：天气问题 → 命中 weather-api
	ans2, err := bridge.Ask(context.Background(), sess, "查询北京天气")
	if err != nil {
		t.Fatalf("Ask2 failed: %v", err)
	}
	t.Logf("Weather Q invoked: %v, answer: %s", ans2.InvokedEntities, ans2.Content)
	foundWeather := false
	for _, n := range ans2.InvokedEntities {
		if n == "weather-api" {
			foundWeather = true
			break
		}
	}
	if !foundWeather {
		t.Errorf("Expected weather-api to be invoked for weather query, got %v", ans2.InvokedEntities)
	}
}

// ---------- 测试 3：统一综合体（文档 + API + AI 三件套） ----------

func TestBridge_UnifiedSynthesis(t *testing.T) {
	// 1. 文档 Entity（产品价格）
	docEntity := quantum.NewDocumentEntity(
		"pricing-doc",
		"产品价格文档",
		"",
		"基础版 99 元/月，专业版 299 元/月。API key 在 /settings/api-key 页面生成。",
		200, 50,
	)

	// 2. LLMEntity（扮演"免费 AI 综合器"）
	mockLLM := mockProvider{name: "mock-deepseek"}
	llmEntity := quantum.NewLLMEntity("ai-assistant", "免费 AI 助手", mockLLM, "mock-model", "模拟额度无限")

	bridge := quantum.NewBridge(nil, quantum.DefaultSessionConfig())
	sess := bridge.NewSession(uuid.Nil, []quantum.Entity{docEntity, llmEntity})

	ans, err := bridge.Ask(context.Background(), sess, "专业版多少钱？")
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	if !ans.Answered {
		t.Fatal("Expected answered=true")
	}
	t.Logf("Answer: %s", ans.Content)

	// 关键断言：回答应当包含 AI 做过的信息整合
	if !strings.Contains(ans.Content, "AI") && !strings.Contains(ans.Content, "专业") {
		t.Errorf("Expected AI-generated summary mentioning pricing, got: %s", ans.Content)
	}
	if len(ans.InvokedEntities) == 0 {
		t.Errorf("Expected at least 1 entity invoked")
	}
}

// ---------- 测试 4：免费 AI 目录 ----------

func TestFreeProviderCatalogue(t *testing.T) {
	catalogue := quantum.FreeProviderCatalogue()
	if len(catalogue) == 0 {
		t.Fatal("Expected non-empty catalog")
	}
	hasDeepSeek := false
	for _, p := range catalogue {
		if p.Name == "deepseek" {
			hasDeepSeek = true
		}
		if p.BaseURL == "" || p.DefaultModel == "" {
			t.Errorf("Provider %q has empty baseURL/model", p.Name)
		}
		t.Logf("  - %s (%s) auth=%s url=%s model=%s", p.Name, p.Label, p.AuthMode, p.BaseURL, p.DefaultModel)
	}
	if !hasDeepSeek {
		t.Error("Expected 'deepseek' in free provider catalog")
	}

	if _, ok := quantum.FindFreeProviderByName("deepseek"); !ok {
		t.Error("Expected deepseek to be findable by name")
	}
	if _, ok := quantum.FindFreeProviderByName("no-such"); ok {
		t.Error("Expected unknown provider not to be found")
	}
}
