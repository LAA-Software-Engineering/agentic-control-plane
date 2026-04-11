package spec

import (
	"reflect"
	"testing"
)

func TestParseToolUses(t *testing.T) {
	tests := []struct {
		uses string
		want string
		ok   bool
	}{
		{"tool.github.pull_request.get", "github", true},
		{"tool.github", "github", true},
		{"  tool.slack.message.send  ", "slack", true},
		{"agent.reviewer", "", false},
		{"tool.", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseToolUses(tt.uses)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseToolUses(%q) = (%q, %v), want (%q, %v)", tt.uses, got, ok, tt.want, tt.ok)
		}
	}
}

func TestInterpolationStepRefs(t *testing.T) {
	s := "x ${steps.fetch.output} and ${steps.fetch.nested} ${steps.other.field}"
	got := InterpolationStepRefs(s)
	want := []string{"fetch", "other"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
