package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encoding/json"

	"github.com/Terfyn/terfyn/internal/project"
	"github.com/Terfyn/terfyn/internal/spec"
)

// runMigrate executes `terfyn migrate ...`, returning combined stdout, stderr, and the error.
func runMigrate(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errb.String(), err
}

// writeYAMLProject writes a hybrid YAML project: project.yaml (with a redundant built-in mock
// provider) importing a tool and a policy.
func writeYAMLProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("project.yaml", `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  imports:
    - ./tools/lookup.yaml
    - ./policies/guarded.yaml
  providers:
    models:
      mock:
        type: mock
`)
	write("tools/lookup.yaml", `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: lookup
spec:
  type: mock
  safety:
    trusted: true
    sideEffects: false
`)
	write("policies/guarded.yaml", `apiVersion: agentic.dev/v0
kind: Policy
metadata:
  name: guarded
spec:
  execution:
    maxTotalCostUsd: 5
  approvals:
    requiredFor:
      - tool.lookup.default
`)
	return root
}

// TestMigrate_raisesDeclarativesAndDropsBuiltin: migrating a hybrid YAML project prints .agent with
// the tool and policy raised and the redundant built-in `mock` provider dropped, and the output
// re-loads to a project with the same resources (issue #440).
func TestMigrate_raisesDeclarativesAndDropsBuiltin(t *testing.T) {
	root := writeYAMLProject(t)
	out, errOut, err := runMigrate(t, "migrate", "--to-agent", "--project", root)
	if err != nil {
		t.Fatalf("migrate failed: %v\nstderr:\n%s", err, errOut)
	}
	for _, want := range []string{"tool lookup {", "policy guarded {", "requiredFor {", "tool.lookup.default"} {
		if !strings.Contains(out, want) {
			t.Fatalf("migrated .agent missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "provider mock") {
		t.Fatalf("redundant built-in provider mock should be dropped:\n%s", out)
	}

	// The migrated source loads as a .agent-only project with the same declarative resources.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.agent"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := project.LoadProject(dir)
	if err != nil {
		t.Fatalf("migrated .agent did not load: %v\n%s", err, out)
	}
	if _, ok := g.Tools["lookup"]; !ok {
		t.Fatalf("tool lookup missing from reloaded graph")
	}
	if _, ok := g.Policies["guarded"]; !ok {
		t.Fatalf("policy guarded missing from reloaded graph")
	}
}

// TestMigrate_writesOutputFile: --output writes the .agent file (and refuses to clobber without --force).
func TestMigrate_writesOutputFile(t *testing.T) {
	root := writeYAMLProject(t)
	target := filepath.Join(t.TempDir(), "main.agent")
	if _, _, err := runMigrate(t, "migrate", "--to-agent", "-o", target, "--project", root); err != nil {
		t.Fatalf("migrate -o failed: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("output not written: %v", err)
	}
	if !strings.Contains(string(data), "tool lookup {") {
		t.Fatalf("output file missing raised tool:\n%s", data)
	}
	// A second run without --force must refuse to overwrite.
	if _, _, err := runMigrate(t, "migrate", "--to-agent", "-o", target, "--project", root); err == nil {
		t.Fatal("migrate should refuse to overwrite an existing --output without --force")
	}
	// With --force it succeeds.
	if _, _, err := runMigrate(t, "migrate", "--to-agent", "-o", target, "--force", "--project", root); err != nil {
		t.Fatalf("migrate --force overwrite failed: %v", err)
	}
}

// TestMigrate_refusesYAMLWorkflow: a YAML-authored workflow has no lossless .agent form; migrate
// reports it and exits non-zero (unless --force).
func TestMigrate_refusesYAMLWorkflow(t *testing.T) {
	root := writeYAMLProject(t)
	// Add a YAML workflow.
	wf := `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: flow
spec:
  steps:
    - id: s
      uses: tool.lookup.default
`
	if err := os.WriteFile(filepath.Join(root, "flow.yaml"), []byte(wf), 0o644); err != nil {
		t.Fatal(err)
	}
	// Import it.
	proj := filepath.Join(root, "project.yaml")
	b, _ := os.ReadFile(proj)
	updated := strings.Replace(string(b), "    - ./policies/guarded.yaml\n", "    - ./policies/guarded.yaml\n    - ./flow.yaml\n", 1)
	if err := os.WriteFile(proj, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := runMigrate(t, "migrate", "--to-agent", "--project", root)
	if err == nil {
		t.Fatal("migrate should refuse when a YAML workflow cannot be raised")
	}
	if !strings.Contains(errOut, "Workflow") || !strings.Contains(errOut, "flow") {
		t.Fatalf("expected a Workflow migration warning naming 'flow', got:\n%s", errOut)
	}
	// --force writes the raiseable rest despite the workflow.
	out, _, ferr := runMigrate(t, "migrate", "--to-agent", "--force", "--project", root)
	if ferr != nil {
		t.Fatalf("migrate --force failed: %v", ferr)
	}
	if !strings.Contains(out, "tool lookup {") {
		t.Fatalf("--force should still emit the raiseable declaratives:\n%s", out)
	}
}

// TestMigrate_requiresDirection: without --to-agent the command errors (only that direction exists).
func TestMigrate_requiresDirection(t *testing.T) {
	root := writeYAMLProject(t)
	if _, _, err := runMigrate(t, "migrate", "--project", root); err == nil {
		t.Fatal("migrate without --to-agent should error")
	}
}

// writeLegacyYAMLProject writes a YAML project whose resources carry the four fields removed from the
// canonical model in ADR 007 step 1 (tool.permissions, policy.security, agent.memory, agent.runtime).
func writeLegacyYAMLProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("project.yaml", `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: legacy
spec:
  imports:
    - ./tool.yaml
    - ./policy.yaml
    - ./agent.yaml
`)
	write("tool.yaml", `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: helper
spec:
  type: mock
  permissions:
    allow:
      - contents.read
  safety:
    trusted: true
`)
	write("policy.yaml", `apiVersion: agentic.dev/v0
kind: Policy
metadata:
  name: guarded
spec:
  execution:
    maxTotalCostUsd: 5
  security:
    networkAccess: restricted
    secretAccess: deny-by-default
`)
	write("agent.yaml", `apiVersion: agentic.dev/v0
kind: Agent
metadata:
  name: assistant
spec:
  model: mock/default
  policy: guarded
  runtime: local
  memory:
    type: session
    maxMessages: 20
`)
	return root
}

// TestMigrate_legacyRemovedFields_warnAndOmit: migrating a legacy YAML project that carries the four
// removed fields accepts it, warns once per field per resource, omits them from the generated .agent,
// and the output still re-loads (ADR 007 step 1 legacy-compat).
func TestMigrate_legacyRemovedFields_warnAndOmit(t *testing.T) {
	root := writeLegacyYAMLProject(t)
	out, errOut, err := runMigrate(t, "migrate", "--to-agent", "--project", root)
	if err != nil {
		t.Fatalf("migrate failed: %v\nstderr:\n%s", err, errOut)
	}
	// One warning per removed field per resource.
	for _, want := range []string{
		"Tool/helper: spec.permissions is deprecated",
		"Policy/guarded: spec.security is no longer part of the canonical model",
		"Agent/assistant: spec.memory is no longer part of the canonical model",
		"Agent/assistant: spec.runtime is no longer part of the canonical model",
	} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("missing legacy warning %q in:\n%s", want, errOut)
		}
	}
	// The removed fields must not appear in the generated .agent.
	for _, banned := range []string{"permissions", "networkAccess", "secretAccess", "memory", "maxMessages", "runtime"} {
		if strings.Contains(out, banned) {
			t.Fatalf("generated .agent must not contain removed field %q:\n%s", banned, out)
		}
	}
	// The kept resources are present and the output re-loads.
	for _, want := range []string{"tool helper {", "policy guarded {", "agent assistant {"} {
		if !strings.Contains(out, want) {
			t.Fatalf("generated .agent missing %q:\n%s", want, out)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.agent"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := project.LoadProject(dir); err != nil {
		t.Fatalf("migrated .agent did not re-load: %v\n%s", err, out)
	}
}

// writeComprehensiveYAMLProject writes a YAML project exercising the full now-supported declarative
// resource model (custom provider; mcp/http/native tools with retry, per-op schema, workspace, limits;
// a policy with execution/approvals/effects/hitl/tools.forbidUnknownTools; an environment overlay; an
// agent). No YAML workflow (workflows are the documented unraiseable residual) and no legacy fields.
func writeComprehensiveYAMLProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	w := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("project.yaml", `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: comprehensive
spec:
  imports:
    - ./policies/guarded.yaml
    - ./tools/github.yaml
    - ./tools/api.yaml
    - ./tools/workspace.yaml
    - ./environments/prod.yaml
    - ./agents/assistant.yaml
  providers:
    models:
      corporate-claude:
        type: anthropic
        apiKeyFrom: env:CORP_ANTHROPIC_KEY
        workspaceIdFrom: env:CORP_WS
`)
	w("policies/guarded.yaml", `apiVersion: agentic.dev/v0
kind: Policy
metadata:
  name: guarded
spec:
  execution:
    maxTotalCostUsd: 5
    maxWallClockSeconds: 300
    requireStructuredOutput: true
  approvals:
    requiredFor:
      - tool.github.create_issue
  effects:
    permit: [workspace.read]
    permitWithApproval: [workspace.write]
  tools:
    forbidUnknownTools: true
  hitl:
    descriptionPrefix: review
    interruptOn:
      github:
        allowedDecisions: [approve, reject]
        description: "gate (${uses})"
`)
	w("tools/github.yaml", `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: github
spec:
  type: mcp
  mcp:
    transport: stdio
    command: npx
    args: ["-y", "server-github"]
    headers: {Authorization: "env:GH_TOKEN"}
  retry: {maxAttempts: 3, backoff: exponential}
  operations:
    create_issue: {schema: schemas/CreateIssue.json, effects: [github.write]}
`)
	w("tools/api.yaml", `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: api
spec:
  type: http
  http:
    baseUrl: https://api.example.com
    headers: {Authorization: "env:API_TOKEN"}
`)
	w("tools/workspace.yaml", `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: workspace
spec:
  type: native
  workspace: {root: sandbox, testCommand: "go test ./..."}
  limits:
    maxToolInputBytes: 1024
    toolInputExceedPolicy: truncate
  safety:
    trusted: true
    sideEffects: true
  operations:
    read_file: {effects: [workspace.read]}
    write_file: {effects: [workspace.write]}
`)
	w("environments/prod.yaml", `apiVersion: agentic.dev/v0
kind: Environment
metadata:
  name: prod
spec:
  overrides:
    agents:
      assistant:
        model: corporate-claude/claude-sonnet-5
        constraints: {timeoutSeconds: 30}
    policies:
      guarded:
        execution: {maxTotalCostUsd: 1}
        approvals:
          requiredFor: [tool.workspace.write_file]
`)
	w("agents/assistant.yaml", `apiVersion: agentic.dev/v0
kind: Agent
metadata:
  name: assistant
spec:
  model: mock/default
  policy: guarded
  constraints: {timeoutSeconds: 60, maxIterations: 8}
  instructions: |
    You are a helpful assistant.
  tools:
    - tool.github.create_issue
    - tool.workspace.read_file
`)
	return root
}

// normResourceSpecJSON normalizes g and returns per-resource normalized spec JSON keyed by "Kind/name"
// plus the project providers — the canonical projection to compare a YAML load against a .agent load.
func normResourceSpecJSON(t *testing.T, g *spec.ProjectGraph) map[string]string {
	t.Helper()
	spec.NormalizeProjectGraph(g)
	out := map[string]string{}
	marshal := func(key string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		out[key] = string(b)
	}
	for n, r := range g.Tools {
		marshal("Tool/"+n, r.Spec)
	}
	for n, r := range g.Policies {
		marshal("Policy/"+n, r.Spec)
	}
	for n, r := range g.Environments {
		marshal("Environment/"+n, r.Spec)
	}
	for n, r := range g.Agents {
		marshal("Agent/"+n, r.Spec)
	}
	marshal("__providers__", g.Spec.Providers)
	return out
}

// TestMigrate_lossless_supportedModel is the ADR 007 step-1 lossless-migration proof: a comprehensive
// declarative YAML project migrates to .agent, re-loads, and yields a graph whose per-resource
// normalized spec JSON is byte-identical to the original YAML graph — no Unsupported findings.
func TestMigrate_lossless_supportedModel(t *testing.T) {
	root := writeComprehensiveYAMLProject(t)

	yamlGraph, err := project.LoadProject(root)
	if err != nil {
		t.Fatalf("load YAML project: %v", err)
	}
	want := normResourceSpecJSON(t, yamlGraph)

	out, errOut, err := runMigrate(t, "migrate", "--to-agent", "--project", root)
	if err != nil {
		t.Fatalf("migrate failed: %v\nstderr:\n%s", err, errOut)
	}
	if strings.Contains(errOut, "need manual migration") {
		t.Fatalf("comprehensive declarative project must migrate without Unsupported findings:\n%s", errOut)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.agent"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	agentGraph, err := project.LoadProject(dir)
	if err != nil {
		t.Fatalf("migrated .agent did not re-load: %v\n%s", err, out)
	}
	got := normResourceSpecJSON(t, agentGraph)

	if len(got) != len(want) {
		t.Fatalf("resource count differs: yaml=%d agent=%d\nyaml keys=%v\nagent keys=%v", len(want), len(got), keysOfStrMap(want), keysOfStrMap(got))
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			t.Fatalf("resource %s missing after migration", k)
		}
		if gv != wv {
			t.Fatalf("resource %s not lossless:\n yaml:  %s\n agent: %s", k, wv, gv)
		}
	}
}

// TestMigrate_lossless_moduloLegacyFields: the comprehensive project PLUS the four removed legacy fields
// migrates to the SAME graph as the legacy-free version — the legacy fields are the only difference and
// are omitted (with warnings). Proves "lossless modulo the intentionally-omitted legacy fields".
func TestMigrate_lossless_moduloLegacyFields(t *testing.T) {
	clean := writeComprehensiveYAMLProject(t)
	cleanGraph, err := project.LoadProject(clean)
	if err != nil {
		t.Fatal(err)
	}
	want := normResourceSpecJSON(t, cleanGraph)

	// A second copy with legacy fields injected into three resources.
	legacy := writeComprehensiveYAMLProject(t)
	inject := func(rel, anchor, add string) {
		p := filepath.Join(legacy, rel)
		b, _ := os.ReadFile(p)
		if err := os.WriteFile(p, []byte(strings.Replace(string(b), anchor, anchor+add, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inject("tools/api.yaml", "    headers: {Authorization: \"env:API_TOKEN\"}\n", "  permissions:\n    allow: [request.send]\n")
	inject("policies/guarded.yaml", "spec:\n", "  security: {networkAccess: restricted}\n")
	inject("agents/assistant.yaml", "  policy: guarded\n", "  runtime: local\n  memory: {type: session, maxMessages: 20}\n")

	out, errOut, err := runMigrate(t, "migrate", "--to-agent", "--project", legacy)
	if err != nil {
		t.Fatalf("migrate (legacy) failed: %v\nstderr:\n%s", err, errOut)
	}
	for _, warn := range []string{"spec.permissions is deprecated", "spec.security is no longer", "spec.runtime is no longer", "spec.memory is no longer"} {
		if !strings.Contains(errOut, warn) {
			t.Fatalf("missing legacy warning %q in:\n%s", warn, errOut)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.agent"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyGraph, err := project.LoadProject(dir)
	if err != nil {
		t.Fatalf("migrated legacy .agent did not re-load: %v\n%s", err, out)
	}
	got := normResourceSpecJSON(t, legacyGraph)
	for k, wv := range want {
		if got[k] != wv {
			t.Fatalf("resource %s diverged after legacy migration (should equal legacy-free):\n clean: %s\n legacy:%s", k, wv, got[k])
		}
	}
}

func keysOfStrMap(m map[string]string) []string {
	var k []string
	for key := range m {
		k = append(k, key)
	}
	return k
}
