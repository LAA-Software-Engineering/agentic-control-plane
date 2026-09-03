package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/project"
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
