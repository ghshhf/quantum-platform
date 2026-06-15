// Package di 提供 biz 层共享的 DI 注册原语。
// 设计要点：
//   - 每个业务模块声明一个 di.Module 变量，该变量描述其 Provide（注册工厂）和 Invoke（立即实例化）两个行为。
//   - di.RegisterModules 按"先全部 Provide、再全部 Invoke"的两阶段顺序执行，
//     保证跨模块依赖时不会因"某工厂尚未注册就被 Invoke"而出错。
//   - 将 Module 放在独立包而不是 biz 包内，是为了避免 biz ↔ biz/* 子包间的 import cycle。
package di

import (
	"github.com/samber/do"
)

// Module 描述一个业务模块的 DI 注册行为。
type Module struct {
	Provide func(*do.Injector)
	Invoke  func(*do.Injector)
}

// RegisterModules 按"先全部 Provide、再全部 Invoke"的两阶段顺序注册。
// 两阶段分离避免模块间循环依赖时出现"某工厂尚未注册就被 Invoke"的竞态。
func RegisterModules(i *do.Injector, modules ...Module) {
	for _, m := range modules {
		m.Provide(i)
	}
	for _, m := range modules {
		m.Invoke(i)
	}
}
