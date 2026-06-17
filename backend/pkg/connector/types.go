package connector

// ParamSchema 描述一个 action 的参数结构，前端可以据此自动渲染表单，
// Runtime 可以据此做类型校验。
type ParamSchema struct {
	Name     string            `json:"name"`               // 参数名（在请求中的 key）
	Label    string            `json:"label"`              // 展示名（给用户看）
	Type     string            `json:"type"`               // "string" | "number" | "bool" | "select" | "textarea" | "file"
	In       string            `json:"in,omitempty"`       // "query" | "body" | "path" | "header"
	Required bool              `json:"required"`           // 是否必填
	Default  any               `json:"default,omitempty"`  // 默认值
	Options  []ParamOption     `json:"options,omitempty"`  // Type=select 时的候选项
	Help     string            `json:"help,omitempty"`     // 帮助文字
}

type ParamOption struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}

// Capability 描述 connector 对外能做什么，供 UI/AI 发现能力。
type Capability struct {
	Action      string            `json:"action"`             // 内部标识，执行时用这个
	Label       string            `json:"label"`              // 展示名
	Description string            `json:"description"`        // 详细描述
	Params      []ParamSchema     `json:"params"`             // 参数列表
	Returns     string            `json:"returns,omitempty"`  // 返回值描述（文本）
	Category    string            `json:"category,omitempty"` // 分类（如 "查询" / "操作" / "管理"）
	DocURL      string            `json:"doc_url,omitempty"`  // 参考文档
}

// AuthScheme 描述认证方案。
type AuthScheme struct {
	Type       string            `json:"type"`                 // "none" | "bearer" | "apikey" | "basic" | "oauth2" | "signature"
	KeyField   string            `json:"key_field,omitempty"`  // key 在 header/query 里叫什么
	TokenURL   string            `json:"token_url,omitempty"`  // oauth2 取 token 的地址
	HeaderName string            `json:"header_name,omitempty"` // 自定义 header 名（apikey 模式）
	LoginAction string           `json:"login_action,omitempty"` // 走哪个 action 登录（自定义登录流程）
	TokenPath  string            `json:"token_path,omitempty"`  // 登录返回的 token 字段路径（如 "data.access_token"）
	TokenIn    string            `json:"token_in,omitempty"`   // token 放在哪："header:Authorization:Bearer" | "query:access_token"
	ExpiresIn  int               `json:"expires_in,omitempty"` // 过期秒数
}

// ActionSpec 描述一个可执行动作。
//
// 对于 HTTP 形态：
//   Method + Path 决定发请求的位置；
//   Params 决定怎么把用户输入映射到 query/body/path/header；
//   ResponseFilter 是可选的 JSON path，用来从响应里取需要的字段。
//
// 对于 Shell 形态：
//   Command 是命令模板（支持 {param} 占位符）；
//   Timeout 秒数；
//   Expect 是可选的期望输出匹配正则（匹配成功才算结束）。
//
// 对于 Script 形态：
//   Code 是脚本代码（当前支持 JS/Go 模板，后续可扩展）；
//   Runtime 是执行器名。
type ActionSpec struct {
	Action         string            `json:"action"`
	Label          string            `json:"label,omitempty"`
	Description    string            `json:"description,omitempty"`

	// 通用
	Params         []ParamSchema     `json:"params,omitempty"`
	ResponseFilter string            `json:"response_filter,omitempty"` // 如 "data.items"
	CacheTTL       int               `json:"cache_ttl,omitempty"`      // 缓存秒数（0 = 不缓存）

	// HTTP 专属
	Method         string            `json:"method,omitempty"`
	Path           string            `json:"path,omitempty"`
	BaseURL        string            `json:"base_url,omitempty"`       // 可选，覆盖 connector 的 baseURL
	Headers        map[string]string `json:"headers,omitempty"`
	BodyTemplate   string            `json:"body_template,omitempty"` // 支持 {param} 占位
	QueryParams    map[string]string `json:"query_params,omitempty"`  // 可选固定 query
	MediaType      string            `json:"media_type,omitempty"`    // 默认 application/json

	// Shell 专属
	Command        string            `json:"command,omitempty"`       // 支持 {param} 占位符
	Timeout        int               `json:"timeout,omitempty"`       // 秒，默认 30
	WorkDir        string            `json:"work_dir,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Expect         string            `json:"expect,omitempty"`        // 等待输出匹配正则（用于交互式 CLI）
	Shell          string            `json:"shell,omitempty"`         // 默认 "sh" / "bash"

	// 元信息
	Category       string            `json:"category,omitempty"`
	DocURL         string            `json:"doc_url,omitempty"`
	RequiresAuth   bool              `json:"requires_auth,omitempty"`
}

// ConnectorSpec 描述一个完整的"能力终端"。
//
// 它可以从 JSON 文件加载，也可以通过 API 动态创建。核心语义：
//   - Name 是唯一标识（建议小写，用点/短横线分隔）
//   - Type 决定执行后端："http" / "shell" / "script"
//   - Auth 描述认证
//   - Actions 是能力清单（每个 Action = 一个可调用的能力）
//   - Meta 给用户看的描述信息
type ConnectorSpec struct {
	Name        string            `json:"name"`                   // 唯一标识，例 "xiaomi-cloud"
	Label       string            `json:"label"`                  // 展示名
	Description string            `json:"description"`            // 简介
	Type        string            `json:"type"`                   // "http" | "shell" | "script"
	Version     string            `json:"version,omitempty"`

	// HTTP 形态专属
	BaseURL     string            `json:"base_url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`

	// Shell 形态专属
	DefaultShell string           `json:"default_shell,omitempty"`
	DefaultTimeout int            `json:"default_timeout,omitempty"`

	// Script 形态专属
	Runtime     string            `json:"runtime,omitempty"`      // "tengo" / "otto" 等

	// 认证
	Auth        AuthScheme        `json:"auth,omitempty"`

	// 能力清单
	Actions     []ActionSpec      `json:"actions"`

	// 元信息
	Tags        []string          `json:"tags,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	Vendor      string            `json:"vendor,omitempty"`
	Homepage    string            `json:"homepage,omitempty"`

	// 安全
	Security    SecurityPolicy    `json:"security,omitempty"`
}

// SecurityPolicy 安全策略（可选，将来扩展用）。
type SecurityPolicy struct {
	AllowPrivateCIDR   bool     `json:"allow_private_cidr"`    // 是否允许访问内网 IP
	AllowFileUpload    bool     `json:"allow_file_upload"`
	AllowFileDownload  bool     `json:"allow_file_download"`
	AllowedHosts       []string `json:"allowed_hosts"`         // 白名单域名（空=不限制）
	MaxResponseSizeMB  int      `json:"max_response_size_mb"` // 响应体上限
	RateLimitPerMin    int      `json:"rate_limit_per_min"`   // 每分钟请求上限
}

// Credential 运行时凭证。不存数据库，由调用方提供。
type Credential struct {
	APIKey      string            `json:"api_key,omitempty"`
	Token       string            `json:"token,omitempty"`
	Username    string            `json:"username,omitempty"`
	Password    string            `json:"password,omitempty"`
	Secret      string            `json:"secret,omitempty"`
	Custom      map[string]string `json:"custom,omitempty"`
}

// ExecuteResult 统一执行结果。
type ExecuteResult struct {
	Connector   string         `json:"connector"`
	Action      string         `json:"action"`
	Success     bool           `json:"success"`
	Data        any            `json:"data,omitempty"`
	Raw         string         `json:"raw,omitempty"`          // 原始响应（调试用）
	Error       string         `json:"error,omitempty"`
	StatusCode  int            `json:"status_code,omitempty"`  // HTTP 状态码（如果是 HTTP）
	LatencyMs   int64          `json:"latency_ms"`
	CacheHit    bool           `json:"cache_hit,omitempty"`
}

// ConnectorInfo 给前端/调用方的"发现"接口返回信息。
type ConnectorInfo struct {
	Name         string         `json:"name"`
	Label        string         `json:"label"`
	Description  string         `json:"description"`
	Type         string         `json:"type"`
	Version      string         `json:"version,omitempty"`
	Vendor       string         `json:"vendor,omitempty"`
	Icon         string         `json:"icon,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Capabilities []Capability   `json:"capabilities"`
	AuthType     string         `json:"auth_type,omitempty"`
	RequiresAuth bool           `json:"requires_auth,omitempty"`
}
