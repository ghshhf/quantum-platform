package connector_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ghshhf/quantum-platform/backend/pkg/connector"
)

// TestHTTPExecutor_Basic 测试 HTTP 执行后端的基本功能：
// 启动一个模拟 HTTP 服务器，构造一个 ConnectorSpec，
// 通过 Runtime.Execute 调用并验证结果。
func TestHTTPExecutor_Basic(t *testing.T) {
	// 1. 启动模拟 HTTP 服务器：接受 GET /echo?name=xxx -> { "hello": "xxx" }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo" {
			name := r.URL.Query().Get("name")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"greeting": "hello " + name,
				"origin":   r.Host,
			})
			return
		}
		if r.URL.Path == "/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "sampletoken-1234",
			})
			return
		}
		if r.URL.Path == "/secure" {
			if r.Header.Get("Authorization") == "Bearer sampletoken-1234" {
				_ = json.NewEncoder(w).Encode(map[string]any{"status": "authed"})
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	// 2. 创建 Runtime 并注册 connector
	rt := connector.NewRuntime(nil)

	// 3. 注册一个带 3 个 action 的 connector
	rt.Register(&connector.ConnectorSpec{
		Name:        "demo-http",
		Label:       "演示平台",
		Description: "一个用于验证 connector 系统的最小 HTTP 平台",
		Type:        "http",
		BaseURL:     srv.URL,
		Actions: []connector.ActionSpec{
			{
				Action:      "echo",
				Label:       "问候",
				Description: "根据名字返回问候语",
				Method:      "GET",
				Path:        "/echo",
				Params: []connector.ParamSchema{
					{Name: "name", Label: "姓名", Type: "string", In: "query", Required: true},
				},
			},
			{
				Action:      "login",
				Label:       "登录",
				Description: "获取登录 token",
				Method:      "GET",
				Path:        "/login",
				Category:    "管理",
			},
			{
				Action:      "secure",
				Label:       "需要认证的接口",
				Description: "演示带 token 的调用",
				Method:      "GET",
				Path:        "/secure",
				Category:    "管理",
				RequiresAuth: true,
			},
		},
	})

	// 4. 测试：List 能看到 connector
	list := rt.List()
	assert.True(t, len(list) >= 1, "至少有一个 connector")

	var found *connector.ConnectorInfo
	for i := range list {
		if list[i].Name == "demo-http" {
			found = &list[i]
			break
		}
	}
	assert.NotNil(t, found, "应该能找到 demo-http")
	assert.Equal(t, 3, len(found.Capabilities), "3 个 action")

	// 5. 测试：execute action (echo)
	result, err := rt.Execute(context.Background(),
		"demo-http", "echo", map[string]any{"name": "world"}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success, "echo 应该成功")
	assert.Equal(t, 200, result.StatusCode)

	// 测试 data 字段（greeting 应该是 "hello world"）
	data, ok := result.Data.(map[string]any)
	assert.True(t, ok, "data 应该是 map")
	assert.Equal(t, "hello world", data["greeting"])

	// 6. 测试：不存在的 connector
	_, err = rt.Execute(context.Background(),
		"unknown", "echo", nil, nil)
	assert.Error(t, err)

	// 7. 测试：缺少必填参数
	_, err = rt.Execute(context.Background(),
		"demo-http", "echo", map[string]any{}, nil)
	assert.Error(t, err, "缺少必填参数应该报错")
}

// TestShellExecutor_Basic 测试 shell 类型执行器（echo 命令）。
func TestShellExecutor_Basic(t *testing.T) {
	rt := connector.NewRuntime(nil)

	rt.Register(&connector.ConnectorSpec{
		Name:        "demo-shell",
		Label:       "演示终端",
		Description: "调用本地 shell 命令",
		Type:        "shell",
		DefaultShell: "sh",
		DefaultTimeout: 10,
		Actions: []connector.ActionSpec{
			{
				Action:      "echo",
				Label:       "回显",
				Command:     "echo hello-{name}",
				Params: []connector.ParamSchema{
					{Name: "name", Label: "姓名", Type: "string", Required: true},
				},
			},
			{
				Action:      "ping_once",
				Label:       "ping 一次",
				Command:     "echo pong",
				Expect:      "pong",
			},
		},
	})

	// 执行 echo action
	result, err := rt.Execute(context.Background(),
		"demo-shell", "echo", map[string]any{"name": "platform"}, nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success, "shell echo 应该成功")
	assert.Contains(t, result.Raw, "hello-platform")
}

// TestRegisterJSON 测试从 JSON 字符串注册 connector。
func TestRegisterJSON(t *testing.T) {
	specJSON := `{
		"name": "json-demo",
		"label": "JSON 注册测试",
		"description": "测试从 JSON 字符串加载 connector",
		"type": "http",
		"base_url": "https://example.com",
		"actions": [
			{
				"action": "hello",
				"label": "Hello",
				"method": "GET",
				"path": "/hello"
			}
		]
	}`
	rt := connector.NewRuntime(nil)
	err := rt.Registry().RegisterJSON([]byte(specJSON))
	assert.NoError(t, err)

	info, ok := rt.Get("json-demo")
	assert.True(t, ok)
	assert.Equal(t, "JSON 注册测试", info.Label)
	assert.Equal(t, 1, len(info.Capabilities))
}
