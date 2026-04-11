package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func testGraphNativeTool() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"demo": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "demo"},
				Spec:       spec.ToolSpec{Type: "native"},
			},
		},
	}
}

func TestRegistry_nativeEcho_succeeds(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry(testGraphNativeTool())
	resp, err := reg.Call(ctx, ToolCallRequest{
		Uses: "tool.demo.echo",
		With: map[string]any{"repo": "acme/api", "number": float64(7)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Output == nil {
		t.Fatal("nil output")
	}
	echo, ok := resp.Output["echo"].(map[string]any)
	if !ok {
		t.Fatalf("echo field: %#v", resp.Output["echo"])
	}
	if echo["repo"] != "acme/api" || echo["number"] != float64(7) {
		t.Fatalf("echo payload %+v", echo)
	}
	if resp.Meta.DurationMs < 0 {
		t.Fatalf("meta %+v", resp.Meta)
	}
}

func TestRegistry_unknownOperation_structuredError(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry(testGraphNativeTool())
	_, err := reg.Call(ctx, ToolCallRequest{
		Uses: "tool.demo.no_such_op",
		With: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	var u *UnknownOperationError
	if !errors.As(err, &u) {
		t.Fatalf("want *UnknownOperationError, got %T: %v", err, err)
	}
	if u.Tool != "demo" || u.Operation != "no_such_op" {
		t.Fatalf("got %+v", u)
	}
}

func TestParseUses_githubExample(t *testing.T) {
	tool, op, err := ParseUses("tool.github.pull_request.get")
	if err != nil {
		t.Fatal(err)
	}
	if tool != "github" || op != "pull_request.get" {
		t.Fatalf("%q %q", tool, op)
	}
}

func TestMockExecutor_isolated(t *testing.T) {
	ctx := context.Background()
	m := &MockExecutor{
		Resp: ToolCallResponse{
			Output: map[string]any{"x": float64(1)},
			Meta:   ToolCallMeta{DurationMs: 9, CostUSD: 0.01},
		},
	}
	resp, err := m.Call(ctx, ToolCallRequest{Uses: "tool.any.x", With: nil})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Output["x"] != float64(1) {
		t.Fatalf("%+v", resp.Output)
	}
}
