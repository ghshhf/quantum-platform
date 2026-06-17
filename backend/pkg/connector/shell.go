package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ShellExecutor 执行 shell 类型的 Action。
// 对"只有 CLI 终端"的场景——把命令执行标准化成 API 风格的调用。
type ShellExecutor struct {
	logger *slog.Logger
}

func NewShellExecutor(logger *slog.Logger) *ShellExecutor {
	return &ShellExecutor{logger: logger}
}

func (s *ShellExecutor) Execute(
	ctx context.Context,
	spec *ConnectorSpec,
	action *ActionSpec,
	params map[string]any,
	cred *Credential,
) (*ExecuteResult, error) {
	start := time.Now()

	// 1) 渲染命令模板
	cmdTpl := action.Command
	if cmdTpl == "" {
		// 没有命令模板？那把 params 拼成简单的 argv
		var sb strings.Builder
		for k, v := range params {
			sb.WriteString(fmt.Sprintf("%s=%v ", k, v))
		}
		cmdTpl = strings.TrimSpace(sb.String())
	}
	cmdStr := renderTemplate(cmdTpl, params)

	// 2) 选择 shell
	shell := action.Shell
	if shell == "" {
		shell = spec.DefaultShell
	}
	if shell == "" {
		shell = "sh"
	}

	// 3) 超时
	timeoutSec := action.Timeout
	if timeoutSec <= 0 {
		timeoutSec = spec.DefaultTimeout
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	// 4) 构造 exec
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	// shell 模式（支持管道、重定向、多命令）
	cmd = exec.CommandContext(ctx, shell, "-c", cmdStr)

	// 设置工作目录
	if action.WorkDir != "" {
		cmd.Dir = action.WorkDir
	}

	// 合并环境变量（系统环境 + connector 级 + action 级）
	env := []string{}
	for k, v := range action.Env {
		env = append(env, k+"="+renderTemplate(v, params))
	}
	cmd.Env = append(cmd.Env, env...)

	// 把凭证塞进环境变量（以 CONNECTOR_ 前缀），避免出现在命令行里
	if cred != nil {
		if cred.APIKey != "" {
			cmd.Env = append(cmd.Env, "CONNECTOR_API_KEY="+cred.APIKey)
		}
		if cred.Token != "" {
			cmd.Env = append(cmd.Env, "CONNECTOR_TOKEN="+cred.Token)
		}
		if cred.Username != "" {
			cmd.Env = append(cmd.Env, "CONNECTOR_USERNAME="+cred.Username)
		}
		if cred.Password != "" {
			cmd.Env = append(cmd.Env, "CONNECTOR_PASSWORD="+cred.Password)
		}
	}

	// 5) 执行
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if s.logger != nil {
		s.logger.DebugContext(ctx, "connector.shell.execute",
			"connector", spec.Name,
			"action", action.Action,
			"shell", shell,
			"cmd", cmdStr,
		)
	}

	err := cmd.Run()
	latency := time.Since(start).Milliseconds()

	outStr := stdout.String()
	errStr := stderr.String()

	// 6) 组装结果
	result := &ExecuteResult{
		Connector: spec.Name,
		Action:    action.Action,
		LatencyMs: latency,
		Raw:       outStr,
	}

	if err != nil {
		result.Success = false
		msg := err.Error()
		if errStr != "" {
			msg = errStr
		}
		result.Error = msg
		return result, nil
	}

	// 7) 期望匹配模式
	if action.Expect != "" {
		re, err := regexp.Compile(action.Expect)
		if err != nil {
			result.Success = false
			result.Error = "invalid expect regex: " + err.Error()
			return result, nil
		}
		if !re.MatchString(outStr) && !re.MatchString(errStr) {
			result.Success = false
			result.Error = fmt.Sprintf("expect pattern %q not matched", action.Expect)
			return result, nil
		}
	}

	result.Success = true

	// 8) 尝试把 stdout 解析为 JSON；失败则按 ResponseFilter 提取文本；再失败就原样返回
	outTrimmed := strings.TrimSpace(outStr)
	if outTrimmed == "" {
		result.Data = map[string]string{"status": "ok"}
		return result, nil
	}

	// 先尝试 JSON 解析
	var jsonData any
	if err := json.Unmarshal([]byte(outTrimmed), &jsonData); err == nil {
		if action.ResponseFilter != "" {
			bs, _ := json.Marshal(jsonData)
			filtered, ferr := filterJSON(bs, action.ResponseFilter)
			if ferr == nil {
				result.Data = filtered
				return result, nil
			}
		}
		result.Data = jsonData
		return result, nil
	}

	// 文本解析：按行拆分，或返回纯文本
	if action.ResponseFilter != "" {
		// 简单的 text 过滤支持 "line:2" 取第 2 行
		if strings.HasPrefix(action.ResponseFilter, "line:") {
			lineIdx := 0
			fmt.Sscanf(action.ResponseFilter, "line:%d", &lineIdx)
			lines := strings.Split(outTrimmed, "\n")
			if lineIdx >= 1 && lineIdx <= len(lines) {
				result.Data = map[string]string{
					"line": strings.TrimSpace(lines[lineIdx-1]),
				}
				return result, nil
			}
		}
	}

	result.Data = map[string]string{
		"output": outTrimmed,
	}
	return result, nil
}
