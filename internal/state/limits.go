package state

// Run list limits shared by SQLite queries and HTTP/CLI surfaces.
const (
	DefaultRunListLimit = 50
	MaxRunListLimit     = 500
)

// DefaultEnvironment is the deployment/runtime env when none is selected (matches CLI planEnvironment).
const DefaultEnvironment = "local"

// ClampRunListLimit returns limit clamped to [DefaultRunListLimit, MaxRunListLimit] when limit <= 0 or above max.
func ClampRunListLimit(limit int) int {
	if limit <= 0 {
		return DefaultRunListLimit
	}
	if limit > MaxRunListLimit {
		return MaxRunListLimit
	}
	return limit
}
