package v1

import (
	"github.com/GoYoko/web"
	"github.com/samber/do"

	"github.com/ghshhf/MonkeyCode/backend/errcode"
	"github.com/ghshhf/MonkeyCode/backend/pkg/p2p"
)

// P2PHandler P2P 控制台 HTTP API
type P2PHandler struct {
	node *p2p.Node
}

// NewP2PHandler 创建并注册路由
func NewP2PHandler(i *do.Injector) (*P2PHandler, error) {
	engine := do.MustInvoke[*web.Web](i)
	node := do.MustInvoke[*p2p.Node](i)

	h := &P2PHandler{node: node}

	g := engine.Group("/api/v1/p2p")
	g.GET("/me", web.BindHandler(h.me))
	g.GET("/peers", web.BindHandler(h.peers))
	g.POST("/connect", web.BindHandler(h.connect))
	g.POST("/disconnect", web.BindHandler(h.disconnect))
	// PRP 路由表与节点交换
	g.GET("/routes", web.BindHandler(h.routes))
	g.GET("/hotspot", web.BindHandler(h.getHotspot))
	g.POST("/hotspot", web.BindHandler(h.setHotspot))
	g.GET("/pex", web.BindHandler(h.pex))
	g.POST("/peers", web.BindHandler(h.addPeer))
	g.GET("/files", web.BindHandler(h.files))
	g.POST("/files/share", web.BindHandler(h.shareFile))
	g.POST("/files/download", web.BindHandler(h.downloadFile))
	g.POST("/chat", web.BindHandler(h.chat))

	return h, nil
}

// ===== Handlers =====

type emptyReq struct{}

func (h *P2PHandler) me(c *web.Context, _ emptyReq) error {
	return c.Success(h.node.Self())
}

func (h *P2PHandler) peers(c *web.Context, _ emptyReq) error {
	return c.Success(h.node.Peers())
}

func (h *P2PHandler) files(c *web.Context, _ emptyReq) error {
	return c.Success(h.node.ListSharedFiles())
}

type connectReq struct {
	Addr string `json:"addr"`
}

func (h *P2PHandler) connect(c *web.Context, req connectReq) error {
	if req.Addr == "" {
		return errcode.ErrBadRequest
	}
	if err := h.node.Connect(req.Addr); err != nil {
		return errcode.ErrBadRequest.Wrap(err)
	}
	return c.Success(nil)
}

type disconnectReq struct {
	PeerID string `json:"peer_id"`
}

func (h *P2PHandler) disconnect(c *web.Context, req disconnectReq) error {
	if req.PeerID == "" {
		return errcode.ErrBadRequest
	}
	h.node.Disconnect(req.PeerID)
	return c.Success(nil)
}

// ========= PRP 路由表 & 节点交换 =========

type routesReq struct{}

func (h *P2PHandler) routes(c *web.Context, _ routesReq) error {
	return c.Success(h.node.Routes())
}

type hotspotReq struct{}

func (h *P2PHandler) getHotspot(c *web.Context, _ hotspotReq) error {
	return c.Success(struct {
		On bool `json:"on"`
	}{On: h.node.IsHotspot()})
}

type setHotspotReq struct {
	On bool `json:"on"`
}

func (h *P2PHandler) setHotspot(c *web.Context, req setHotspotReq) error {
	h.node.SetHotspot(req.On)
	return c.Success(nil)
}

type pexReq struct{}

func (h *P2PHandler) pex(c *web.Context, _ pexReq) error {
	return c.Success(struct {
		Candidates int `json:"candidates"`
	}{Candidates: h.node.PexSize()})
}

type addPeerReq struct {
	Addr string `json:"addr"`
}

func (h *P2PHandler) addPeer(c *web.Context, req addPeerReq) error {
	if err := h.node.AddPeerAddress(req.Addr); err != nil {
		return errcode.ErrBadRequest.Wrap(err)
	}
	return c.Success(nil)
}

type shareFileReq struct {
	Path string `json:"path"`
}

func (h *P2PHandler) shareFile(c *web.Context, req shareFileReq) error {
	if req.Path == "" {
		return errcode.ErrBadRequest
	}
	id, err := h.node.ShareFile(req.Path)
	if err != nil {
		return errcode.ErrBadRequest.Wrap(err)
	}
	return c.Success(struct {
		FileID string `json:"file_id"`
	}{FileID: id})
}

type downloadFileReq struct {
	PeerAddr string `json:"peer_addr"`
	FileID   string `json:"file_id"`
	Dest     string `json:"dest"`
}

func (h *P2PHandler) downloadFile(c *web.Context, req downloadFileReq) error {
	if req.PeerAddr == "" || req.FileID == "" || req.Dest == "" {
		return errcode.ErrBadRequest
	}
	man, err := h.node.DownloadFile(c.Request().Context(), req.PeerAddr, req.FileID, req.Dest)
	if err != nil {
		return errcode.ErrBadRequest.Wrap(err)
	}
	return c.Success(man)
}

type chatReq struct {
	Model     string            `json:"model"`
	System    string            `json:"system"`
	Messages  []p2p.ChatMessage `json:"messages"`
	MaxTokens int               `json:"max_tokens"`
}

func (h *P2PHandler) chat(c *web.Context, req chatReq) error {
	if len(req.Messages) == 0 {
		return errcode.ErrBadRequest
	}
	resp, err := h.node.Chat(c.Request().Context(), p2p.ChatRequest{
		Model:     req.Model,
		System:    req.System,
		Messages:  req.Messages,
		MaxTokens: req.MaxTokens,
	})
	if err != nil {
		return errcode.ErrBadRequest.Wrap(err)
	}
	return c.Success(resp)
}
