package statejson

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

func TestParseRunListLimit(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{"", state.DefaultRunListLimit},
		{"10", 10},
		{"0", state.DefaultRunListLimit},
		{"-1", state.DefaultRunListLimit},
		{"abc", state.DefaultRunListLimit},
		{"9999", state.MaxRunListLimit},
		{"500", state.MaxRunListLimit},
	}
	for _, tc := range tests {
		if got := ParseRunListLimit(tc.raw); got != tc.want {
			t.Fatalf("ParseRunListLimit(%q)=%d want %d", tc.raw, got, tc.want)
		}
	}
}
