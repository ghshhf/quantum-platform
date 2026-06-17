package p2phandler

import (
	"context"

	"github.com/samber/do"

	v1 "github.com/ghshhf/quantum-platform/backend/biz/p2phandler/handler/v1"
	"github.com/ghshhf/quantum-platform/backend/pkg/p2p"
)

// Provide P2P 模块
func Provide(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*p2p.Node, error) {
		return p2p.NewNode(p2p.NodeOption{})
	})
	do.Provide(i, v1.NewP2PHandler)
}

// Invoke 触发 Handler 注册路由并启动节点
func Invoke(i *do.Injector) {
	do.MustInvoke[*v1.P2PHandler](i)
	node := do.MustInvoke[*p2p.Node](i)
	go node.Start(context.Background())
}
