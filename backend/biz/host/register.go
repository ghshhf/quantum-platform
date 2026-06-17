package host

import (
	"github.com/samber/do"

	"github.com/ghshhf/quantum-platform/backend/biz/di"
	v1 "github.com/ghshhf/quantum-platform/backend/biz/host/handler/v1"
	"github.com/ghshhf/quantum-platform/backend/biz/host/repo"
	"github.com/ghshhf/quantum-platform/backend/biz/host/usecase"
)

// Module 描述 host 模块的 DI 注册行为。
var Module = di.Module{
	Provide: func(i *do.Injector) {
		do.Provide(i, repo.NewHostRepo)
		do.Provide(i, usecase.NewHostUsecase)
		do.Provide(i, v1.NewHostHandler)
		do.Provide(i, v1.NewInternalHostHandler)
	},
	Invoke: func(i *do.Injector) {
		do.MustInvoke[*v1.HostHandler](i)
		do.MustInvoke[*v1.InternalHostHandler](i)
	},
}
