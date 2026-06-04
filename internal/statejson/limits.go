package statejson

import (
	"strconv"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// ParseRunListLimit parses an optional HTTP/query limit string using [state.ClampRunListLimit].
func ParseRunListLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return state.DefaultRunListLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return state.DefaultRunListLimit
	}
	return state.ClampRunListLimit(n)
}
