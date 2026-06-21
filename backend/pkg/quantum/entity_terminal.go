// ─── TerminalEntity ──────────────────────────────────────────────────────────
//
// TerminalEntity 代表一个"桌面终端/智能体"——能直接在电脑上执行命令、读写文件。
//
// 设计理念（跟整个 quantum-platform 一致）：
//   "任何产生数据的东西都能交互"
//
// 终端就是这样一个东西：
//   - 你可以拖拽文件进来 → 终端读文件内容
//   - 你可以说"执行这个命令" → 终端运行并返回结果
//   - 你可以说"查找桌面上的 xxx" → 终端搜索本地文件
//
// 两种工作模式：
//   1. Local（本地模式）：通过 os/exec 直接在本机执行
//      适合：桌面端运行 quantum-platform 后端
//   2. Remote（远程模式）：通过 WebSocket 连接到一个远程桌面客户端
//      适合：MiMoCode 插件通过 WebSocket 连到 Bridge
//
// 两种模式通过 TerminalConnector 接口统一抽象。

package quantum

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// TerminalConnector 终端执行器抽象
// ─────────────────────────────────────────────────────────────────────────────

// TerminalConnector 统一了"本地执行"和"远程终端"的接口。
type TerminalConnector interface {
	// Exec 执行一条命令并返回输出
	Exec(ctx context.Context, command string, args []string) (string, error)

	// ReadFile 读取指定路径的文件内容
	ReadFile(ctx context.Context, path string) (string, error)

	// WriteFile 写入内容到指定路径
	WriteFile(ctx context.Context, path string, content string) error

	// ListDir 列出目录内容
	ListDir(ctx context.Context, path string) ([]string, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// LocalConnector 本地执行器（os/exec）
// ─────────────────────────────────────────────────────────────────────────────

// LocalConnector 直接在本地执行命令，使用 os/exec。
type LocalConnector struct{}

func NewLocalConnector() *LocalConnector {
	return &LocalConnector{}
}

func (l *LocalConnector) Exec(ctx context.Context, command string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("exec %s: %w\nstderr: %s", command, err, stderr.String())
	}
	return stdout.String(), nil
}

func (l *LocalConnector) ReadFile(ctx context.Context, path string) (string, error) {
	// 用本地命令读文件（跨平台兼容）
	return l.Exec(ctx, "cat", []string{path})
}

func (l *LocalConnector) WriteFile(ctx context.Context, path string, content string) error {
	// 用 echo 写入文件
	_, err := l.Exec(ctx, "sh", []string{"-c", fmt.Sprintf("cat > %s << 'ENDOFFILE'\n%s\nENDOFFILE", path, content)})
	return err
}

func (l *LocalConnector) ListDir(ctx context.Context, path string) ([]string, error) {
	out, err := l.Exec(ctx, "ls", []string{"-la", path})
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	return lines, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// RemoteConnector 远程执行器（通过 WebSocket 连接桌面客户端）
// ─────────────────────────────────────────────────────────────────────────────

// RemoteConnector 把命令通过 WebSocket 发给远程桌面客户端。
// 客户端（如 MiMoCode 插件）收到后在本机执行并返回结果。
type RemoteConnector struct {
	// wsURL 是远程客户端的 WebSocket 地址
	wsURL string
	// 未来可以通过 transport_ws.go 的 WSTransport 来发送消息
}

func NewRemoteConnector(wsURL string) *RemoteConnector {
	return &RemoteConnector{wsURL: wsURL}
}

// RemoteCommand 发给远程客户端的命令结构
type RemoteCommand struct {
	Action  string   `json:"action"`            // "exec" / "read_file" / "write_file" / "list_dir"
	Command string   `json:"command,omitempty"` // 要执行的命令
	Args    []string `json:"args,omitempty"`     // 命令参数
	Path    string   `json:"path,omitempty"`     // 文件路径
	Content string   `json:"content,omitempty"`  // 写入的内容
}

// RemoteResult 远程客户端返回的结果
type RemoteResult struct {
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

func (r *RemoteConnector) Exec(ctx context.Context, command string, args []string) (string, error) {
	return "", fmt.Errorf("RemoteConnector: WebSocket transport not yet wired (use transport_ws.go WSTransport)")
}

func (r *RemoteConnector) ReadFile(ctx context.Context, path string) (string, error) {
	return "", fmt.Errorf("RemoteConnector: WebSocket transport not yet wired (use transport_ws.go WSTransport)")
}

func (r *RemoteConnector) WriteFile(ctx context.Context, path string, content string) error {
	return fmt.Errorf("RemoteConnector: WebSocket transport not yet wired (use transport_ws.go WSTransport)")
}

func (r *RemoteConnector) ListDir(ctx context.Context, path string) ([]string, error) {
	return nil, fmt.Errorf("RemoteConnector: WebSocket transport not yet wired (use transport_ws.go WSTransport)")
}

// ─────────────────────────────────────────────────────────────────────────────
// TerminalEntity
// ─────────────────────────────────────────────────────────────────────────────

// TerminalEntity 代表一个桌面终端/智能体。
type TerminalEntity struct {
	name        string
	label       string
	connector   TerminalConnector
	workingDir  string // 当前工作目录
	allowedCmds []string // 允许执行的命令白名单（空=全部允许）
}

// TerminalOption 终端 Entity 的可选配置
type TerminalOption struct {
	Label       string
	WorkingDir  string
	AllowedCmds []string // 命令白名单，如 ["ls","cat","pwd","git","pip"]
}

// NewTerminalEntity 创建一个桌面终端 Entity。
//
// name: 唯一标识，如 "desktop-agent"
// connector: 执行器（LocalConnector / RemoteConnector）
// opt: 可选配置
func NewTerminalEntity(name string, connector TerminalConnector, opt *TerminalOption) *TerminalEntity {
	e := &TerminalEntity{
		name:      name,
		label:     name,
		connector: connector,
	}
	if opt != nil {
		if opt.Label != "" {
			e.label = opt.Label
		}
		e.workingDir = opt.WorkingDir
		e.allowedCmds = opt.AllowedCmds
	}
	return e
}

func (t *TerminalEntity) Profile() EntityProfile {
	desc := "桌面终端智能体 – 可以执行命令、读写文件、管理本地数据。"
	if t.workingDir != "" {
		desc += fmt.Sprintf(" 工作目录: %s", t.workingDir)
	}
	keywords := []string{
		"终端", "terminal", "shell", "命令行", "命令",
		"文件", "file", "读取", "写入", "执行", "运行",
		"桌面", "desktop", "下载", "download", "目录",
		"查找", "搜索", "find", "grep", "ls", "cat",
	}
	// 如果有白名单，追加白名单中的命令作为关键词
	for _, cmd := range t.allowedCmds {
		keywords = append(keywords, cmd)
	}
	return EntityProfile{
		Name:        t.name,
		Label:       t.label,
		Kind:        KindTerminal,
		Description: desc,
		Keywords:    keywords,
		Metadata: map[string]string{
			"mode":         t.connectorType(),
			"working_dir":  t.workingDir,
			"allowed_cmds": strings.Join(t.allowedCmds, ","),
		},
		CreatedAt: time.Now(),
	}
}

func (t *TerminalEntity) Match(question string) float64 {
	q := strings.TrimSpace(strings.ToLower(question))
	if q == "" {
		return 0
	}

	// 核心关键词：文件操作
	fileKeywords := []string{"文件", "读取", "写入", "打开", "保存", "file", "read", "write"}
	// 执行关键词
	execKeywords := []string{"执行", "运行", "命令", "终端", "shell", "execute", "run", "cmd"}
	// 目录关键词
	dirKeywords := []string{"目录", "文件夹", "桌面", "下载", "directory", "folder", "desktop", "ls"}
	// 搜索关键词
	searchKeywords := []string{"查找", "搜索", "在哪里", "find", "grep", "search", "locate"}

	score := 0.0
	hitCount := 0

	allKeywords := append(append(append(fileKeywords, execKeywords...), dirKeywords...), searchKeywords...)
	for _, kw := range allKeywords {
		if strings.Contains(q, kw) {
			hitCount++
		}
	}

	if hitCount > 0 {
		// 每命中一个关键词 +0.08，上限 0.6
		score = float64(hitCount) * 0.08
		if score > 0.6 {
			score = 0.6
		}
	}

	// 白名单命令直接匹配 → 强信号
	if len(t.allowedCmds) > 0 {
		for _, cmd := range t.allowedCmds {
			if strings.Contains(q, strings.ToLower(cmd)) {
				score += 0.2
				break
			}
		}
	}

	return clamp01(score)
}

func (t *TerminalEntity) Execute(ctx context.Context, query EntityQuery) EntityResult {
	start := time.Now()
	profile := t.Profile()
	result := EntityResult{
		Profile:   profile,
		LatencyMs: time.Since(start).Milliseconds(),
	}

	if t.connector == nil {
		result.Error = "终端连接器未配置（未设置 LocalConnector 或 RemoteConnector）"
		return result
	}

	// 从问题中解析意图
	action, command, args := t.parseIntent(query.Question)

	var frag EntityFragment

	switch action {
	case "exec":
		// 检查白名单
		if err := t.checkAllowed(command); err != nil {
			return EntityResult{
				Profile:   profile,
				Error:     err.Error(),
				LatencyMs: time.Since(start).Milliseconds(),
			}
		}
		output, err := t.connector.Exec(ctx, command, args)
		if err != nil {
			frag = EntityFragment{
				EntityName: t.name,
				Content:    fmt.Sprintf("命令执行失败: %v", err),
				Confidence: 0.5,
			}
		} else {
			frag = EntityFragment{
				EntityName: t.name,
				SourceRef:  command,
				Content:    t.formatOutput("命令", command, output),
				Confidence: 0.9,
			}
		}

	case "read_file":
		path := t.resolvePath(command) // command 在这里是文件路径
		content, err := t.connector.ReadFile(ctx, path)
		if err != nil {
			frag = EntityFragment{
				EntityName: t.name,
				Content:    fmt.Sprintf("读取文件失败: %v", err),
				Confidence: 0.3,
			}
		} else {
			summary := t.summarizeContent(content)
			frag = EntityFragment{
				EntityName: t.name,
				SourceRef:  path,
				Content:    fmt.Sprintf("文件 %s 的内容:\n%s", path, summary),
				Confidence: 0.9,
			}
		}

	case "write_file":
		path := t.resolvePath(command)
		content := strings.Join(args, " ")
		if err := t.connector.WriteFile(ctx, path, content); err != nil {
			frag = EntityFragment{
				EntityName: t.name,
				Content:    fmt.Sprintf("写入文件失败: %v", err),
				Confidence: 0.3,
			}
		} else {
			frag = EntityFragment{
				EntityName: t.name,
				SourceRef:  path,
				Content:    fmt.Sprintf("已写入文件 %s（%d 字节）", path, len(content)),
				Confidence: 0.9,
			}
		}

	case "list_dir":
		path := t.resolvePath(command)
		entries, err := t.connector.ListDir(ctx, path)
		if err != nil {
			frag = EntityFragment{
				EntityName: t.name,
				Content:    fmt.Sprintf("列出目录失败: %v", err),
				Confidence: 0.3,
			}
		} else {
			frag = EntityFragment{
				EntityName: t.name,
				SourceRef:  path,
				Content:    fmt.Sprintf("目录 %s 的内容:\n%s", path, strings.Join(entries, "\n")),
				Confidence: 0.9,
			}
		}

	default:
		// 兜底：把整个问题当命令执行
		output, err := t.connector.Exec(ctx, "sh", []string{"-c", query.Question})
		if err != nil {
			frag = EntityFragment{
				EntityName: t.name,
				Content:    fmt.Sprintf("无法理解请求，请明确指定要执行的操作。\n（例如：'执行 ls -la' 或 '读取 README.md'）\n\n错误: %v", err),
				Confidence: 0.2,
			}
		} else {
			frag = EntityFragment{
				EntityName: t.name,
				Content:    t.formatOutput("命令", query.Question, output),
				Confidence: 0.6,
			}
		}
	}

	return EntityResult{
		Profile:   profile,
		Fragments: []EntityFragment{frag},
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 内部工具
// ─────────────────────────────────────────────────────────────────────────────

func (t *TerminalEntity) connectorType() string {
	switch t.connector.(type) {
	case *LocalConnector:
		return "local"
	case *RemoteConnector:
		return "remote"
	default:
		return "unknown"
	}
}

// parseIntent 从自然语言问题中解析意图。
// 返回 (action, command/param, args)。
func (t *TerminalEntity) parseIntent(question string) (string, string, []string) {
	q := strings.TrimSpace(question)

	// 尝试 JSON 格式的远程命令
	var rc RemoteCommand
	if json.Unmarshal([]byte(q), &rc) == nil && rc.Action != "" {
		return rc.Action, rc.Command, rc.Args
	}

	qLower := strings.ToLower(q)

	// 读取文件: "读取 xxx" / "打开 xxx" / "读 xxx" / "read xxx" / "cat xxx"
	for _, prefix := range []string{"读取", "打开", "读一下", "读", "read ", "cat "} {
		if strings.HasPrefix(qLower, prefix) {
			path := strings.TrimSpace(q[len(prefix):])
			if path == "" {
				// 尝试从引号内提取
				if idx := strings.Index(q, "\""); idx >= 0 {
					if end := strings.LastIndex(q[idx+1:], "\""); end >= 0 {
						path = strings.TrimSpace(q[idx+1 : idx+1+end])
					}
				}
			}
			if path != "" {
				return "read_file", path, nil
			}
			return "read_file", q[len(prefix):], nil
		}
	}

	// 执行命令: "执行 xxx" / "运行 xxx" / "run xxx" / "exec xxx"
	for _, prefix := range []string{"执行", "运行", "run ", "exec "} {
		if strings.HasPrefix(qLower, prefix) {
			rest := strings.TrimSpace(q[len(prefix):])
			parts := splitCmd(rest)
			if len(parts) > 0 {
				return "exec", parts[0], parts[1:]
			}
		}
	}

	// 写文件: "写入 xxx 内容为 yyy" / "保存 xxx"
	if strings.Contains(qLower, "写入") || strings.Contains(qLower, "保存") {
		parts := strings.SplitN(q, "内容为", 2)
		if len(parts) == 2 {
			path := strings.TrimSpace(strings.TrimPrefix(parts[0], "写入"))
			path = strings.TrimSpace(strings.TrimPrefix(path, "保存"))
			content := strings.TrimSpace(parts[1])
			return "write_file", path, []string{content}
		}
	}

	// 列目录: "查看目录 xxx" / "ls xxx" / "列出 xxx"
	for _, prefix := range []string{"查看目录", "列出", "ls "} {
		if strings.HasPrefix(qLower, prefix) {
			rest := strings.TrimSpace(q[len(prefix):])
			if rest == "" {
				rest = "."
			}
			return "list_dir", rest, nil
		}
	}

	// 无法识别 → 直接执行整句
	return "", q, nil
}

// checkAllowed 检查命令是否在白名单内
func (t *TerminalEntity) checkAllowed(command string) error {
	if len(t.allowedCmds) == 0 {
		return nil // 空白名单 = 允许全部
	}
	for _, allowed := range t.allowedCmds {
		if command == allowed {
			return nil
		}
	}
	return fmt.Errorf("命令 %q 不在白名单中（允许: %s）", command, strings.Join(t.allowedCmds, ", "))
}

// resolvePath 解析路径（相对于工作目录）
func (t *TerminalEntity) resolvePath(path string) string {
	if path == "" {
		path = "."
	}
	if t.workingDir != "" && !filepath.IsAbs(path) {
		return filepath.Join(t.workingDir, path)
	}
	return path
}

// formatOutput 把执行结果格式化成人类可读的文本
func (t *TerminalEntity) formatOutput(action string, target string, output string) string {
	if len(output) > 2000 {
		output = output[:2000] + "\n...（输出过长，已截断）"
	}
	return fmt.Sprintf("[%s] %s\n%s", action, target, output)
}

// summarizeContent 摘要化文件内容（太长时截断）
func (t *TerminalEntity) summarizeContent(content string) string {
	if len(content) > 3000 {
		return content[:3000] + "\n...（文件过长，已截断）"
	}
	return content
}

// splitCmd 把命令字符串拆成命令+参数（简单实现）
func splitCmd(s string) []string {
	if s == "" {
		return nil
	}
	// 简单按空格分割（TODO: 需要处理引号内的空格）
	return strings.Fields(s)
}

// compile-time interface check
var _ Entity = (*TerminalEntity)(nil)
