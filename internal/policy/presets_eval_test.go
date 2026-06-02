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

func TestCheckToolCall_shellSafe_requiredForLayering(t *testing.T) {
	g := testGraphWithTools("helper")
	g.Tools["helper"].Spec.Safety = &spec.ToolSafety{SideEffects: spec.BoolPtr(false)}
	base, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	pol := spec.MergePolicySpec(base, spec.PolicySpec{
		Approvals: &spec.PolicyApprovals{
			RequiredFor: []string{"tool.helper.echo"},
		},
	})
	ev := NewEvaluator(g, &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.helper.echo",
	})
	if err == nil {
		t.Fatal("shell_safe with local requiredFor should gate exact uses")
	}
}

func TestCheckToolCall_shellSafe_gatesChainedCommand(t *testing.T) {
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(shellSafeGraph(), &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.shell.run",
		With: map[string]any{"command": "ls; rm -rf /"},
	})
	if err == nil {
		t.Fatal("expected chained command to require approval")
	}
}

func TestCheckToolCall_shellSafe_approveGrantsRm(t *testing.T) {
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(shellSafeGraph(), &pol)
	uses := "tool.shell.command.run"
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Run:  RunContext{ApprovedActions: []string{uses}},
		Uses: uses,
		With: map[string]any{"command": "rm -rf /tmp/x"},
	})
	if err != nil {
		t.Fatalf("--approve should grant gated command: %v", err)
	}
}

func TestCheckToolCall_shellSafe_unknownTokenGated(t *testing.T) {
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(shellSafeGraph(), &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.shell.exec",
		With: map[string]any{"command": "totally-unknown"},
	})
	if err == nil {
		t.Fatal("expected unknown token to gate")
	}
}

func TestCheckToolCall_shellSafe_nonShellSideEffectToolGated(t *testing.T) {
	g := testGraphWithTools("slack")
	g.Tools["slack"].Spec.Safety = &spec.ToolSafety{SideEffects: spec.BoolPtr(true)}
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	ev := NewEvaluator(g, &pol)
	err = ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.slack.message.send",
	})
	if err == nil {
		t.Fatal("side-effecting non-shell tool should gate under shell_safe")
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

func TestEngine_Evaluator_resolvesBuiltinShellSafeAfterNormalize(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Defaults: &spec.ProjectDefaults{Policy: spec.PresetShellSafe},
		},
		Tools: shellSafeGraph().Tools,
	}
	spec.NormalizeProjectGraph(g)
	eng := NewEngine(g)
	ev := eng.Evaluator(spec.PresetShellSafe)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.shell.command.run",
		With: map[string]any{"command": "ls"},
	})
	if err != nil {
		t.Fatalf("builtin shell_safe after normalize should allow ls: %v", err)
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

func TestEffectiveToolDecision_shellSafe_builtinPresetNoApprovals(t *testing.T) {
	g := shellSafeGraph()
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	if pol.Approvals != nil {
		t.Fatal("builtin shell_safe should not set Approvals")
	}
	td := EffectiveToolDecision(g, &pol, "shell")
	if td.Decision != DecisionRequireApproval {
		t.Fatalf("plan should flag side-effecting shell tool via ResolvedPreset: %+v", td)
	}
}

func TestEffectiveToolDecision_shellSafe_toolGranularPlan(t *testing.T) {
	g := shellSafeGraph()
	pol, err := spec.BuildPreset(spec.PresetShellSafe)
	if err != nil {
		t.Fatal(err)
	}
	td := EffectiveToolDecision(g, &pol, "shell")
	if td.Decision != DecisionRequireApproval {
		t.Fatalf("plan should conservatively flag side-effecting shell tool: %+v", td)
	}
}
