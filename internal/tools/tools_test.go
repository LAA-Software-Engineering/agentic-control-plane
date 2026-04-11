package tools

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
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

func mockMCPBinaryFromTools(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "mockmcp")
	cmd := exec.Command("go", "build", "-o", out, "./mcp/testdata/mockmcp")
	cmd.Dir = "."
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mockmcp: %v\n%s", err, b)
	}
	return out
}

func testGraphMCP(bin string) *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"mc": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindTool,
				Metadata:   spec.Metadata{Name: "mc"},
				Spec: spec.ToolSpec{
					Type: "mcp",
					MCP:  &spec.ToolMCP{Transport: "stdio", Command: bin},
				},
			},
		},
	}
}

func TestRegistry_MCP_stdio_mockServer(t *testing.T) {
	bin := mockMCPBinaryFromTools(t)
	reg := NewRegistry(testGraphMCP(bin))
	resp, err := reg.Call(context.Background(), ToolCallRequest{
		Uses: "tool.mc.echo",
		With: map[string]any{"hello": "world"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Output["hello"] != "world" {
		t.Fatalf("output %+v", resp.Output)
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
