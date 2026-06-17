package connector

import (
	"github.com/samber/do"

	v1 "github.com/ghshhf/MonkeyCode/backend/biz/connector/handler/v1"
	"github.com/ghshhf/MonkeyCode/backend/pkg/connector"
)

// Provide 注册 connector 模块依赖。
func Provide(i *do.Injector) {
	// Runtime 是全局单例：注册表 + 执行后端
	do.Provide(i, func(i *do.Injector) (*connector.Runtime, error) {
		// 注意：这里不调用 logger，因为我们用 slog.Default() 更简单
		rt := connector.NewRuntime(nil)
		// 尝试从内置目录加载 connector spec
		if count, err := rt.LoadDir("connectors"); err == nil && count > 0 {
			// 静默加载
			_ = count
		}
		return rt, nil
	})
	do.Provide(i, v1.NewConnectorHandler)
}

// Invoke 触发 Handler 注册路由（无额外 goroutine）。
func Invoke(i *do.Injector) {
	// 确保 Handler 已被 DI 解析并注册路由
	do.MustInvoke[*v1.ConnectorHandler](i)
}
