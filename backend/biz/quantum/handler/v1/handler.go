// Package v1 提供量子平台的 HTTP 接口。
//
//   GET  /api/v1/quantum/platforms                列出平台
//   POST /api/v1/quantum/platforms                创建平台
//   GET  /api/v1/quantum/platforms/:id            查看平台详情（含 Entity 列表）
//   POST /api/v1/quantum/platforms/:id/entities   注册一个 Entity（文档/API/…）
//   POST /api/v1/quantum/platforms/:id/ask        向平台提一个问题（Bridge 回答）
//   DEL  /api/v1/quantum/platforms/:id            删除平台
package v1

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/GoYoko/web"
	"github.com/google/uuid"
	"github.com/samber/do"

	"github.com/ghshhf/MonkeyCode/backend/pkg/connector"
	"github.com/ghshhf/MonkeyCode/backend/pkg/quantum"
)

// QuantumHandler 量子平台 HTTP 入口
type QuantumHandler struct {
	repo   quantum.PlatformRepo
	connRT *connector.Runtime
}

// NewQuantumHandler 创建并注册路由
func NewQuantumHandler(i *do.Injector) (*QuantumHandler, error) {
	engine := do.MustInvoke[*web.Web](i)
	repo := do.MustInvoke[quantum.PlatformRepo](i)
	connRT := do.MustInvoke[*connector.Runtime](i)

	h := &QuantumHandler{repo: repo, connRT: connRT}

	g := engine.Group("/api/v1/quantum")
	g.GET("/platforms", web.BindHandler(h.listPlatforms))
	g.POST("/platforms", web.BindHandler(h.createPlatform))
	g.GET("/platforms/:id", web.BindHandler(h.getPlatform))
	g.DELETE("/platforms/:id", web.BindHandler(h.deletePlatform))
	g.POST("/platforms/:id/entities", web.BindHandler(h.addEntity))
	g.POST("/platforms/:id/ask", web.BindHandler(h.ask))

	return h, nil
}

// ---------- DTOs ----------

type listPlatformsReq struct {
	UserID string `query:"user_id"`
}

type platformDTO struct {
	ID         string          `json:"id"`
	UserID     string          `json:"user_id"`
	Name       string          `json:"name"`
	Desc       string          `json:"description"`
	EntityCnt  int             `json:"entity_count"`
	Entities   []entitySummary `json:"entities,omitempty"`
	CreatedAt  string          `json:"created_at"`
}

type entitySummary struct {
	Name        string   `json:"name"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords,omitempty"`
}

type createPlatformReq struct {
	Name string `json:"name" validate:"required"`
	Desc string `json:"description"`
}

type addEntityReq struct {
	ID   string `param:"id"`
	// kind: "document" | "connector"
	Kind        string          `json:"kind" validate:"required"`
	Name        string          `json:"name" validate:"required"`
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Content     string          `json:"content"`
	Connector   json.RawMessage `json:"connector"`
	ChunkSize   int             `json:"chunk_size"`
	Overlap     int             `json:"overlap"`
}

type askReq struct {
	ID       string `param:"id"`
	Question string `json:"question" validate:"required,min=1"`
}

type askResp struct {
	Answer          string       `json:"answer"`
	Answered        bool         `json:"answered"`
	InvokedEntities []string     `json:"invoked_entities"`
	Sources         []sourceRef  `json:"sources"`
	LatencyMs       int64        `json:"latency_ms"`
}

type sourceRef struct {
	Entity   string `json:"entity"`
	Label    string `json:"label"`
	Location string `json:"location"`
	Preview  string `json:"preview"`
}

// ---------- Handlers ----------

func (h *QuantumHandler) listPlatforms(c *web.Context, req listPlatformsReq) error {
	userID := uuid.Nil
	if req.UserID != "" {
		if id, err := uuid.Parse(req.UserID); err == nil {
			userID = id
		}
	}
	list, err := h.repo.List(c.Request().Context(), userID)
	if err != nil {
		return err
	}
	out := make([]platformDTO, 0, len(list))
	for _, p := range list {
		out = append(out, platformDTO{
			ID:        p.ID.String(),
			UserID:    p.UserID.String(),
			Name:      p.Name,
			Desc:      p.Desc,
			EntityCnt: len(p.Entities),
			CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return c.Success(out)
}

func (h *QuantumHandler) createPlatform(c *web.Context, req createPlatformReq) error {
	p := &quantum.Platform{
		ID:       uuid.New(),
		UserID:   uuid.Nil, // 简化：当前版本不绑定用户
		Name:     req.Name,
		Desc:     req.Desc,
		Entities: []quantum.Entity{},
	}
	if err := h.repo.Create(c.Request().Context(), p); err != nil {
		return err
	}
	return c.Success(platformDTO{
		ID:        p.ID.String(),
		UserID:    p.UserID.String(),
		Name:      p.Name,
		Desc:      p.Desc,
		EntityCnt: 0,
		CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

type getPlatformReq struct {
	ID string `param:"id"`
}

func (h *QuantumHandler) getPlatform(c *web.Context, req getPlatformReq) error {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return c.Success(nil)
	}
	p, err := h.repo.Get(c.Request().Context(), id)
	if err != nil {
		return err
	}
	ents := make([]entitySummary, 0, len(p.Entities))
	for _, e := range p.Entities {
		prof := e.Profile()
		ents = append(ents, entitySummary{
			Name:        prof.Name,
			Label:       prof.Label,
			Kind:        string(prof.Kind),
			Description: prof.Description,
			Keywords:    prof.Keywords,
		})
	}
	return c.Success(platformDTO{
		ID:        p.ID.String(),
		UserID:    p.UserID.String(),
		Name:      p.Name,
		Desc:      p.Desc,
		EntityCnt: len(p.Entities),
		Entities:  ents,
		CreatedAt: p.CreatedAt.Format("2006-01-02 15:04:05"),
	})
}

type deletePlatformReq struct {
	ID string `param:"id"`
}

func (h *QuantumHandler) deletePlatform(c *web.Context, req deletePlatformReq) error {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return c.Success(nil)
	}
	_ = h.repo.Delete(c.Request().Context(), id)
	return c.Success(map[string]string{"status": "deleted"})
}

func (h *QuantumHandler) addEntity(c *web.Context, req addEntityReq) error {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return c.Success(nil)
	}
	p, err := h.repo.Get(c.Request().Context(), id)
	if err != nil {
		return err
	}

	var entity quantum.Entity

	switch strings.ToLower(req.Kind) {
	case "document":
		if strings.TrimSpace(req.Content) == "" {
			return c.JSON(400, map[string]string{"error": "document kind requires non-empty content"})
		}
		entity = quantum.NewDocumentEntity(
			req.Name,
			firstOr(req.Label, req.Name),
			req.Description,
			req.Content,
			req.ChunkSize,
			req.Overlap,
		)
	case "connector":
		if len(req.Connector) == 0 {
			return c.JSON(400, map[string]string{"error": "connector kind requires 'connector' JSON spec"})
		}
		var spec connector.ConnectorSpec
		if err := json.Unmarshal(req.Connector, &spec); err != nil {
			return c.JSON(400, map[string]string{"error": "invalid connector spec: " + err.Error()})
		}
		if spec.Name == "" {
			spec.Name = req.Name
		}
		// 注册到 connector runtime，并包装为 Entity
		h.connRT.Register(&spec)
		entity = quantum.NewConnectorEntity(h.connRT, &spec, nil)
	default:
		return c.JSON(400, map[string]string{"error": "unknown entity kind: " + req.Kind + " (supported: document, connector)"})
	}

	p.Entities = append(p.Entities, entity)
	if err := h.repo.Update(c.Request().Context(), p); err != nil {
		return err
	}

	prof := entity.Profile()
	return c.Success(entitySummary{
		Name:        prof.Name,
		Label:       prof.Label,
		Kind:        string(prof.Kind),
		Description: prof.Description,
		Keywords:    prof.Keywords,
	})
}

func (h *QuantumHandler) ask(c *web.Context, req askReq) error {
	id, err := uuid.Parse(req.ID)
	if err != nil {
		return c.Success(nil)
	}
	p, err := h.repo.Get(c.Request().Context(), id)
	if err != nil {
		return err
	}

	// 创建 Bridge 和 Session
	bridge := quantum.NewBridge(nil, quantum.DefaultSessionConfig())
	sess := bridge.NewSession(p.UserID, p.Entities)

	// 调用 Bridge（没有 LLM 也能工作 —— 用启发式汇总）
	ans, err := bridge.Ask(context.Background(), sess, req.Question)
	if err != nil {
		return err
	}

	// 组装来源引用
	sources := make([]sourceRef, 0, len(ans.EntityResults))
	for _, r := range ans.EntityResults {
		for _, frag := range r.Fragments {
			preview := frag.Content
			if len(preview) > 160 {
				preview = preview[:160] + "..."
			}
			sources = append(sources, sourceRef{
				Entity:   r.Profile.Name,
				Label:    r.Profile.Label,
				Location: frag.SourceRef,
				Preview:  preview,
			})
		}
	}

	return c.Success(askResp{
		Answer:          ans.Content,
		Answered:        ans.Answered,
		InvokedEntities: ans.InvokedEntities,
		Sources:         sources,
		LatencyMs:       ans.LatencyMs,
	})
}

func firstOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
