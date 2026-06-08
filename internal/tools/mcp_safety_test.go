package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestApplyMCPSafetyDiscovery_stdio_inheritsMeta(t *testing.T) {
	bin := buildMockMCP(t)
	g := &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"mc": {
				Metadata: spec.Metadata{Name: "mc"},
				Spec: spec.ToolSpec{
					Type: "mcp",
					MCP:  &spec.ToolMCP{Transport: "stdio", Command: bin},
				},
			},
		},
	}
	ApplyMCPSafetyDiscovery(context.Background(), g)
	spec.NormalizeToolSafety(&g.Tools["mc"].Spec)

	s := g.Tools["mc"].Spec.Safety
	if s == nil || s.RequiresApproval == nil || !*s.RequiresApproval {
		t.Fatalf("conservative MCP merge requires approval: %+v", s)
	}
	if s.Trusted == nil || *s.Trusted {
		t.Fatalf("expected untrusted after merge: %+v", s)
	}
}

func TestApplyMCPSafetyDiscovery_authorOverridesMCP(t *testing.T) {
	bin := buildMockMCP(t)
	tr := true
	g := &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"mc": {
				Metadata: spec.Metadata{Name: "mc"},
				Spec: spec.ToolSpec{
					Type: "mcp",
					MCP:  &spec.ToolMCP{Transport: "stdio", Command: bin},
					Safety: &spec.ToolSafety{
						Trusted: &tr,
					},
				},
			},
		},
	}
	ApplyMCPSafetyDiscovery(context.Background(), g)
	spec.NormalizeToolSafety(&g.Tools["mc"].Spec)

	s := g.Tools["mc"].Spec.Safety
	if s == nil || s.Trusted == nil || !*s.Trusted {
		t.Fatalf("author trusted should win: %+v", s)
	}
}

func TestApplyMCPSafetyDiscovery_missingServer_failClosed(t *testing.T) {
	g := &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"mc": {
				Metadata: spec.Metadata{Name: "mc"},
				Spec: spec.ToolSpec{
					Type: "mcp",
					MCP:  &spec.ToolMCP{Transport: "stdio", Command: "/nonexistent/mcp-binary"},
				},
			},
		},
	}
	ApplyMCPSafetyDiscovery(context.Background(), g)
	spec.NormalizeToolSafety(&g.Tools["mc"].Spec)

	s := g.Tools["mc"].Spec.Safety
	if s == nil || s.RequiresApproval == nil || !*s.RequiresApproval {
		t.Fatalf("fail-closed without discovery: %+v", s)
	}
}

func TestApplyMCPSafetyDiscovery_nilGraph(t *testing.T) {
	ApplyMCPSafetyDiscovery(context.Background(), nil)
}

func buildMockMCP(t *testing.T) string {
	t.Helper()
	name := "mockmcp"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", out, "./mcp/testdata/mockmcp")
	cmd.Dir = "."
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build mockmcp: %v\n%s", err, b)
	}
	return out
}
