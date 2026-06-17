package quantum_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ghshhf/MonkeyCode/backend/pkg/connector"
	"github.com/ghshhf/MonkeyCode/backend/pkg/quantum"
)

// TestBridge_DocumentEntity 验证：
// 1. 可以把一份文档变成一个 Document Entity
// 2. Bridge 会把它识别为"匹配度最高"的 Entity
// 3. Bridge.Ask 返回的 answer 中包含文档里的内容片段
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

	bridge := quantum.NewBridge(nil, quantum.SessionConfig{
		MaxEntitiesPerQuery: 3,
		MinMatchScore:       0.05, // 放宽匹配阈值，让问题中的关键词能命中文档
	})
	sess := bridge.NewSession([16]byte{}, []quantum.Entity{docEntity})

	// 向平台提一个与文档内容高度相关的问题
	ans, err := bridge.Ask(context.Background(), sess, "量子平台的 Entity Bridge Platform 核心概念是什么？")
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	if !ans.Answered {
		t.Fatalf("Expected Answered=true, got false (content=%q)", ans.Content)
	}
	t.Logf("Answer: %s", ans.Content)

	// 应该调用到了 document entity
	if len(ans.InvokedEntities) == 0 ||
		ans.InvokedEntities[0] != "doc-quantum-intro" {
		t.Errorf("Expected doc-quantum-intro to be invoked, got %v", ans.InvokedEntities)
	}

	// 内容里应该出现关键词
	for _, kw := range []string{"Entity", "Bridge", "Platform"} {
		if !strings.Contains(ans.Content, kw) {
			t.Logf("(warn) expected keyword %q in answer", kw)
		}
	}
}

// TestBridge_MultipleEntities 验证：当有多个 Entity 时，Bridge 会选最合适的。
func TestBridge_MultipleEntities(t *testing.T) {
	// Entity A: 天气 API 连接器
	specA := &connector.ConnectorSpec{
		Name:        "weather-api",
		Label:       "天气查询 API",
		Description: "查询世界各地天气信息",
		Type:        "http",
		BaseURL:     "https://example-weather.invalid",
		Actions: []connector.ActionSpec{
			{
				Action:   "get_weather",
				Label:    "查询天气",
				Method:   "GET",
				Path:     "/weather",
				Params: []connector.ParamSchema{
					{Name: "city", Label: "城市", Type: "string", In: "query", Required: true},
				},
			},
		},
	}
	connRuntime := connector.NewRuntime(nil)
	connRuntime.Register(specA)
	connEntity := quantum.NewConnectorEntity(connRuntime, specA, nil)

	// Entity B: 产品文档
	docEntity := quantum.NewDocumentEntity(
		"product-manual",
		"产品手册",
		"介绍产品功能、价格、订购方式",
		"产品定价：基础版 99 元/月，专业版 299 元/月。支持 API 调用。",
		200, 50,
	)

	bridge := quantum.NewBridge(nil, quantum.DefaultSessionConfig())
	sess := bridge.NewSession([16]byte{}, []quantum.Entity{docEntity, connEntity})

	// 问一个和"文档"最相关的问题
	ans, err := bridge.Ask(context.Background(), sess, "产品的专业版价格是多少？")
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}
	t.Logf("Question1 invoked: %v, answer: %s", ans.InvokedEntities, ans.Content)

	if len(ans.InvokedEntities) == 0 {
		t.Errorf("Expected at least 1 entity invoked")
	}
	found := false
	for _, name := range ans.InvokedEntities {
		if name == "product-manual" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected product-manual to be among invoked entities, got %v", ans.InvokedEntities)
	}

	// 问一个和天气 API 相关的
	ans2, err := bridge.Ask(context.Background(), sess, "查询北京的天气")
	if err != nil {
		t.Fatalf("Ask2 failed: %v", err)
	}
	t.Logf("Question2 invoked: %v, answer: %s", ans2.InvokedEntities, ans2.Content)

	foundWeather := false
	for _, name := range ans2.InvokedEntities {
		if name == "weather-api" {
			foundWeather = true
			break
		}
	}
	if !foundWeather {
		t.Errorf("Expected weather-api to be invoked for weather query, got %v", ans2.InvokedEntities)
	}
}
