package policy

import (
	"context"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func shellSafeGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"shell": {
				Metadata: spec.Metadata{Name: "shell"},
				Spec: spec.ToolSpec{
					Type: "native",
					Safety: &spec.ToolSafety{
						SideEffects: spec.BoolPtr(true),
					},
				},
			},
		},
	}
}

func TestCheckToolCall_shellSafe_allowsLs(t *testing.T) {
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(shellSafeGraph(), &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.shell.command.run",
		With: map[string]any{"command": "ls -la"},
	})
	if err != nil {
		t.Fatalf("ls should be allowed: %v", err)
	}
}

func TestCheckToolCall_shellSafe_gatesRm(t *testing.T) {
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(shellSafeGraph(), &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.shell.command.run",
		With: map[string]any{"command": "rm -rf /tmp/x"},
	})
	if err == nil {
		t.Fatal("expected rm to require approval")
	}
	d, ok := AsDenied(err)
	if !ok || d.Reason != ReasonApprovalRequired {
		t.Fatalf("got %v", err)
	}
}

func TestCheckToolCall_shellSafe_unknownTokenGated(t *testing.T) {
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(shellSafeGraph(), &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.shell.command.run",
		With: map[string]any{"command": "totally-unknown"},
	})
	if err == nil {
		t.Fatal("expected unknown token to gate")
	}
}

func TestCheckToolCall_strict_gatesAllTools(t *testing.T) {
	g := testGraphWithTools("helper")
	g.Tools["helper"].Spec.Safety = &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}
	pol, err := spec.BuildPreset(spec.PresetStrict)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(g, &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.helper.echo",
	})
	if err == nil {
		t.Fatal("strict should gate all tools")
	}
}

func TestCheckToolCall_permissive_allowsMutatingTool(t *testing.T) {
	g := testGraphWithTools("slack")
	g.Tools["slack"].Spec.Safety = &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}
	pol, err := spec.BuildPreset(spec.PresetPermissive)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(g, &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.slack.message.send",
	})
	if err != nil {
		t.Fatalf("permissive should allow: %v", err)
	}
}

func TestExpandPresetsInGraph_materializesDefault(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Defaults: &spec.ProjectDefaults{Policy: spec.PresetShellSafe},
		},
	}
	spec.ExpandPresetsInGraph(g)
	pr, ok := g.Policies[spec.PresetShellSafe]
	if !ok || pr == nil {
		t.Fatal("expected injected shell_safe policy")
	}
	if pr.Spec.ResolvedPreset != spec.PresetShellSafe {
		t.Fatalf("ResolvedPreset = %q", pr.Spec.ResolvedPreset)
	}
}

func TestExpandPresetsInGraph_userPolicyOverridesBuiltin(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Defaults: &spec.ProjectDefaults{Policy: spec.PresetStrict},
		},
		Policies: map[string]*spec.PolicyResource{
			spec.PresetStrict: {
				Metadata: spec.Metadata{Name: spec.PresetStrict},
				Spec: spec.PolicySpec{
					Execution: &spec.PolicyExecution{MaxWallClockSeconds: 99},
				},
			},
		},
	}
	spec.ExpandPresetsInGraph(g)
	if g.Policies[spec.PresetStrict].Spec.Execution.MaxWallClockSeconds != 99 {
		t.Fatal("user policy should not be replaced by builtin")
	}
}
