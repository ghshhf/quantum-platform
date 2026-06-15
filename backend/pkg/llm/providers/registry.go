package providers

import (
	"fmt"
	"sync"
)

// ProviderFactory 根据 Config 构造一个 Provider
type ProviderFactory func(Config) (Provider, error)

var (
	registryMu sync.RWMutex
	factories  = make(map[string]ProviderFactory)
)

// Register 注册一个 provider factory
func Register(name string, factory ProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if factory == nil {
		panic("providers: Register factory is nil")
	}
	factories[name] = factory
}

// New 根据 Config 构造 Provider
func New(cfg Config) (Provider, error) {
	registryMu.RLock()
	factory, ok := factories[cfg.Provider]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("providers: unknown provider %q (registered: %v)", cfg.Provider, Registered())
	}
	provider, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("providers: failed to create provider %q: %w", cfg.Provider, err)
	}
	return provider, nil
}

// Registered 返回已注册的 provider 名称列表
func Registered() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	return names
}
