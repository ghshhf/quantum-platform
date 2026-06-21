// ─── TerminalEntity 单元测试 ─────────────────────────────────────────────────
//
// 使用 MockConnector 模拟终端操作，不依赖真实 OS。
// 测试覆盖：意图解析、Profile/Match/Execute 各路径

package quantum

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// MockConnector — 模拟终端执行器
// ─────────────────────────────────────────────────────────────────────────────

type MockConnector struct {
	execFunc    func(ctx context.Context, cmd string, args []string) (string, error)
	readFunc    func(ctx context.Context, path string) (string, error)
	writeFunc   func(ctx context.Context, path, content string) error
	listFunc    func(ctx context.Context, path string) ([]string, error)
	execCount   int
}

func (m *MockConnector) Exec(ctx context.Context, cmd string, args []string) (string, error) {
	m.execCount++
	if m.execFunc != nil {
		return m.execFunc(ctx, cmd, args)
	}
	return fmt.Sprintf("exec: %s %v", cmd, args), nil
}

func (m *MockConnector) ReadFile(ctx context.Context, path string) (string, error) {
	if m.readFunc != nil {
		return m.readFunc(ctx, path)
	}
	return fmt.Sprintf("content of %s", path), nil
}

func (m *MockConnector) WriteFile(ctx context.Context, path, content string) error {
	if m.writeFunc != nil {
		return m.writeFunc(ctx, path, content)
	}
	return nil
}

func (m *MockConnector) ListDir(ctx context.Context, path string) ([]string, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, path)
	}
	return []string{"file1.txt", "file2.go", "dir1/"}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Profile
// ─────────────────────────────────────────────────────────────────────────────

func TestTerminalProfile(t *testing.T) {
	e := NewTerminalEntity("test-terminal", NewLocalConnector(), &TerminalOption{
		Label:      "测试终端",
		WorkingDir: "/home/test",
	})
	p := e.Profile()

	if p.Name != "test-terminal" {
		t.Errorf("Name: got %q, want %q", p.Name, "test-terminal")
	}
	if p.Label != "测试终端" {
		t.Errorf("Label: got %q, want %q", p.Label, "测试终端")
	}
	if p.Kind != KindTerminal {
		t.Errorf("Kind: got %q, want %q", p.Kind, KindTerminal)
	}
	if !strings.Contains(p.Description, "桌面终端智能体") {
		t.Errorf("Description should contain 桌面终端智能体, got: %s", p.Description)
	}
	if p.Metadata["working_dir"] != "/home/test" {
		t.Errorf("Metadata working_dir: got %q", p.Metadata["working_dir"])
	}
}

func TestTerminalProfileNoOptions(t *testing.T) {
	e := NewTerminalEntity("simple-term", NewLocalConnector(), nil)
	p := e.Profile()

	if p.Name != "simple-term" {
		t.Errorf("Name: got %q", p.Name)
	}
	if p.Label != "simple-term" {
		t.Errorf("Label should default to name, got %q", p.Label)
	}
}

func TestTerminalProfileWithAllowedCmds(t *testing.T) {
	e := NewTerminalEntity("safe-term", NewLocalConnector(), &TerminalOption{
		AllowedCmds: []string{"ls", "cat", "pwd"},
	})
	p := e.Profile()

	found := false
	for _, kw := range p.Keywords {
		if kw == "ls" {
			found = true
			break
		}
	}
	if !found {
		t.Error("allowed cmd 'ls' should appear in keywords")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Match
// ─────────────────────────────────────────────────────────────────────────────

func TestTerminalMatch_FileKeywords(t *testing.T) {
	e := NewTerminalEntity("t", NewLocalConnector(), nil)

	// 包含"文件" → 应该命中
	score := e.Match("帮我读取一个文件")
	if score <= 0 {
		t.Errorf("expected positive score for '文件', got %f", score)
	}
}

func TestTerminalMatch_ExecKeywords(t *testing.T) {
	e := NewTerminalEntity("t", NewLocalConnector(), nil)

	score := e.Match("执行 ls -la")
	if score <= 0 {
		t.Errorf("expected positive score for '执行', got %f", score)
	}
}

func TestTerminalMatch_DesktopKeywords(t *testing.T) {
	e := NewTerminalEntity("t", NewLocalConnector(), nil)

	score := e.Match("桌面上的文件在哪里")
	if score <= 0 {
		t.Errorf("expected positive score for '桌面', got %f", score)
	}
}

func TestTerminalMatch_SearchKeywords(t *testing.T) {
	e := NewTerminalEntity("t", NewLocalConnector(), nil)

	score := e.Match("查找下载目录的 pdf 文件")
	if score <= 0 {
		t.Errorf("expected positive score for search keywords, got %f", score)
	}
}

func TestTerminalMatch_EmptyString(t *testing.T) {
	e := NewTerminalEntity("t", NewLocalConnector(), nil)

	if score := e.Match(""); score != 0 {
		t.Errorf("expected 0 for empty string, got %f", score)
	}
}

func TestTerminalMatch_Irrelevant(t *testing.T) {
	e := NewTerminalEntity("t", NewLocalConnector(), nil)

	// 完全不相关的问题
	score := e.Match("今天天气怎么样")
	if score >= 0.3 {
		t.Errorf("expected low score for irrelevant question, got %f", score)
	}
}

func TestTerminalMatch_SpecificCmd(t *testing.T) {
	e := NewTerminalEntity("t", NewLocalConnector(), &TerminalOption{
		AllowedCmds: []string{"docker", "git"},
	})

	// 包含白名单命令 → 强加分
	score := e.Match("执行 docker ps")
	dockerScore := e.Match("查看 docker 容器")
	if dockerScore <= 0 || dockerScore > 1 {
		t.Errorf("expected high score for 'docker', got %f", dockerScore)
	}
	_ = score
}

// ─────────────────────────────────────────────────────────────────────────────
// Execute / parseIntent
// ─────────────────────────────────────────────────────────────────────────────

func TestTerminalExecute_ReadFile(t *testing.T) {
	mock := &MockConnector{
		readFunc: func(ctx context.Context, path string) (string, error) {
			return "hello world", nil
		},
	}
	e := NewTerminalEntity("t", mock, nil)

	result := e.Execute(context.Background(), EntityQuery{
		Question: "读取 test.txt",
	})
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if len(result.Fragments) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(result.Fragments))
	}
	if !strings.Contains(result.Fragments[0].Content, "test.txt") {
		t.Errorf("Content should contain filename, got: %s", result.Fragments[0].Content)
	}
}

func TestTerminalExecute_WriteFile(t *testing.T) {
	var writtenPath, writtenContent string
	mock := &MockConnector{
		writeFunc: func(ctx context.Context, path, content string) error {
			writtenPath = path
			writtenContent = content
			return nil
		},
	}
	e := NewTerminalEntity("t", mock, nil)

	result := e.Execute(context.Background(), EntityQuery{
		Question: "写入 test.txt 内容为 hello world",
	})
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if writtenPath != "test.txt" {
		t.Errorf("WriteFile path: got %q", writtenPath)
	}
	if writtenContent != "hello world" {
		t.Errorf("WriteFile content: got %q", writtenContent)
	}
}

func TestTerminalExecute_ListDir(t *testing.T) {
	mock := &MockConnector{
		listFunc: func(ctx context.Context, path string) ([]string, error) {
			return []string{"a.txt", "b.go"}, nil
		},
	}
	e := NewTerminalEntity("t", mock, nil)

	result := e.Execute(context.Background(), EntityQuery{
		Question: "列出 /home/test",
	})
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	frag := result.Fragments[0]
	if !strings.Contains(frag.Content, "a.txt") {
		t.Errorf("Content should list files, got: %s", frag.Content)
	}
}

func TestTerminalExecute_ExecCommand(t *testing.T) {
	var execCmd string
	mock := &MockConnector{
		execFunc: func(ctx context.Context, cmd string, args []string) (string, error) {
			execCmd = cmd
			return "output ok", nil
		},
	}
	e := NewTerminalEntity("t", mock, nil)

	result := e.Execute(context.Background(), EntityQuery{
		Question: "执行 ls -la",
	})
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if execCmd != "ls" {
		t.Errorf("exec cmd: got %q, want 'ls'", execCmd)
	}
}

func TestTerminalExecute_AllowedCmd(t *testing.T) {
	mock := &MockConnector{}
	e := NewTerminalEntity("t", mock, &TerminalOption{
		AllowedCmds: []string{"ls", "cat"},
	})

	// 允许的命令 → 成功
	result := e.Execute(context.Background(), EntityQuery{
		Question: "执行 ls -la",
	})
	if result.Error != "" {
		t.Errorf("allowed cmd 'ls' should succeed, got error: %s", result.Error)
	}

	// 不允许的命令 → 报错
	result = e.Execute(context.Background(), EntityQuery{
		Question: "执行 rm -rf /",
	})
	if result.Error == "" && (len(result.Fragments) == 0 || !strings.Contains(result.Fragments[0].Content, "失败")) {
		t.Error("expected error for disallowed cmd 'rm', got nil")
	}
}

func TestTerminalExecute_ReadFileError(t *testing.T) {
	mock := &MockConnector{
		readFunc: func(ctx context.Context, path string) (string, error) {
			return "", fmt.Errorf("file not found")
		},
	}
	e := NewTerminalEntity("t", mock, nil)

	result := e.Execute(context.Background(), EntityQuery{
		Question: "读取 missing.txt",
	})
	if result.Error == "" && (len(result.Fragments) == 0 || !strings.Contains(result.Fragments[0].Content, "失败")) {
		t.Fatal("expected error for missing file")
	}
}

func TestTerminalExecute_NoConnector(t *testing.T) {
	e := &TerminalEntity{
		name:  "broken",
		label: "broken",
	}
	result := e.Execute(context.Background(), EntityQuery{Question: "ls"})
	if result.Error == "" {
		t.Error("expected error when connector is nil")
	}
}

func TestTerminalExecute_LatencyMs(t *testing.T) {
	mock := &MockConnector{
		execFunc: func(ctx context.Context, cmd string, args []string) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return "ok", nil
		},
	}
	e := NewTerminalEntity("t", mock, nil)
	result := e.Execute(context.Background(), EntityQuery{Question: "执行 echo ok"})

	if result.LatencyMs <= 0 {
		t.Errorf("expected positive LatencyMs, got %d", result.LatencyMs)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseIntent 自然语言意图解析
// ─────────────────────────────────────────────────────────────────────────────

func TestParseIntent_ReadFile(t *testing.T) {
	e := NewTerminalEntity("t", nil, nil)

	tests := []struct {
		input    string
		wantAct  string
		wantPath string
	}{
		{"读取 README.md", "read_file", "README.md"},
		{"打开 config.json", "read_file", "config.json"},
		{"读一下 main.go", "read_file", "main.go"},
		{"cat /etc/hosts", "read_file", "/etc/hosts"},
		{"read test.txt", "read_file", "test.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			act, param, _ := e.parseIntent(tt.input)
			if act != tt.wantAct {
				t.Errorf("action: got %q, want %q", act, tt.wantAct)
			}
			if param != tt.wantPath {
				t.Errorf("param: got %q, want %q", param, tt.wantPath)
			}
		})
	}
}

func TestParseIntent_ExecCommand(t *testing.T) {
	e := NewTerminalEntity("t", nil, nil)

	tests := []struct {
		input    string
		wantCmd  string
		wantArgs []string
	}{
		{"执行 ls -la", "ls", []string{"-la"}},
		{"运行 docker ps", "docker", []string{"ps"}},
		{"run go build", "go", []string{"build"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			act, cmd, args := e.parseIntent(tt.input)
			if act != "exec" {
				t.Errorf("action: got %q, want 'exec'", act)
			}
			if cmd != tt.wantCmd {
				t.Errorf("cmd: got %q, want %q", cmd, tt.wantCmd)
			}
			if strings.Join(args, " ") != strings.Join(tt.wantArgs, " ") {
				t.Errorf("args: got %v, want %v", args, tt.wantArgs)
			}
		})
	}
}

func TestParseIntent_WriteFile(t *testing.T) {
	e := NewTerminalEntity("t", nil, nil)

	act, path, args := e.parseIntent("写入 hello.txt 内容为 你好世界")
	if act != "write_file" {
		t.Errorf("action: got %q, want 'write_file'", act)
	}
	if path != "hello.txt" {
		t.Errorf("path: got %q", path)
	}
	if len(args) != 1 || args[0] != "你好世界" {
		t.Errorf("content: got %v", args)
	}
}

func TestParseIntent_ListDir(t *testing.T) {
	e := NewTerminalEntity("t", nil, nil)

	tests := []struct {
		input string
		want  string
	}{
		{"列出 /home", "/home"},
		{"ls /tmp", "/tmp"},
		{"查看目录 Downloads", "Downloads"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			act, param, _ := e.parseIntent(tt.input)
			if act != "list_dir" {
				t.Errorf("action: got %q, want 'list_dir'", act)
			}
			if param != tt.want {
				t.Errorf("param: got %q, want %q", param, tt.want)
			}
		})
	}
}

func TestParseIntent_JSONCommand(t *testing.T) {
	e := NewTerminalEntity("t", nil, nil)

	json := `{"action":"exec","command":"ls","args":["-la"]}`
	act, cmd, args := e.parseIntent(json)
	if act != "exec" {
		t.Errorf("action: got %q", act)
	}
	if cmd != "ls" {
		t.Errorf("cmd: got %q", cmd)
	}
	if len(args) != 1 || args[0] != "-la" {
		t.Errorf("args: got %v", args)
	}
}

func TestParseIntent_Unrecognized(t *testing.T) {
	e := NewTerminalEntity("t", nil, nil)

	act, param, _ := e.parseIntent("你好，能帮我查个东西吗")
	if act != "" {
		t.Errorf("expected empty action for unrecognized, got %q", act)
	}
	if param != "你好，能帮我查个东西吗" {
		t.Errorf("expected full question as fallback")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// NewTerminalEntity / TerminalOption
// ─────────────────────────────────────────────────────────────────────────────

func TestNewTerminalEntityDefault(t *testing.T) {
	e := NewTerminalEntity("default", NewLocalConnector(), nil)
	if e == nil {
		t.Fatal("NewTerminalEntity returned nil")
	}
	if e.name != "default" {
		t.Errorf("name: got %q", e.name)
	}
	if e.label != "default" {
		t.Errorf("label should default to name, got %q", e.label)
	}
}

func TestNewTerminalEntityWithOptions(t *testing.T) {
	e := NewTerminalEntity("opt", NewLocalConnector(), &TerminalOption{
		Label:       "MyTerm",
		WorkingDir:  "/tmp",
		AllowedCmds: []string{"echo"},
	})
	if e.label != "MyTerm" {
		t.Errorf("label: got %q", e.label)
	}
	if e.workingDir != "/tmp" {
		t.Errorf("workingDir: got %q", e.workingDir)
	}
	if len(e.allowedCmds) != 1 || e.allowedCmds[0] != "echo" {
		t.Errorf("allowedCmds: got %v", e.allowedCmds)
	}
}
