package v1

import (
    "encoding/json"
    "errors"
    "net/http"

    "github.com/GoYoko/web"
    "github.com/samber/do"

    "github.com/ghshhf/quantum-platform/backend/errcode"
    "github.com/ghshhf/quantum-platform/backend/pkg/modeldownloader"
)

// LocalModelHandler 本地模型下载与管理
type LocalModelHandler struct {
    manager *modeldownloader.Manager
}

// NewLocalModelHandler 创建并注册路由
func NewLocalModelHandler(i *do.Injector) (*LocalModelHandler, error) {
    w := do.MustInvoke[*web.Web](i)
    manager := do.MustInvoke[*modeldownloader.Manager](i)

    h := &LocalModelHandler{manager: manager}

    v1 := w.Group("/api/v1/local-models")
    v1.GET("/catalog", web.BindHandler(h.getCatalog))
    v1.GET("/tasks", web.BindHandler(h.listTasks))
    v1.POST("/download", web.BindHandler(h.startDownload))
    v1.POST("/tasks/:id/cancel", web.BindHandler(h.cancel))
    v1.DELETE("/tasks/:id", web.BindHandler(h.deleteTask))
    v1.GET("/events", web.BindHandler(h.sseEvents))

    return h, nil
}

// —— 响应 DTOs ——

type emptyReq struct{}

type downloadReq struct {
    ModelID string `json:"model_id"`
}

type taskIDReq struct {
    ID string `param:"id"`
}

// —— Handlers ——

func (h *LocalModelHandler) getCatalog(c *web.Context, _ emptyReq) error {
    return c.Success(h.manager.Catalog())
}

func (h *LocalModelHandler) listTasks(c *web.Context, _ emptyReq) error {
    return c.Success(h.manager.Tasks())
}

func (h *LocalModelHandler) startDownload(c *web.Context, req downloadReq) error {
    info, err := h.manager.Start(req.ModelID)
    if err != nil {
        return errcode.ErrBadRequest.Wrap(err)
    }
    return c.Success(info)
}

func (h *LocalModelHandler) cancel(c *web.Context, req taskIDReq) error {
    if err := h.manager.Cancel(req.ID); err != nil {
        return errcode.ErrBadRequest.Wrap(err)
    }
    return c.Success(nil)
}

func (h *LocalModelHandler) deleteTask(c *web.Context, req taskIDReq) error {
    if err := h.manager.Delete(req.ID); err != nil {
        return errcode.ErrBadRequest.Wrap(err)
    }
    return c.Success(nil)
}

// sseEvents SSE 订阅下载进度，不走 JSON 响应
func (h *LocalModelHandler) sseEvents(c *web.Context, _ emptyReq) error {
    w := c.Response().Writer
    flusher, ok := w.(http.Flusher)
    if !ok {
        return errors.New("streaming unsupported")
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(http.StatusOK)
    flusher.Flush()

    ch := h.manager.Subscribe()
    defer h.manager.Unsubscribe(ch)

    ctx := c.Request().Context()
    for {
        select {
        case <-ctx.Done():
            return nil
        case ev, ok := <-ch:
            if !ok { return nil }
            if ev.Type != "" {
                w.Write([]byte("event: " + ev.Type + "\n"))
            }
            data, _ := json.Marshal(ev)
            w.Write([]byte("data: " + string(data) + "\n\n"))
            flusher.Flush()
        }
    }
}
