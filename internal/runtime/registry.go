package runtime

import (
	"fmt"
	"strings"
	"sync"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/catalog"
)

// NameLocal is the built-in disk-backed workflow runtime (MVP).
const NameLocal = catalog.NameLocal

// ErrUnknownRuntime indicates spec.runtime names a runtime that is not registered.
type ErrUnknownRuntime = catalog.ErrUnknownRuntime

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Factory constructs a [Runtime] from shared dependencies supplied by the control plane.
type Factory func(Deps) (Runtime, error)

// Register adds a runtime factory and catalog name. Duplicate names panic at init time.
func Register(name string, factory Factory) {
	name = strings.TrimSpace(name)
	if name == "" {
		panic("runtime: empty runtime name")
	}
	if factory == nil {
		panic("runtime: nil factory for " + name)
	}
	catalog.Register(name)
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic("runtime: duplicate registration for " + name)
	}
	registry[name] = factory
}

// Lookup returns the factory for a registered runtime name.
func Lookup(name string) (Factory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = NameLocal
	}
	if !catalog.IsKnown(name) {
		return nil, &ErrUnknownRuntime{Name: name}
	}
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("runtime: %q is known but no factory is registered (import runtime adapter package)", name)
	}
	return factory, nil
}

// IsKnown reports whether name is a registered runtime identifier.
func IsKnown(name string) bool {
	return catalog.IsKnown(name)
}

// KnownNames returns sorted registered runtime identifiers.
func KnownNames() []string {
	return catalog.KnownNames()
}
