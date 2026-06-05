package spec

import (
	"errors"
	"strings"
	"testing"
)

func TestParseResourceFromBytes_unknownFieldWithSuggestion(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  defualts:
    model: openai/gpt-4
`
	_, err := ParseResourceFromBytes([]byte(y), "project.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("want *LoadError, got %T: %v", err, err)
	}
	if !errors.Is(err, ErrUnknownField) {
		t.Fatalf("want ErrUnknownField in chain, got %v", err)
	}
	msg := le.Error()
	if !strings.Contains(msg, "project.yaml") {
		t.Fatalf("want path in error: %q", msg)
	}
	if !strings.Contains(msg, `unknown field "defualts"`) {
		t.Fatalf("want unknown field message: %q", msg)
	}
	if !strings.Contains(msg, "defaults") {
		t.Fatalf("want suggestion for defaults: %q", msg)
	}
}

func TestParseResourceFromBytes_unknownFieldInToolSafety(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: helper
spec:
  type: http
  http:
    baseUrl: https://example.com
  safety:
    reqiresApproval: true
`
	_, err := ParseResourceFromBytes([]byte(y), "tools/helper.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "reqiresApproval") {
		t.Fatalf("want field name in error: %v", err)
	}
	if !strings.Contains(err.Error(), "requiresApproval") {
		t.Fatalf("want suggestion: %v", err)
	}
}

func TestParseResourceFromBytes_unknownTopLevelField(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Agent
metadata:
  name: a
spec: {}
extra: true
`
	_, err := ParseResourceFromBytes([]byte(y), "agent.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUnknownField) {
		t.Fatalf("want ErrUnknownField, got %v", err)
	}
}

func TestSuggestYAMLField_typo(t *testing.T) {
	if got := suggestYAMLField("spec.ProjectSpec", "defualts"); got != "defaults" {
		t.Fatalf("suggestYAMLField = %q, want defaults", got)
	}
}

func TestLevenshtein(t *testing.T) {
	if levenshtein("defaults", "defualts") != 2 {
		t.Fatalf("unexpected distance")
	}
}
