package user

import (
	"github.com/samber/do"

	"github.com/ghshhf/MonkeyCode/backend/biz/di"
	v1 "github.com/ghshhf/MonkeyCode/backend/biz/user/handler/v1"
	"github.com/ghshhf/MonkeyCode/backend/biz/user/repo"
	"github.com/ghshhf/MonkeyCode/backend/biz/user/usecase"
)

// Module 描述 user 模块的 DI 注册行为。
var Module = di.Module{
	Provide: func(i *do.Injector) {
		do.Provide(i, repo.NewUserRepo)
		do.Provide(i, repo.NewUserActiveRepo)
		do.Provide(i, usecase.NewUserUsecase)
		do.Provide(i, v1.NewAuthHandler)
	},
	Invoke: func(i *do.Injector) {
		do.MustInvoke[*v1.AuthHandler](i)
	},
}
