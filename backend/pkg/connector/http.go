package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HTTPExecutor 执行 HTTP 类型的 Action。
// 它解析 spec.ActionSpec 里的 Method/Path/Params/Headers 等字段，
// 拼成实际的 HTTP 请求并发送，返回统一的 ExecuteResult。
type HTTPExecutor struct {
	client *http.Client
	logger *slog.Logger
}

func NewHTTPExecutor(logger *slog.Logger) *HTTPExecutor {
	return &HTTPExecutor{
		client: &http.Client{Timeout: 60 * time.Second},
		logger: logger,
	}
}

// Execute 执行一个 HTTP action。
// spec 是整个 connector 的描述（baseURL 等全局配置在这里）；
// action 是具体的动作描述；
// params 是用户传入的参数（按 name 索引）；
// cred 是凭证（可能为空，取决于 auth.Type）。
func (h *HTTPExecutor) Execute(
	ctx context.Context,
	spec *ConnectorSpec,
	action *ActionSpec,
	params map[string]any,
	cred *Credential,
) (*ExecuteResult, error) {
	start := time.Now()

	// 1) 解析 base URL
	baseURL := spec.BaseURL
	if action.BaseURL != "" {
		baseURL = action.BaseURL
	}
	if baseURL == "" {
		return makeErrResult(spec.Name, action.Action, "base_url is required"), fmt.Errorf("base_url empty")
	}

	// 2) 替换 path 中的 {param} 占位符
	path := action.Path
	if path == "" {
		path = "/"
	}
	path = renderTemplate(path, params)

	// 3) 构造完整 URL
	u, err := url.Parse(baseURL)
	if err != nil {
		return makeErrResult(spec.Name, action.Action, fmt.Sprintf("invalid base_url: %v", err)), err
	}
	u.Path = singleJoiningSlash(u.Path, path)

	// 4) 拼接 query
	q := u.Query()
	for k, v := range action.QueryParams {
		q.Set(k, renderTemplate(v, params))
	}
	for _, p := range action.Params {
		if p.In == "query" {
			if val, ok := params[p.Name]; ok {
				q.Set(p.Name, fmt.Sprintf("%v", val))
			}
		}
	}
	u.RawQuery = q.Encode()

	// 5) 构造 body
	var body io.Reader
	mediaType := action.MediaType
	if mediaType == "" {
		mediaType = "application/json"
	}

	// 根据 BodyTemplate 或 params 构造 body
	if action.BodyTemplate != "" {
		bodyStr := renderTemplate(action.BodyTemplate, params)
		body = strings.NewReader(bodyStr)
	} else if len(action.Params) > 0 {
		bodyMap := make(map[string]any)
		for _, p := range action.Params {
			if p.In == "body" || p.In == "" {
				if val, ok := params[p.Name]; ok {
					bodyMap[p.Name] = val
				} else if p.Default != nil {
					bodyMap[p.Name] = p.Default
				}
			}
		}
		if len(bodyMap) > 0 && action.Method != "GET" && action.Method != "DELETE" {
			bs, err := json.Marshal(bodyMap)
			if err != nil {
				return makeErrResult(spec.Name, action.Action, fmt.Sprintf("marshal body: %v", err)), err
			}
			body = bytes.NewBuffer(bs)
		}
	}

	method := strings.ToUpper(action.Method)
	if method == "" {
		method = "GET"
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return makeErrResult(spec.Name, action.Action, fmt.Sprintf("build request: %v", err)), err
	}

	// 6) 设置 header
	req.Header.Set("Content-Type", mediaType)
	req.Header.Set("Accept", "application/json")

	// 合并 connector 级别的 header
	for k, v := range spec.Headers {
		req.Header.Set(k, renderTemplate(v, params))
	}
	// 合并 action 级别的 header（优先级更高）
	for k, v := range action.Headers {
		req.Header.Set(k, renderTemplate(v, params))
	}
	// 处理 path 参数
	for _, p := range action.Params {
		if p.In == "path" {
			if val, ok := params[p.Name]; ok {
				req.URL.Path = strings.ReplaceAll(req.URL.Path, "{"+p.Name+"}", fmt.Sprintf("%v", val))
			}
		} else if p.In == "header" {
			if val, ok := params[p.Name]; ok {
				req.Header.Set(p.Name, fmt.Sprintf("%v", val))
			}
		}
	}

	// 7) 处理认证
	if cred != nil && spec.Auth.Type != "none" && spec.Auth.Type != "" {
		switch spec.Auth.Type {
		case "bearer":
			if cred.Token != "" {
				req.Header.Set("Authorization", "Bearer "+cred.Token)
			}
		case "apikey":
			keyField := spec.Auth.KeyField
			if keyField == "" {
				keyField = "X-API-Key"
			}
			headerName := spec.Auth.HeaderName
			if headerName == "" {
				headerName = keyField
			}
			if cred.APIKey != "" {
				req.Header.Set(headerName, cred.APIKey)
			}
		case "basic":
			if cred.Username != "" {
				req.SetBasicAuth(cred.Username, cred.Password)
			}
		}
	}

	// 8) 发送请求
	if h.logger != nil {
		h.logger.DebugContext(ctx, "connector.http.execute",
			"connector", spec.Name,
			"action", action.Action,
			"url", u.String(),
			"method", method,
		)
	}

	resp, err := h.client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &ExecuteResult{
			Connector:  spec.Name,
			Action:     action.Action,
			Success:    false,
			Error:      err.Error(),
			LatencyMs:  latency,
		}, nil
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ExecuteResult{
			Connector:  spec.Name,
			Action:     action.Action,
			Success:    false,
			Error:      "read body: " + err.Error(),
			LatencyMs:  latency,
		}, nil
	}

	result := &ExecuteResult{
		Connector:  spec.Name,
		Action:     action.Action,
		StatusCode: resp.StatusCode,
		Raw:        string(raw),
		LatencyMs:  latency,
	}

	// 9) 解析响应
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result.Success = true
		// 按 response_filter 提取数据
		data, filterErr := filterJSON(raw, action.ResponseFilter)
		if filterErr == nil {
			result.Data = data
		} else {
			// 回退：原样返回
			var rawData any
			if err := json.Unmarshal(raw, &rawData); err == nil {
				result.Data = rawData
			} else {
				result.Data = string(raw)
			}
		}
	} else {
		result.Success = false
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		var errData any
		if err := json.Unmarshal(raw, &errData); err == nil {
			result.Data = errData
		}
	}

	return result, nil
}

// ---------- 辅助函数 ----------

func makeErrResult(connector, action, errMsg string) *ExecuteResult {
	return &ExecuteResult{
		Connector: connector,
		Action:    action,
		Success:   false,
		Error:     errMsg,
	}
}

// renderTemplate 把字符串中的 {key} 占位符按 params 替换。
func renderTemplate(s string, params map[string]any) string {
	if !strings.Contains(s, "{") {
		return s
	}
	for k, v := range params {
		placeholder := "{" + k + "}"
		if strings.Contains(s, placeholder) {
			s = strings.ReplaceAll(s, placeholder, fmt.Sprintf("%v", v))
		}
	}
	return s
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		if a == "" {
			return b
		}
		return a + "/" + b
	}
	return a + b
}

// filterJSON 按 "a.b.c" 这种路径从 json 里提取指定字段。
// path 为空返回整个对象。
func filterJSON(raw []byte, path string) (any, error) {
	if path == "" {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	}

	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}

	parts := strings.Split(path, ".")
	cur := root
	for _, part := range parts {
		if part == "" {
			continue
		}
		switch node := cur.(type) {
		case map[string]any:
			if v, ok := node[part]; ok {
				cur = v
			} else {
				return nil, fmt.Errorf("path not found: %s", path)
			}
		default:
			return nil, fmt.Errorf("cannot traverse non-object at %s", part)
		}
	}
	return cur, nil
}
