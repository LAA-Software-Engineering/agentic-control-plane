package engine

import (
	"context"
	"testing"

	"github.com/Terfyn/terfyn/internal/models"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
)

func TestExtractAgentJSONObject(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean object unchanged", `{"x":"ok"}`, `{"x":"ok"}`},
		{"leading and trailing space", "  {\"x\":\"ok\"}\n", `{"x":"ok"}`},
		{"prose preamble", `Perfect, here is the state:` + "\n" + `{"x":"ok"}`, `{"x":"ok"}`},
		{"json code fence", "```json\n{\"x\":\"ok\"}\n```", `{"x":"ok"}`},
		{"bare code fence", "```\n{\"x\":\"ok\"}\n```", `{"x":"ok"}`},
		{"preamble and fence", "Sure!\n```json\n{\"x\":\"ok\"}\n```\nDone.", `{"x":"ok"}`},
		{"nested object", `noise {"a":{"b":1},"c":"}"}` + " trailing", `{"a":{"b":1},"c":"}"}`},
		{"brace inside string is not a close", `text {"path":"a}b","x":1} end`, `{"path":"a}b","x":1}`},
		{"skips a prose brace before the real object", `use {x} format:` + "\n" + `{"x":"ok"}`, `{"x":"ok"}`},
		// No recoverable object: return the trimmed input so the caller reports the original error.
		{"no object at all", "just prose, no json", "just prose, no json"},
		{"unbalanced object", `{"x":`, `{"x":`},
		{"empty", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractAgentJSONObject(tc.in); got != tc.want {
				t.Fatalf("extractAgentJSONObject(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// completeAgentOutput must recover a schema-valid object from a completion that carries a prose
// preamble, instead of failing the run on the first non-JSON character (issue #510).
func TestCompleteAgentOutput_recoversPreambleAndFence(t *testing.T) {
	agent := &spec.AgentResource{
		Metadata: spec.Metadata{Name: "a"},
		Spec:     spec.AgentSpec{Output: &spec.AgentIO{Schema: "./out.json"}},
	}
	e := &Executor{PinnedGraph: true, Schemas: map[string]string{"./out.json": strictInputSchema}}
	step := spec.WorkflowStep{ID: "s1"}

	cases := []struct {
		name    string
		content string
	}{
		{"clean", `{"x":"ok"}`},
		{"preamble", "Perfect. Here is the state:\n{\"x\":\"ok\"}"},
		{"json fence", "```json\n{\"x\":\"ok\"}\n```"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := e.completeAgentOutput(context.Background(), policy.NewEvaluator(nil, nil), agent, step, tc.content, models.GenerateMeta{})
			if err != nil {
				t.Fatalf("completeAgentOutput(%q) unexpected error: %v", tc.content, err)
			}
			if out["x"] != "ok" {
				t.Fatalf("completeAgentOutput(%q) = %v, want x=ok", tc.content, out)
			}
		})
	}
}

// A preamble around output that does not satisfy the schema still fails, and the reported error is
// the schema violation on the recovered object — not a stray-character parse error.
func TestCompleteAgentOutput_recoveredButSchemaInvalid(t *testing.T) {
	agent := &spec.AgentResource{
		Metadata: spec.Metadata{Name: "a"},
		Spec:     spec.AgentSpec{Output: &spec.AgentIO{Schema: "./out.json"}},
	}
	e := &Executor{PinnedGraph: true, Schemas: map[string]string{"./out.json": strictInputSchema}}
	step := spec.WorkflowStep{ID: "s1"}
	if _, _, err := e.completeAgentOutput(context.Background(), policy.NewEvaluator(nil, nil), agent, step, "Here:\n{\"y\":1}", models.GenerateMeta{}); err == nil {
		t.Fatal("expected schema violation on recovered object")
	}
}
