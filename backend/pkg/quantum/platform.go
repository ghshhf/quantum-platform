package quantum

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Platform 是"量子平台"的业务实体。
// 一个用户/团队可以有多个 Platform，
// 每个 Platform 就是一组 Entity（文档、API 服务、数据源…）。
//
// Platform & PlatformRepo 定义在 pkg 层而不是 biz 层，是为了避免 import 循环：
//   biz/quantum → biz/quantum/handler/v1 → biz/quantum   ← 循环 ❌
//   biz/quantum → pkg/quantum ← biz/quantum/handler/v1     ← OK
type Platform struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Name      string
	Desc      string
	Entities  []Entity
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PlatformRepo 是 Platform 的存储接口。
// 具体实现放在 biz/quantum 层（in-memory / sql / redis 等）。
type PlatformRepo interface {
	Create(ctx context.Context, p *Platform) error
	Get(ctx context.Context, id uuid.UUID) (*Platform, error)
	List(ctx context.Context, userID uuid.UUID) ([]*Platform, error)
	Update(ctx context.Context, p *Platform) error
	Delete(ctx context.Context, id uuid.UUID) error
}
