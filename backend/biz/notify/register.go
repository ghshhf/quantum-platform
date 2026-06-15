package notify

import (
	"github.com/samber/do"

	"github.com/chaitin/MonkeyCode/backend/biz/di"
	v1 "github.com/chaitin/MonkeyCode/backend/biz/notify/handler/v1"
	"github.com/chaitin/MonkeyCode/backend/biz/notify/repo"
	"github.com/chaitin/MonkeyCode/backend/biz/notify/usecase"
)

// Module 描述 notify 模块的 DI 注册行为。
var Module = di.Module{
	Provide: func(i *do.Injector) {
		do.Provide(i, repo.NewNotifyChannelRepo)
		do.Provide(i, repo.NewNotifySubscriptionRepo)
		do.Provide(i, repo.NewNotifySendLogRepo)
		do.Provide(i, usecase.NewNotifyChannelUsecase)
		do.Provide(i, usecase.NewWechatMPUsecase)
		do.Provide(i, v1.NewNotifyHandler)
		do.Provide(i, v1.NewWechatMPHandler)
		do.Provide(i, v1.NewWechatCallbackHandler)
	},
	Invoke: func(i *do.Injector) {
		do.MustInvoke[*v1.NotifyHandler](i)
		do.MustInvoke[*v1.WechatMPHandler](i)
		do.MustInvoke[*v1.WechatCallbackHandler](i)
	},
}
