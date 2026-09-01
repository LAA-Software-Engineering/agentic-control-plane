package lower

import (
	"encoding/json"
	"testing"

	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/spec"
)

func lowerToolsOrFatal(t *testing.T, src string) *Result {
	t.Helper()
	f, diags := lang.Parse("t.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse errors: %v", diags)
	}
	res, ld := LowerFile(f, Options{})
	if ld.HasErrors() {
		t.Fatalf("lower diagnostics: %v", ld)
	}
	return res
}

// TestInlineTool_OperationsDeclaredPresence is the soundness invariant (ADR 005 §2): an explicit
// `operations {}` is a closed, deny-all manifest (OperationsDeclared=true); an omitted block is open.
func TestInlineTool_OperationsDeclaredPresence(t *testing.T) {
	closed := lowerToolsOrFatal(t, "tool a {\n type native\n operations {}\n}")
	if len(closed.Tools) != 1 || !closed.Tools[0].Spec.OperationsDeclared {
		t.Fatalf("empty operations {} must set OperationsDeclared=true: %+v", closed.Tools)
	}
	if len(closed.Tools[0].Spec.Operations) != 0 {
		t.Fatalf("empty operations {} must have no operations, got %v", closed.Tools[0].Spec.Operations)
	}

	open := lowerToolsOrFatal(t, "tool b {\n type native\n}")
	if len(open.Tools) != 1 || open.Tools[0].Spec.OperationsDeclared {
		t.Fatalf("omitted operations block must leave OperationsDeclared=false: %+v", open.Tools)
	}
}

// TestInlineTool_YAMLEquivalence is the required equivalence golden (ADR 005 §2): an inline tool and
// its YAML twin normalize to byte-identical spec JSON, including the OperationsDeclared bit — so the
// two front ends can never diverge on the closed-world security boundary.
func TestInlineTool_YAMLEquivalence(t *testing.T) {
	agentSrc := `tool workspace {
    type native
    safety { trusted true }
    operations {
        read_file  { effects { workspace.read } }
        write_file { effects { workspace.write } }
    }
}`
	yamlSrc := `apiVersion: agentic.dev/v0
kind: Tool
metadata: {name: workspace}
spec:
  type: native
  safety: {trusted: true}
  operations:
    read_file: {effects: [workspace.read]}
    write_file: {effects: [workspace.write]}
`
	inline := lowerToolsOrFatal(t, agentSrc).Tools[0]

	dec, err := spec.ParseResourceFromBytes([]byte(yamlSrc), "workspace.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromYAML := dec.Resource.(*spec.ToolResource)

	if inline.Spec.OperationsDeclared != fromYAML.Spec.OperationsDeclared || !inline.Spec.OperationsDeclared {
		t.Fatalf("OperationsDeclared mismatch: inline=%v yaml=%v", inline.Spec.OperationsDeclared, fromYAML.Spec.OperationsDeclared)
	}
	if normSpecJSON(t, inline) != normSpecJSON(t, fromYAML) {
		t.Fatalf("inline tool spec differs from YAML twin:\n inline: %s\n yaml:   %s", normSpecJSON(t, inline), normSpecJSON(t, fromYAML))
	}
}

// normSpecJSON normalizes a single-tool graph and returns the tool's marshaled spec.
func normSpecJSON(t *testing.T, tr *spec.ToolResource) string {
	t.Helper()
	g := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{tr.Metadata.Name: tr}}
	spec.NormalizeProjectGraph(g)
	b, err := json.Marshal(g.Tools[tr.Metadata.Name].Spec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInlinePolicy_Lowering(t *testing.T) {
	res := lowerToolsOrFatal(t, `policy p {
    execution { maxTotalCostUsd 5 maxWallClockSeconds 300 }
    approvals { requiredFor { tool.git.push_branch } }
    effects { permit { workspace.read } permitWithApproval { workspace.write } }
}`)
	if len(res.Policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(res.Policies))
	}
	p := res.Policies[0].Spec
	if p.Execution == nil || p.Execution.MaxTotalCostUsd != 5 || p.Execution.MaxWallClockSeconds != 300 {
		t.Fatalf("execution: %+v", p.Execution)
	}
	if p.Approvals == nil || len(p.Approvals.RequiredFor) != 1 || p.Approvals.RequiredFor[0] != "tool.git.push_branch" {
		t.Fatalf("approvals: %+v", p.Approvals)
	}
	if p.Effects == nil || len(p.Effects.Permit) != 1 || len(p.Effects.PermitWithApproval) != 1 ||
		p.Effects.Permit[0] != "workspace.read" || p.Effects.PermitWithApproval[0] != "workspace.write" {
		t.Fatalf("effects: %+v", p.Effects)
	}
}

// TestMergeLowered_ToolPolicyCollision is the cross-ingress collision rule (ADR 005 §3): a lowered
// inline Tool/Policy whose name already exists (e.g. an imported YAML resource) is a load error.
func TestMergeLowered_ToolPolicyCollision(t *testing.T) {
	g := &spec.ProjectGraph{
		Tools:    map[string]*spec.ToolResource{"github": {Metadata: spec.Metadata{Name: "github"}}},
		Policies: map[string]*spec.PolicyResource{"default": {Metadata: spec.Metadata{Name: "default"}}},
	}
	res := &Result{
		Tools:    []*spec.ToolResource{{Metadata: spec.Metadata{Name: "github"}}},
		Policies: []*spec.PolicyResource{{Metadata: spec.Metadata{Name: "default"}}},
	}
	err := MergeLowered(g, res)
	if err == nil {
		t.Fatal("a duplicate Tool/Policy across ingress must be an error")
	}
	for _, want := range []string{"Tool", "github", "Policy", "default"} {
		if !containsStr(err.Error(), want) {
			t.Fatalf("collision error should name %q: %v", want, err)
		}
	}
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
