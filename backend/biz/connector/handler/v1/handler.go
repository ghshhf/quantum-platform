package v1

import (
	"github.com/GoYoko/web"
	"github.com/samber/do"

	"github.com/ghshhf/MonkeyCode/backend/errcode"
	"github.com/ghshhf/MonkeyCode/backend/pkg/connector"
)

// ConnectorHandler connector 模块的 HTTP 入口。
type ConnectorHandler struct {
	rt *connector.Runtime
}

// NewConnectorHandler 创建并注册路由。
func NewConnectorHandler(i *do.Injector) (*ConnectorHandler, error) {
	engine := do.MustInvoke[*web.Web](i)
	rt := do.MustInvoke[*connector.Runtime](i)

	h := &ConnectorHandler{rt: rt}

	// 注册路由
	g := engine.Group("/api/v1/connectors")
	g.GET("", web.BindHandler(h.listConnectors))
	g.GET("/:name", web.BindHandler(h.getConnector))
	g.POST("/:name/execute/:action", web.BindHandler(h.executeAction))
	g.POST("/spec", web.BindHandler(h.registerSpec))
	g.DELETE("/:name", web.BindHandler(h.removeConnector))

	return h, nil
}

// ===== Handlers =====

// listConnectors 返回所有已注册 connector 的信息列表（不含敏感字段）。
func (h *ConnectorHandler) listConnectors(c *web.Context, _ emptyConnectorsReq) error {
	list := h.rt.List()
	return c.Success(list)
}

type getConnectorReq struct {
	Name string `param:"name"`
}

// getConnector 返回一个 connector 的详细信息（能力清单）。
func (h *ConnectorHandler) getConnector(c *web.Context, req getConnectorReq) error {
	info, ok := h.rt.Get(req.Name)
	if !ok {
		return errcode.ErrNotFound
	}
	return c.Success(info)
}

type emptyConnectorsReq struct{}

type executeActionReq struct {
	Name   string         `param:"name"`
	Action string         `param:"action"`
	Params map[string]any `json:"params"`
	// 凭证：暂不在请求中直接传，未来可以通过团队设置/安全策略
	Credential *connector.Credential `json:"credential,omitempty"`
}

// executeAction 执行某个 connector 的某个 action。
// 这是对外的核心入口。前端拿到 capabilities 后选择 action、填参数然后调用。
func (h *ConnectorHandler) executeAction(c *web.Context, req executeActionReq) error {
	if req.Name == "" || req.Action == "" {
		return errcode.ErrBadRequest
	}
	result, err := h.rt.Execute(c.Request().Context(),
		req.Name, req.Action, req.Params, req.Credential)
	if err != nil {
		return errcode.ErrBadRequest.Wrap(err)
	}
	return c.Success(result)
}

type registerSpecReq struct {
	connector.ConnectorSpec
}

// registerSpec 动态注册一个 connector spec（运行时新增，重启后丢失）。
// 用于快速试错 / 前端调试：把一个 JSON spec 告诉后端"我想试试这个平台"。
func (h *ConnectorHandler) registerSpec(c *web.Context, req registerSpecReq) error {
	if req.Name == "" {
		return errcode.ErrBadRequest
	}
	h.rt.Register(&req.ConnectorSpec)
	info, _ := h.rt.Get(req.Name)
	return c.Success(info)
}

type removeConnectorReq struct {
	Name string `param:"name"`
}

// removeConnector 移除一个 connector（动态注册）。
func (h *ConnectorHandler) removeConnector(c *web.Context, req removeConnectorReq) error {
	_, ok := h.rt.Get(req.Name)
	if !ok {
		return errcode.ErrNotFound
	}
	h.rt.Registry().Unregister(req.Name)
	return c.Success(nil)
}
