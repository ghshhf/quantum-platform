package localmodel

import (
    "os"

    "github.com/samber/do"

    v1 "github.com/ghshhf/MonkeyCode/backend/biz/localmodel/handler/v1"
    "github.com/ghshhf/MonkeyCode/backend/pkg/modeldownloader"
)

// ProvideLocalModel 注册本地模型管理模块
func ProvideLocalModel(i *do.Injector) {
    do.Provide(i, func(i *do.Injector) (*modeldownloader.Manager, error) {
        root := os.Getenv("MCAI_MODEL_DIR")
        ollama := os.Getenv("MCAI_OLLAMA_URL")
        return modeldownloader.NewManager(root, ollama)
    })
    do.Provide(i, v1.NewLocalModelHandler)
}

// InvokeLocalModel 触发 Handler 初始化并注册路由
func InvokeLocalModel(i *do.Injector) {
    do.MustInvoke[*v1.LocalModelHandler](i)
}
