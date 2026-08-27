package policy

import (
	"context"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

func toolWithOperations(name string, ops ...string) *spec.ToolResource {
	m := make(map[string]spec.ToolOperation, len(ops))
	for _, op := range ops {
		// A declared effect makes the tool closed-world; the effect value is not asserted here.
		m[op] = spec.ToolOperation{Effects: []string{"example.effect"}}
	}
	return &spec.ToolResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindTool,
		Metadata:   spec.Metadata{Name: name},
		Spec:       spec.ToolSpec{Type: "mock", Operations: m},
	}
}

func TestCheckToolCall_manifest_operationOutsideManifestDenied(t *testing.T) {
	g := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"github": toolWithOperations("github", "read_pr"),
	}}
	// No policy at all: the closed world is a hard authority boundary, not an approval gate.
	ev := NewEvaluator(g, nil)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{
		Uses: "tool.github.delete_repo",
	})
	d, ok := AsDenied(err)
	if !ok {
		t.Fatalf("expected denial, got %v", err)
	}
	if d.Reason != ReasonOperationNotInManifest {
		t.Fatalf("reason = %q, want %q", d.Reason, ReasonOperationNotInManifest)
	}
	if d.Extra["operation"] != "delete_repo" || d.Extra["tool"] != "github" {
		t.Fatalf("trace data = %v", d.Extra)
	}
}

func TestCheckToolCall_manifest_declaredOperationAllowed(t *testing.T) {
	g := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"github": toolWithOperations("github", "read_pr"),
	}}
	g.Tools["github"].Spec.Safety = &spec.ToolSafety{
		Trusted:          spec.BoolPtr(true),
		SideEffects:      spec.BoolPtr(false),
		RequiresApproval: spec.BoolPtr(false),
	}
	ev := NewEvaluator(g, nil)
	if err := ev.CheckToolCall(context.Background(), ToolCallContext{Uses: "tool.github.read_pr"}); err != nil {
		t.Fatalf("declared operation must be permitted by the manifest: %v", err)
	}
}

func TestCheckToolCall_manifest_openToolNotEnforced(t *testing.T) {
	// A tool that declares no operations has an open callable set: closed-world enforcement is
	// opt-in, so existing MCP/HTTP examples keep dispatching any operation.
	g := testGraphWithTools("slack")
	g.Tools["slack"].Spec.Safety = &spec.ToolSafety{
		Trusted:          spec.BoolPtr(true),
		SideEffects:      spec.BoolPtr(false),
		RequiresApproval: spec.BoolPtr(false),
	}
	ev := NewEvaluator(g, nil)
	if err := ev.CheckToolCall(context.Background(), ToolCallContext{Uses: "tool.slack.message.send"}); err != nil {
		t.Fatalf("open tool must not be closed-world enforced: %v", err)
	}
}

// Production run builds a compiledEvaluator (NewCompiledEvaluator), not *evaluator. The closed
// world must bind there too, before any residual short-circuit. Without the compiled-path check a
// tool-level DecisionAllow / permissive residual would treat every operation on the tool as
// callable — the exact failure #204 exists to prevent.
func compiledManifestGraph(permissive bool) *spec.ProjectGraph {
	pol := spec.PolicySpec{}
	if permissive {
		pol.Approvals = &spec.PolicyApprovals{Permissive: spec.BoolPtr(true)}
	}
	return &spec.ProjectGraph{
		Spec: spec.ProjectSpec{Defaults: &spec.ProjectDefaults{Policy: "default"}},
		Policies: map[string]*spec.PolicyResource{
			"default": {Metadata: spec.Metadata{Name: "default"}, Spec: pol},
		},
		Tools: map[string]*spec.ToolResource{
			"github": {
				Metadata: spec.Metadata{Name: "github"},
				Spec: spec.ToolSpec{
					Type:               "mock",
					OperationsDeclared: true,
					Operations:         map[string]spec.ToolOperation{"read_pr": {Effects: []string{"github.read"}}},
					Safety: &spec.ToolSafety{
						Trusted:          spec.BoolPtr(true),
						SideEffects:      spec.BoolPtr(false),
						RequiresApproval: spec.BoolPtr(false),
					},
				},
			},
		},
	}
}

func TestCompiledEvaluator_manifest_operationOutsideManifestDenied(t *testing.T) {
	for _, permissive := range []bool{false, true} {
		g := compiledManifestGraph(permissive)
		cp, err := Compile(g, "default")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		ev := NewCompiledEvaluator(g, cp)
		// A declared operation is permitted by the compiled snapshot.
		if err := ev.CheckToolCall(context.Background(), ToolCallContext{Uses: "tool.github.read_pr"}); err != nil {
			t.Fatalf("permissive=%v: declared operation denied: %v", permissive, err)
		}
		// An operation outside the manifest is denied even under a permissive/allow residual.
		err = ev.CheckToolCall(context.Background(), ToolCallContext{Uses: "tool.github.delete_repo"})
		d, ok := AsDenied(err)
		if !ok || d.Reason != ReasonOperationNotInManifest {
			t.Fatalf("permissive=%v: compiled path must enforce the closed world, got %v", permissive, err)
		}
	}
}

func TestCheckToolCall_manifest_permissivePolicyStillClosedWorld(t *testing.T) {
	// A permissive policy relaxes approvals; it must not relax the closed world.
	g := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"github": toolWithOperations("github", "read_pr"),
	}}
	pol := &spec.PolicySpec{Approvals: &spec.PolicyApprovals{Permissive: spec.BoolPtr(true)}}
	ev := NewEvaluator(g, pol)
	err := ev.CheckToolCall(context.Background(), ToolCallContext{Uses: "tool.github.delete_repo"})
	if d, ok := AsDenied(err); !ok || d.Reason != ReasonOperationNotInManifest {
		t.Fatalf("permissive policy must not widen the closed world: %v", err)
	}
}
