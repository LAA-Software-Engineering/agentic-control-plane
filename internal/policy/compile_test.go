package policy

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func compileTestGraph() *spec.ProjectGraph {
	return &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Defaults: &spec.ProjectDefaults{Policy: "default"},
		},
		Policies: map[string]*spec.PolicyResource{
			"default": {
				Metadata: spec.Metadata{Name: "default"},
				Spec: spec.PolicySpec{
					Preset:         spec.PresetShellSafe,
					ResolvedPreset: spec.PresetShellSafe,
					Approvals: &spec.PolicyApprovals{
						RequiredFor: []string{"tool.helper.echo"},
					},
				},
			},
		},
		Tools: map[string]*spec.ToolResource{
			"helper": {
				Metadata: spec.Metadata{Name: "helper"},
				Spec: spec.ToolSpec{
					Type: "native",
					Safety: &spec.ToolSafety{
						Trusted:     spec.BoolPtr(true),
						SideEffects: spec.BoolPtr(false),
					},
				},
			},
			"shell": {
				Metadata: spec.Metadata{Name: "shell"},
				Spec: spec.ToolSpec{
					Type: "native",
					Safety: &spec.ToolSafety{
						SideEffects: spec.BoolPtr(true),
					},
				},
			},
			"reader": {
				Metadata: spec.Metadata{Name: "reader"},
				Spec: spec.ToolSpec{
					Type: "native",
					Safety: &spec.ToolSafety{
						Trusted:     spec.BoolPtr(true),
						SideEffects: spec.BoolPtr(false),
					},
				},
			},
		},
	}
}

func TestCompile_precedenceAndSources(t *testing.T) {
	g := compileTestGraph()
	cp, err := Compile(g, "default")
	if err != nil {
		t.Fatal(err)
	}
	if cp.PolicyName != "default" {
		t.Fatalf("policyName = %q", cp.PolicyName)
	}
	if cp.Digest == "" {
		t.Fatal("expected digest")
	}

	planCases := []struct {
		tool     string
		decision Decision
		source   DecisionSource
	}{
		{"helper", DecisionRequireApproval, SourceExplicitPolicyRule},
		{"shell", DecisionRequireApproval, SourceExplicitPolicyRule},
		{"reader", DecisionAllow, SourceSafetyMetadata},
	}
	for _, tc := range planCases {
		td, ok := cp.PlanTools[tc.tool]
		if !ok {
			t.Fatalf("missing plan tool %q", tc.tool)
		}
		if td.Decision != tc.decision {
			t.Fatalf("plan tool %q decision = %q, want %q", tc.tool, td.Decision, tc.decision)
		}
		if td.Source != tc.source {
			t.Fatalf("plan tool %q source = %q, want %q", tc.tool, td.Source, tc.source)
		}
	}
	runtimeCases := []struct {
		tool     string
		decision Decision
	}{
		{"helper", DecisionAllow},
		{"shell", DecisionRequireApproval},
		{"reader", DecisionAllow},
	}
	for _, tc := range runtimeCases {
		td, ok := cp.Tools[tc.tool]
		if !ok {
			t.Fatalf("missing runtime tool %q", tc.tool)
		}
		if td.Decision != tc.decision {
			t.Fatalf("runtime tool %q decision = %q, want %q", tc.tool, td.Decision, tc.decision)
		}
	}
	if !cp.Residual.ShellSafe {
		t.Fatal("expected shell_safe residual")
	}
	if len(cp.Residual.RequiredForExact) != 1 || cp.Residual.RequiredForExact[0] != "tool.helper.echo" {
		t.Fatalf("requiredForExact = %#v", cp.Residual.RequiredForExact)
	}
}

func TestCompile_digestStability(t *testing.T) {
	g := compileTestGraph()
	cp1, err := Compile(g, "default")
	if err != nil {
		t.Fatal(err)
	}
	cp2, err := Compile(g, "default")
	if err != nil {
		t.Fatal(err)
	}
	if cp1.Digest != cp2.Digest {
		t.Fatalf("digests differ: %s vs %s", cp1.Digest, cp2.Digest)
	}
}

func TestCompile_digestChangesOnInput(t *testing.T) {
	g := compileTestGraph()
	before, err := Compile(g, "default")
	if err != nil {
		t.Fatal(err)
	}
	g.Policies["default"].Spec.Approvals.RequiredFor = append(
		g.Policies["default"].Spec.Approvals.RequiredFor,
		"tool.reader.fetch",
	)
	after, err := Compile(g, "default")
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest == after.Digest {
		t.Fatal("digest should change when tool safety changes")
	}
}

func TestCompileReferenced_collectsDefaultPolicy(t *testing.T) {
	g := compileTestGraph()
	set, err := CompileReferenced(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(set) != 1 {
		t.Fatalf("policies = %d, want 1", len(set))
	}
	if set["default"] == nil {
		t.Fatal("missing default policy")
	}
	digest, err := SnapshotSetDigest(set)
	if err != nil {
		t.Fatal(err)
	}
	if digest == "" {
		t.Fatal("expected set digest")
	}
}

func TestCompiledEvaluator_matchesLegacyShellSafe(t *testing.T) {
	g := compileTestGraph()
	cp, err := Compile(g, "default")
	if err != nil {
		t.Fatal(err)
	}
	legacy := NewEvaluator(g, &g.Policies["default"].Spec)
	compiled := NewCompiledEvaluator(g, cp)

	cases := []struct {
		name string
		call ToolCallContext
	}{
		{
			name: "helper echo gated",
			call: ToolCallContext{Uses: "tool.helper.echo"},
		},
		{
			name: "reader allowed",
			call: ToolCallContext{Uses: "tool.reader.fetch"},
		},
		{
			name: "shell ls allowed",
			call: ToolCallContext{
				Uses: "tool.shell.command.run",
				With: map[string]any{"command": "ls -la"},
			},
		},
		{
			name: "shell rm gated",
			call: ToolCallContext{
				Uses: "tool.shell.command.run",
				With: map[string]any{"command": "rm -rf /tmp/x"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errLegacy := legacy.CheckToolCall(t.Context(), tc.call)
			errCompiled := compiled.CheckToolCall(t.Context(), tc.call)
			if (errLegacy == nil) != (errCompiled == nil) {
				t.Fatalf("legacy=%v compiled=%v", errLegacy, errCompiled)
			}
			if errLegacy != nil && errCompiled != nil {
				d1, ok1 := AsDenied(errLegacy)
				d2, ok2 := AsDenied(errCompiled)
				if !ok1 || !ok2 || d1.Reason != d2.Reason {
					t.Fatalf("reasons differ: legacy=%v compiled=%v", errLegacy, errCompiled)
				}
			}
		})
	}
}

func TestCompile_unknownPolicy(t *testing.T) {
	_, err := Compile(compileTestGraph(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
}
