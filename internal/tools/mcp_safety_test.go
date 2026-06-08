package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
	warnings := ApplyMCPSafetyDiscovery(context.Background(), g)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
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

func TestApplyMCPSafetyDiscovery_missingServer_warnsAndFailClosed(t *testing.T) {
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
	warnings := ApplyMCPSafetyDiscovery(context.Background(), g)
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %+v", warnings)
	}
	if warnings[0].Tool != "mc" {
		t.Fatalf("tool name: %+v", warnings[0])
	}
	if !strings.Contains(warnings[0].Message, "fail-closed") {
		t.Fatalf("message: %q", warnings[0].Message)
	}
	spec.NormalizeToolSafety(&g.Tools["mc"].Spec)

	s := g.Tools["mc"].Spec.Safety
	if s == nil || s.RequiresApproval == nil || !*s.RequiresApproval {
		t.Fatalf("fail-closed without discovery: %+v", s)
	}
}

func TestApplyMCPSafetyDiscovery_perToolTimeout(t *testing.T) {
	bin := buildMockMCP(t)
	g := &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"slow": {
				Metadata: spec.Metadata{Name: "slow"},
				Spec: spec.ToolSpec{
					Type: "mcp",
					MCP:  &spec.ToolMCP{Transport: "stdio", Command: bin},
				},
			},
			"bad": {
				Metadata: spec.Metadata{Name: "bad"},
				Spec: spec.ToolSpec{
					Type: "mcp",
					MCP:  &spec.ToolMCP{Transport: "stdio", Command: "/nonexistent/mcp-binary"},
				},
			},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	warnings := ApplyMCPSafetyDiscovery(ctx, g)
	if len(warnings) != 2 {
		t.Fatalf("expected warnings for both tools under expired ctx, got %+v", warnings)
	}
}

func TestApplyMCPSafetyDiscovery_nilGraph(t *testing.T) {
	if warnings := ApplyMCPSafetyDiscovery(context.Background(), nil); warnings != nil {
		t.Fatalf("expected nil warnings, got %+v", warnings)
	}
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
