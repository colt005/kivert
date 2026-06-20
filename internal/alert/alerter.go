package alert

import (
	"context"
	"fmt"
	"sync"
)

// Alerter defines the interface that all alert channels must implement.
type Alerter interface {
	Name() string
	Send(ctx context.Context, a Alert) error
}

// Factory defines the function signature for instantiating an Alerter.
type Factory func(cfg map[string]any) (Alerter, error)

var (
	registryMu sync.RWMutex
	registry   = make(map[string]Factory)
)

// Register registers an Alerter factory under a given kind (type string).
// Channels call Register() from their init() function.
func Register(kind string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if f == nil {
		panic(fmt.Sprintf("alerter: Register factory for %q is nil", kind))
	}
	if _, dup := registry[kind]; dup {
		panic(fmt.Sprintf("alerter: Register called twice for %q", kind))
	}
	registry[kind] = f
}

// Build creates an Alerter instance of the specified kind using the provided config map.
func Build(kind string, cfg map[string]any) (Alerter, error) {
	registryMu.RLock()
	factory, exists := registry[kind]
	registryMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown alert channel kind %q", kind)
	}
	return factory(cfg)
}
