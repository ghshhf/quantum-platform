package task

import (
	"github.com/samber/do"

	"github.com/ghshhf/quantum-platform/backend/biz/di"
	v1 "github.com/ghshhf/quantum-platform/backend/biz/task/handler/v1"
	"github.com/ghshhf/quantum-platform/backend/biz/task/repo"
	"github.com/ghshhf/quantum-platform/backend/biz/task/service"
	"github.com/ghshhf/quantum-platform/backend/biz/task/usecase"
)

// Module 描述 task 模块的 DI 注册行为。
var Module = di.Module{
	Provide: func(i *do.Injector) {
		do.Provide(i, usecase.NewTaskUsecase)
		do.Provide(i, usecase.NewGitTaskUsecase)
		do.Provide(i, service.NewTaskActivityRefresher)
		do.Provide(i, service.NewTaskSummaryService)
		do.Provide(i, v1.NewTaskHandler)
		do.Provide(i, repo.NewTaskRepo)
		do.Provide(i, repo.NewGitTaskRepo)
	},
	Invoke: func(i *do.Injector) {
		do.MustInvoke[*v1.TaskHandler](i)
	},
}
