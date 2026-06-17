package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// Executor 是"执行后端"的抽象接口。
// 目前有 HTTPExecutor 和 ShellExecutor 两种实现。
// 未来可以加 ScriptExecutor、FileExecutor 等。
type Executor interface {
	Execute(ctx context.Context, spec *ConnectorSpec, action *ActionSpec,
		params map[string]any, cred *Credential) (*ExecuteResult, error)
}

// Registry 保存已加载的 ConnectorSpec，并提供查询接口。
// 线程安全，支持运行时动态加载/卸载。
type Registry struct {
	mu         sync.RWMutex
	specs      map[string]*ConnectorSpec
	logger     *slog.Logger
}

func NewRegistry(logger *slog.Logger) *Registry {
	return &Registry{
		specs:  make(map[string]*ConnectorSpec),
		logger: logger,
	}
}

// Register 注册一个 ConnectorSpec。已存在同名会覆盖。
func (r *Registry) Register(spec *ConnectorSpec) {
	if spec == nil || spec.Name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.specs[spec.Name] = spec
	if r.logger != nil {
		r.logger.Debug("connector registered", "name", spec.Name, "type", spec.Type, "actions", len(spec.Actions))
	}
}

// RegisterJSON 从 JSON 字节注册。
func (r *Registry) RegisterJSON(data []byte) error {
	var spec ConnectorSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("parse connector spec: %w", err)
	}
	r.Register(&spec)
	return nil
}

// Unregister 按 name 移除。
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.specs, name)
}

// Get 按 name 获取。
func (r *Registry) Get(name string) (*ConnectorSpec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[name]
	return spec, ok
}

// List 返回所有已注册 Connector 的概览信息。
func (r *Registry) List() []ConnectorInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ConnectorInfo, 0, len(r.specs))
	for _, spec := range r.specs {
		caps := make([]Capability, 0, len(spec.Actions))
		for _, a := range spec.Actions {
			caps = append(caps, Capability{
				Action:      a.Action,
				Label:       a.Label,
				Description: a.Description,
				Params:      a.Params,
				Category:    a.Category,
				DocURL:      a.DocURL,
			})
		}
		requiresAuth := spec.Auth.Type != "" && spec.Auth.Type != "none"
		out = append(out, ConnectorInfo{
			Name:         spec.Name,
			Label:        spec.Label,
			Description:  spec.Description,
			Type:         spec.Type,
			Version:      spec.Version,
			Vendor:       spec.Vendor,
			Icon:         spec.Icon,
			Tags:         spec.Tags,
			Capabilities: caps,
			AuthType:     spec.Auth.Type,
			RequiresAuth: requiresAuth,
		})
	}
	return out
}

// LoadDir 扫描目录下所有 *.json 文件并注册为 ConnectorSpec。
func (r *Registry) LoadDir(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		full := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(full)
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("connector: read file failed", "file", full, "err", err)
			}
			continue
		}
		if err := r.RegisterJSON(data); err != nil {
			if r.logger != nil {
				r.logger.Warn("connector: parse file failed", "file", full, "err", err)
			}
			continue
		}
		count++
	}
	return count, nil
}

// ---------- Runtime ----------

// Runtime 是对外的统一入口：
//  1. 保存所有注册的 ConnectorSpec
//  2. 按 spec.Type 选择正确的 Executor
//  3. 做参数校验、调用日志、错误统一处理
type Runtime struct {
	reg       *Registry
	executors map[string]Executor
	logger    *slog.Logger
}

func NewRuntime(logger *slog.Logger) *Runtime {
	reg := NewRegistry(logger)
	return &Runtime{
		reg:    reg,
		logger: logger,
		executors: map[string]Executor{
			"http":  NewHTTPExecutor(logger),
			"shell": NewShellExecutor(logger),
		},
	}
}

func (rt *Runtime) Registry() *Registry { return rt.reg }

// Execute 是对外的核心调用入口：
//   name   - connector 名
//   action - action 名
//   params - 用户参数
//   cred   - 可选凭证（取决于 connector 的 auth.Type）
func (rt *Runtime) Execute(
	ctx context.Context,
	name, action string,
	params map[string]any,
	cred *Credential,
) (*ExecuteResult, error) {
	// 1) 取 spec
	spec, ok := rt.reg.Get(name)
	if !ok {
		return nil, fmt.Errorf("connector %q not found", name)
	}

	// 2) 选择 executor
	exec, ok := rt.executors[spec.Type]
	if !ok {
		return nil, fmt.Errorf("connector type %q not supported", spec.Type)
	}

	// 3) 查找 action
	var target *ActionSpec
	for i := range spec.Actions {
		if spec.Actions[i].Action == action {
			target = &spec.Actions[i]
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("action %q not found in connector %q", action, name)
	}

	// 4) 检查必填参数
	merged := make(map[string]any, len(params))
	for k, v := range params {
		merged[k] = v
	}
	for _, p := range target.Params {
		if _, has := merged[p.Name]; !has {
			if p.Required && p.Default == nil {
				return nil, fmt.Errorf("param %q is required", p.Name)
			}
			if p.Default != nil {
				merged[p.Name] = p.Default
			}
		}
	}

	// 5) 执行
	return exec.Execute(ctx, spec, target, merged, cred)
}

// List 返回运行时所有 connector 的信息列表（供前端"发现"接口使用）。
func (rt *Runtime) List() []ConnectorInfo {
	return rt.reg.List()
}

// Get 获取单个 connector 的完整信息。
func (rt *Runtime) Get(name string) (*ConnectorInfo, bool) {
	spec, ok := rt.reg.Get(name)
	if !ok {
		return nil, false
	}
	caps := make([]Capability, 0, len(spec.Actions))
	for _, a := range spec.Actions {
		caps = append(caps, Capability{
			Action:      a.Action,
			Label:       a.Label,
			Description: a.Description,
			Params:      a.Params,
			Category:    a.Category,
			DocURL:      a.DocURL,
		})
	}
	return &ConnectorInfo{
		Name:         spec.Name,
		Label:        spec.Label,
		Description:  spec.Description,
		Type:         spec.Type,
		Version:      spec.Version,
		Vendor:       spec.Vendor,
		Icon:         spec.Icon,
		Tags:         spec.Tags,
		Capabilities: caps,
		AuthType:     spec.Auth.Type,
		RequiresAuth: spec.Auth.Type != "" && spec.Auth.Type != "none",
	}, true
}

// Register 是 rt.reg.Register 的快捷方式。
func (rt *Runtime) Register(spec *ConnectorSpec) { rt.reg.Register(spec) }

// LoadDir 从目录加载所有 .json connector spec。
func (rt *Runtime) LoadDir(dir string) (int, error) { return rt.reg.LoadDir(dir) }
