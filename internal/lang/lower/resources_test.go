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

// TestInlinePolicy_YAMLEquivalence mirrors the tool golden on the security-relevant policy
// projection (permit / permitWithApproval / requiredFor / execution): an inline policy and its YAML
// twin normalize to byte-identical spec JSON, so a future change to policy lowering or normalization
// cannot silently diverge on the approval/effect surface.
func TestInlinePolicy_YAMLEquivalence(t *testing.T) {
	agentSrc := `policy coding {
    execution { maxTotalCostUsd 5  maxWallClockSeconds 300 }
    approvals { requiredFor { tool.workspace.run_tests } }
    effects {
        permit             { workspace.read }
        permitWithApproval { workspace.write }
    }
}`
	yamlSrc := `apiVersion: agentic.dev/v0
kind: Policy
metadata: {name: coding}
spec:
  execution: {maxTotalCostUsd: 5, maxWallClockSeconds: 300}
  approvals: {requiredFor: [tool.workspace.run_tests]}
  effects:
    permit: [workspace.read]
    permitWithApproval: [workspace.write]
`
	inline := lowerToolsOrFatal(t, agentSrc).Policies[0]

	dec, err := spec.ParseResourceFromBytes([]byte(yamlSrc), "coding.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromYAML := dec.Resource.(*spec.PolicyResource)

	if normPolicySpecJSON(t, inline) != normPolicySpecJSON(t, fromYAML) {
		t.Fatalf("inline policy spec differs from YAML twin:\n inline: %s\n yaml:   %s", normPolicySpecJSON(t, inline), normPolicySpecJSON(t, fromYAML))
	}
}

// TestInlinePolicy_HitlYAMLEquivalence is the ADR 005 §2 golden for the .agent hitl block (issues
// #106, #440): an inline policy with a rich hitl config and its YAML twin normalize to byte-identical
// spec JSON, so the two front ends can never diverge on human-in-the-loop review config.
func TestInlinePolicy_HitlYAMLEquivalence(t *testing.T) {
	agentSrc := `policy gated-publish {
    approvals { requiredFor { tool.publish.default } }
    hitl {
        descriptionPrefix "Publishing requires operator approval"
        redactKeys { "token" "password" }
        toolSwitchMap {
            deploy_to_production { missing_operation staging }
        }
        interruptOn {
            deploy
            publish {
                allowedDecisions { approve reject edit }
                description "Review publish (${uses})"
                allowedEditArgs { "topic" }
                deniedEditArgs { "secret" }
                switchMap {
                    a.b { c.d }
                }
                redactKeys { "apiKey" }
            }
        }
    }
}`
	yamlSrc := `apiVersion: agentic.dev/v0
kind: Policy
metadata: {name: gated-publish}
spec:
  approvals: {requiredFor: [tool.publish.default]}
  hitl:
    descriptionPrefix: Publishing requires operator approval
    redactKeys: [token, password]
    toolSwitchMap:
      deploy_to_production: [missing_operation, staging]
    interruptOn:
      deploy: true
      publish:
        allowedDecisions: [approve, reject, edit]
        description: "Review publish (${uses})"
        allowedEditArgs: [topic]
        deniedEditArgs: [secret]
        switchMap:
          a.b: [c.d]
        redactKeys: [apiKey]
`
	inline := lowerToolsOrFatal(t, agentSrc).Policies[0]

	dec, err := spec.ParseResourceFromBytes([]byte(yamlSrc), "gated-publish.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromYAML := dec.Resource.(*spec.PolicyResource)

	if normPolicySpecJSON(t, inline) != normPolicySpecJSON(t, fromYAML) {
		t.Fatalf("inline hitl policy spec differs from YAML twin:\n inline: %s\n yaml:   %s", normPolicySpecJSON(t, inline), normPolicySpecJSON(t, fromYAML))
	}
}

func normPolicySpecJSON(t *testing.T, pr *spec.PolicyResource) string {
	t.Helper()
	g := &spec.ProjectGraph{Policies: map[string]*spec.PolicyResource{pr.Metadata.Name: pr}}
	spec.NormalizeProjectGraph(g)
	b, err := json.Marshal(g.Policies[pr.Metadata.Name].Spec)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestInlinePolicy_Preset covers the .agent `preset` field (issue #430): a policy may select a
// built-in preset, which lowers to spec.PolicySpec.Preset and resolves through normalization exactly
// like the YAML spec.preset — so a .agent-only project can declare a shell_safe default policy.
func TestInlinePolicy_Preset(t *testing.T) {
	res := lowerToolsOrFatal(t, `policy default {
    preset shell_safe
}`)
	if len(res.Policies) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(res.Policies))
	}
	if got := res.Policies[0].Spec.Preset; got != spec.PresetShellSafe {
		t.Fatalf("lowered preset = %q, want %q", got, spec.PresetShellSafe)
	}

	// End to end: after normalization the preset resolves to the built-in shell_safe policy.
	g := &spec.ProjectGraph{
		Meta:     spec.Metadata{Name: "p"},
		Policies: map[string]*spec.PolicyResource{"default": res.Policies[0]},
	}
	spec.NormalizeProjectGraph(g)
	if rp := g.Policies["default"].Spec.ResolvedPreset; rp != spec.PresetShellSafe {
		t.Fatalf("resolved preset = %q, want %q", rp, spec.PresetShellSafe)
	}
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

// TestInlineTool_MCPYAMLEquivalence is the ADR 005 §2 golden for the .agent mcp transport block
// (issue #440): an inline mcp tool and its YAML twin normalize to byte-identical spec JSON, so the
// two front ends can never diverge on transport config.
func TestInlineTool_MCPYAMLEquivalence(t *testing.T) {
	agentSrc := `tool github {
    type mcp
    mcp {
        transport "stdio"
        command "npx"
        args { "-y" "@modelcontextprotocol/server-github" }
        headers { "Authorization" "env:GITHUB_TOKEN" }
    }
}`
	yamlSrc := `apiVersion: agentic.dev/v0
kind: Tool
metadata: {name: github}
spec:
  type: mcp
  mcp:
    transport: stdio
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    headers: {Authorization: "env:GITHUB_TOKEN"}
`
	inline := lowerToolsOrFatal(t, agentSrc).Tools[0]
	dec, err := spec.ParseResourceFromBytes([]byte(yamlSrc), "github.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromYAML := dec.Resource.(*spec.ToolResource)
	if normSpecJSON(t, inline) != normSpecJSON(t, fromYAML) {
		t.Fatalf("inline mcp tool differs from YAML twin:\n inline: %s\n yaml:   %s", normSpecJSON(t, inline), normSpecJSON(t, fromYAML))
	}
}

// TestInlineTool_HTTPYAMLEquivalence is the ADR 005 §2 golden for the .agent http transport block.
func TestInlineTool_HTTPYAMLEquivalence(t *testing.T) {
	agentSrc := `tool webhook {
    type http
    http {
        baseUrl "https://api.example.com"
        headers { "Authorization" "env:API_TOKEN" }
    }
}`
	yamlSrc := `apiVersion: agentic.dev/v0
kind: Tool
metadata: {name: webhook}
spec:
  type: http
  http:
    baseUrl: "https://api.example.com"
    headers: {Authorization: "env:API_TOKEN"}
`
	inline := lowerToolsOrFatal(t, agentSrc).Tools[0]
	dec, err := spec.ParseResourceFromBytes([]byte(yamlSrc), "webhook.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromYAML := dec.Resource.(*spec.ToolResource)
	if normSpecJSON(t, inline) != normSpecJSON(t, fromYAML) {
		t.Fatalf("inline http tool differs from YAML twin:\n inline: %s\n yaml:   %s", normSpecJSON(t, inline), normSpecJSON(t, fromYAML))
	}
}

// TestInlineEnvironment_YAMLEquivalence is the ADR 005 §2 golden for the .agent environment overlay
// block (issue #440): an inline environment and its YAML twin lower to byte-identical spec JSON.
func TestInlineEnvironment_YAMLEquivalence(t *testing.T) {
	agentSrc := `environment prod {
    overrides {
        agents {
            reviewer {
                model anthropic/claude-sonnet-5
                constraints { timeoutSeconds 300 }
            }
        }
        policies {
            guarded-writes {
                execution { maxTotalCostUsd 10 }
                approvals { requiredFor { tool.workspace.run_tests } }
            }
        }
    }
}`
	yamlSrc := `apiVersion: agentic.dev/v0
kind: Environment
metadata: {name: prod}
spec:
  overrides:
    agents:
      reviewer:
        model: "anthropic/claude-sonnet-5"
        constraints: {timeoutSeconds: 300}
    policies:
      guarded-writes:
        execution: {maxTotalCostUsd: 10}
        approvals: {requiredFor: [tool.workspace.run_tests]}
`
	res := lowerToolsOrFatal(t, agentSrc)
	if len(res.Environments) != 1 {
		t.Fatalf("expected 1 environment, got %d", len(res.Environments))
	}
	inline := res.Environments[0]

	dec, err := spec.ParseResourceFromBytes([]byte(yamlSrc), "prod.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromYAML := dec.Resource.(*spec.EnvironmentResource)

	inJSON, err := json.Marshal(inline.Spec)
	if err != nil {
		t.Fatal(err)
	}
	yJSON, err := json.Marshal(fromYAML.Spec)
	if err != nil {
		t.Fatal(err)
	}
	if string(inJSON) != string(yJSON) {
		t.Fatalf("inline environment differs from YAML twin:\n inline: %s\n yaml:   %s", inJSON, yJSON)
	}
}
