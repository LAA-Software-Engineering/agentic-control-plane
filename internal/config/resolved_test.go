package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fmt"
)

// writeProject writes a minimal .agent project (ADR 007: .agent is the sole source). Project-level
// defaults become a `defaults { … }` block; operator config (state) is no longer a source concern, so
// it comes from the default state path or a user-local overlay, not the project. Model values are
// coerced to the <provider>/<name> form the grammar requires.
func writeProject(t *testing.T, root string, specDefaults map[string]string) {
	t.Helper()
	var b strings.Builder
	if len(specDefaults) > 0 {
		b.WriteString("defaults {\n")
		if v, ok := specDefaults["model"]; ok {
			fmt.Fprintf(&b, "    model %s\n", ensureModelRef(v))
		}
		if v, ok := specDefaults["runtime"]; ok {
			fmt.Fprintf(&b, "    runtime %s\n", v)
		}
		if v, ok := specDefaults["policy"]; ok {
			fmt.Fprintf(&b, "    policy %s\n", v)
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("agent assistant {\n    model mock/default\n}\n")
	writeYAML(t, filepath.Join(root, "main.agent"), b.String())
}

// ensureModelRef coerces a bare model name to the <provider>/<name> form the .agent grammar requires,
// so a test fixture can pass a plain label like "project-model".
func ensureModelRef(v string) string {
	if strings.Contains(v, "/") {
		return v
	}
	return "mock/" + v
}

func TestResolve_precedenceLadder(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeProject(t, root, map[string]string{"model": "project-model", "runtime": "local"})

	writeYAML(t, filepath.Join(home, ".config", "terfyn", "config.yaml"), `
defaults:
  model: user-global-model
state:
  dsn: /tmp/global-state.db
`)
	writeYAML(t, filepath.Join(root, ".agentic", "local.yaml"), `
defaults:
  model: user-local-model
state:
  dsn: /tmp/local-state.db
`)

	rc, err := Resolve(ResolveOptions{ProjectRoot: root, HomeDir: home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	g := rc.Graph()
	// defaults are .agent project source: the project value wins over both overlays (overlays only fill
	// fields the project left unset).
	if g.Spec.Defaults.Model != "mock/project-model" {
		t.Fatalf("project model should win, got %q", g.Spec.Defaults.Model)
	}
	if g.Spec.Defaults.Runtime != "local" {
		t.Fatalf("project runtime should remain local, got %q", g.Spec.Defaults.Runtime)
	}
	// state is operator-config with no project-source layer (ADR 007): the highest-precedence overlay —
	// the project-local .agentic/local.yaml — wins over the user-global overlay.
	if g.Spec.State.DSN != "/tmp/local-state.db" {
		t.Fatalf("project-local overlay state should win, got %q", g.Spec.State.DSN)
	}
}

func TestResolve_cliStateOverride(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, nil)
	custom := filepath.Join(root, "custom.db")
	rc, err := Resolve(ResolveOptions{ProjectRoot: root, StatePath: custom})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if rc.StatePath() != custom {
		t.Fatalf("StatePath = %q, want %q", rc.StatePath(), custom)
	}
	d1 := rc.Digest()
	rc2, err := Resolve(ResolveOptions{ProjectRoot: root, StatePath: custom})
	if err != nil {
		t.Fatal(err)
	}
	if rc2.Digest() != d1 {
		t.Fatal("digest should be stable for same inputs")
	}
	rc3, err := Resolve(ResolveOptions{ProjectRoot: root, StatePath: filepath.Join(root, "other.db")})
	if err != nil {
		t.Fatal(err)
	}
	if rc3.Digest() == d1 {
		t.Fatal("digest should change when CLI state override changes")
	}
}

func TestResolvedConfig_snapshotRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, nil)

	rc, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteSnapshot(rc); err != nil {
		t.Fatal(err)
	}
	if err := AssertSnapshotMatchesStored(rc); err != nil {
		t.Fatalf("matching snapshot should pass: %v", err)
	}

	other, err := Resolve(ResolveOptions{ProjectRoot: root, StatePath: filepath.Join(root, "mutated.db")})
	if err != nil {
		t.Fatal(err)
	}
	err = AssertSnapshotMatchesStored(other)
	if err == nil {
		t.Fatal("expected drift error")
	}
	if !errors.Is(err, ErrResolvedConfigDrift) {
		t.Fatalf("want ErrResolvedConfigDrift, got %v", err)
	}
}

func TestResolvedConfig_digestStability(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, map[string]string{"model": "m1"})
	rc1, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	rc2, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if rc1.Digest() != rc2.Digest() {
		t.Fatalf("digests differ: %s vs %s", rc1.Digest(), rc2.Digest())
	}
}

func TestResolve_unknownFieldInUserLocal(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeProject(t, root, nil)
	writeYAML(t, filepath.Join(home, ".config", "terfyn", "config.yaml"), "defualts:\n  model: x\n")
	_, err := Resolve(ResolveOptions{ProjectRoot: root, HomeDir: home})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "defualts") {
		t.Fatalf("want typo in error: %v", err)
	}
}

// TestResolve_rejectsYAMLProjectSource proves the ADR 007 reject reaches config.Resolve (the shared
// load path for validate/plan/apply/run): a project.yaml manifest is refused with a migrate hint, so
// no CLI command silently ingests a YAML project. (Strict unknown-field decoding still lives in the
// retained YAML codec and is exercised at the internal/spec level.)
func TestResolve_rejectsYAMLProjectSource(t *testing.T) {
	root := t.TempDir()
	writeYAML(t, filepath.Join(root, "project.yaml"), `
apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
`)
	_, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err == nil {
		t.Fatal("expected a YAML-source rejection")
	}
	if !strings.Contains(err.Error(), "no longer an accepted project source") || !strings.Contains(err.Error(), "migrate --to-agent") {
		t.Fatalf("want ADR 007 reject with migrate hint, got: %v", err)
	}
}

func TestAssertSnapshotMatchesStored_invalidEmptyDigest(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, nil)
	rc, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	path := SnapshotPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"digest":"","environment":"local"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err = AssertSnapshotMatchesStored(rc)
	if err == nil {
		t.Fatal("expected invalid snapshot error")
	}
	if !errors.Is(err, ErrInvalidSnapshot) {
		t.Fatalf("want ErrInvalidSnapshot, got %v", err)
	}
}

func TestAssertSnapshotMatchesStored_missingFile(t *testing.T) {
	root := t.TempDir()
	writeProject(t, root, nil)
	rc, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if err := AssertSnapshotMatchesStored(rc); err != nil {
		t.Fatalf("missing snapshot should not error: %v", err)
	}
}

func TestSnapshotPath(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, ".agentic", "resolved-config.json")
	if got := SnapshotPath(root); got != want {
		t.Fatalf("SnapshotPath = %q, want %q", got, want)
	}
	_ = os.MkdirAll(filepath.Dir(want), 0o755)
}

func TestResolve_doesNotEnforceEffectBounds(t *testing.T) {
	root := t.TempDir()
	// The agent reaches tool.kubernetes.restart (production.write) under a policy that permits only
	// production.read — a policy-effect-bound violation (effects.Check's domain, run by validate/plan),
	// NOT a workflow effects-clause violation (the loader's check.Check). So the project loads, and
	// Resolve must return the graph without enforcing the bound.
	writeYAML(t, filepath.Join(root, "main.agent"), `tool kubernetes {
    type native
    operations {
        restart { effects { production.write } }
    }
}

policy staging-only {
    effects {
        permit { production.read }
    }
}

agent deploy-agent {
    model mock/gpt-4
    policy staging-only
    instructions "Restart the service when asked."
    grants {
        tool.kubernetes.restart
    }
}

workflow deploy-production(input: any) policy staging-only {
    remediate = deploy-agent(input)
    return remediate
}
`)
	rc, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("Resolve must not run effects.Check (validate/plan only): %v", err)
	}
	if rc.Graph() == nil {
		t.Fatal("expected graph")
	}
}

// TestResolve_agentOnlyProject is issue #430: config.Resolve loads a project with only .agent source
// (no project.yaml) end to end — validate/plan/apply/run all go through Resolve, so a non-error here
// with a populated graph and a stable digest is the whole static lifecycle working YAML-free.
func TestResolve_agentOnlyProject(t *testing.T) {
	root := t.TempDir()
	writeYAML(t, filepath.Join(root, "main.agent"), `agent assistant {
    model mock/default

    instructions """
    You are a helpful assistant.
    """
}

workflow hello(input: string) -> string {
    return assistant(input)
}
`)
	rc, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err != nil {
		t.Fatalf("agent-only project must resolve without project.yaml: %v", err)
	}
	g := rc.Graph()
	if g.Meta.Name != filepath.Base(root) {
		t.Fatalf("project name = %q, want the directory basename %q", g.Meta.Name, filepath.Base(root))
	}
	if g.Workflows["hello"] == nil || g.Agents["assistant"] == nil {
		t.Fatalf(".agent resources missing: agents=%v workflows=%v", len(g.Agents), len(g.Workflows))
	}
	if strings.TrimSpace(rc.Digest()) == "" {
		t.Fatal("resolved config digest must be non-empty")
	}
	// The default state path lands under the project root (no project.yaml state block needed).
	if want := filepath.Join(root, ".agentic", "state.db"); rc.StatePath() != want {
		t.Fatalf("state path = %q, want %q", rc.StatePath(), want)
	}
}

// TestResolve_yamlSourceDeprecation is issue #440 Phase 2a: a project loaded from a hand-authored
// .agent-only project (the only loadable kind under ADR 007) is not flagged as a deprecated source.
// A YAML project is no longer merely deprecated — it is rejected outright (see
// TestResolve_rejectsYAMLProjectSource).
func TestResolve_yamlSourceDeprecation(t *testing.T) {
	// .agent-only project (no project.yaml) → not flagged.
	agentRoot := t.TempDir()
	writeYAML(t, filepath.Join(agentRoot, "main.agent"), `agent a {
    model mock/default
    instructions "x"
}

workflow w(input: any) -> any {
    return input
}
`)
	rcAgent, err := Resolve(ResolveOptions{ProjectRoot: agentRoot})
	if err != nil {
		t.Fatalf("resolve .agent-only project: %v", err)
	}
	if w := rcAgent.SourceDeprecation(); w != "" {
		t.Fatalf(".agent-only project must not be flagged, got %q", w)
	}
}
