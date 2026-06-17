// Package quantum 实现"量子平台"的业务层组装：
//   - inMemoryRepo：Platform 的进程内实现（快速验证用）
//   - Provide/Invoke：DI 注册与 HTTP 路由注册
package quantum

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samber/do"

	handler "github.com/ghshhf/quantum-platform/backend/biz/quantum/handler/v1"
	"github.com/ghshhf/quantum-platform/backend/pkg/connector"
	"github.com/ghshhf/quantum-platform/backend/pkg/quantum"
)

// errNotFound 当 Get 找不到时返回。
type errNotFoundType struct{}

func (e *errNotFoundType) Error() string { return "platform not found" }
func (e *errNotFoundType) HTTPStatus() int { return 404 }

var errNotFound = &errNotFoundType{}

// inMemoryRepo 是进程内的实现，方便快速验证。
type inMemoryRepo struct {
	mu   sync.RWMutex
	data map[uuid.UUID]*quantum.Platform
}

func newInMemoryRepo() quantum.PlatformRepo {
	return &inMemoryRepo{data: make(map[uuid.UUID]*quantum.Platform)}
}

func (r *inMemoryRepo) Create(ctx context.Context, p *quantum.Platform) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	r.data[p.ID] = p
	return nil
}

func (r *inMemoryRepo) Get(ctx context.Context, id uuid.UUID) (*quantum.Platform, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.data[id]
	if !ok {
		return nil, errNotFound
	}
	return p, nil
}

func (r *inMemoryRepo) List(ctx context.Context, userID uuid.UUID) ([]*quantum.Platform, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*quantum.Platform, 0, len(r.data))
	for _, p := range r.data {
		if userID == uuid.Nil || p.UserID == userID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (r *inMemoryRepo) Update(ctx context.Context, p *quantum.Platform) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.data[p.ID]; !ok {
		return errNotFound
	}
	p.UpdatedAt = time.Now()
	r.data[p.ID] = p
	return nil
}

func (r *inMemoryRepo) Delete(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.data, id)
	return nil
}

// Provide 注册依赖。
func Provide(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (quantum.PlatformRepo, error) {
		return newInMemoryRepo(), nil
	})
	do.Provide(i, func(i *do.Injector) (*connector.Runtime, error) {
		if outer, err := do.Invoke[*connector.Runtime](i); err == nil {
			return outer, nil
		}
		return connector.NewRuntime(nil), nil
	})
	do.Provide(i, handler.NewQuantumHandler)
}

// Invoke 确保 Handler 被 DI 解析并注册了路由。
func Invoke(i *do.Injector) {
	_ = do.MustInvoke[*handler.QuantumHandler](i)
}
