package biz

import (
	"context"

	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/chaitin/MonkeyCode/backend/biz/di"
	"github.com/chaitin/MonkeyCode/backend/biz/host"
	"github.com/chaitin/MonkeyCode/backend/biz/notify"
	"github.com/chaitin/MonkeyCode/backend/biz/task"
	"github.com/chaitin/MonkeyCode/backend/biz/user"
	"github.com/chaitin/MonkeyCode/backend/biz/vmidle"
	"github.com/chaitin/MonkeyCode/backend/consts"
	"github.com/chaitin/MonkeyCode/backend/domain"
)

// RegisterAll 注册核心业务模块。
// user/task/host/notify 四个标准模块已走 di.Module 表驱动；
// 其余模块暂时保留 ProvideXxx/InvokeXxx 形式，待后续逐步迁移。
func RegisterAll(i *do.Injector) error {
	di.RegisterModules(i,
		user.Module,
		task.Module,
		host.Module,
		notify.Module,
	)
	vmidle.ProvideVMIdle(i)
	vmidle.InvokeVMIdle(i)
	return nil
}

// InvokeAll 目前为空壳——标准模块的 Invoke 已在 RegisterAll 内联动完成。
// 保留以兼容外层调用模式。
func InvokeAll(i *do.Injector) {}

// RegisterOpenSource 注册开源版专用模块。
func RegisterOpenSource(i *do.Injector) {
	do.ProvideValue[domain.TaskHook](i, &taskhook{})
}

func InvokeOpenSource(i *do.Injector) {}

// taskhook 是开源版对 domain.TaskHook 的空实现。
type taskhook struct{}

func (t *taskhook) GetMaxConcurrent(ctx context.Context, uid uuid.UUID) (int, error) {
	return 3, nil
}

func (t *taskhook) GetSystemPrompt(ctx context.Context, taskType consts.TaskType, subType consts.TaskSubType) (string, error) {
	return "", nil
}

func (t *taskhook) GitTask(ctx context.Context, id uuid.UUID) (*domain.GitTask, error) {
	return &domain.GitTask{}, nil
}

func (t *taskhook) OnTaskCreated(ctx context.Context, task *domain.ProjectTask) error {
	return nil
}

var _ domain.TaskHook = &taskhook{}
