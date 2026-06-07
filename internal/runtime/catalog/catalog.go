// Package catalog lists workflow runtime names validated at spec time (issue #114).
// Execution adapters register additional names via [runtime.Register].
package catalog

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// NameLocal is the built-in disk-backed workflow runtime (MVP).
const NameLocal = "local"

var (
	mu    sync.RWMutex
	names = map[string]struct{}{
		NameLocal: {},
	}
)

// Register adds a runtime name known to validate. Duplicate names are ignored.
func Register(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		panic("runtime catalog: empty name")
	}
	mu.Lock()
	defer mu.Unlock()
	names[name] = struct{}{}
}

// IsKnown reports whether name is a registered runtime identifier.
// Empty means implicit local.
func IsKnown(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	mu.RLock()
	_, ok := names[name]
	mu.RUnlock()
	return ok
}

// KnownNames returns sorted registered runtime identifiers.
func KnownNames() []string {
	mu.RLock()
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	mu.RUnlock()
	sort.Strings(out)
	return out
}

// ErrUnknownRuntime indicates spec.runtime names a runtime that is not registered.
type ErrUnknownRuntime struct {
	Name string
}

func (e *ErrUnknownRuntime) Error() string {
	if e == nil || strings.TrimSpace(e.Name) == "" {
		return "runtime: unknown runtime"
	}
	return fmt.Sprintf("runtime: unknown runtime %q (valid: %s)", e.Name, strings.Join(KnownNames(), ", "))
}

// ResetForTest removes a runtime name registered during tests.
func ResetForTest(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	mu.Lock()
	delete(names, name)
	mu.Unlock()
}
