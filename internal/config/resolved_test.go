package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func writeProject(t *testing.T, root string, specDefaults map[string]string) {
	t.Helper()
	defaults := ""
	if specDefaults != nil {
		defaults = "  defaults:\n"
		for k, v := range specDefaults {
			defaults += fmt.Sprintf("    %s: %s\n", k, v)
		}
	}
	content := fmt.Sprintf(`apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
%s  state:
    backend: sqlite
    dsn: .agentic/state.db
`, defaults)
	writeYAML(t, filepath.Join(root, "project.yaml"), content)
}

func TestResolve_precedenceLadder(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	writeProject(t, root, map[string]string{"model": "project-model", "runtime": "local"})

	writeYAML(t, filepath.Join(home, ".config", "agentctl", "config.yaml"), `
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
	if g.Spec.Defaults.Model != "project-model" {
		t.Fatalf("project model should win, got %q", g.Spec.Defaults.Model)
	}
	if g.Spec.Defaults.Runtime != "local" {
		t.Fatalf("project runtime should remain local, got %q", g.Spec.Defaults.Runtime)
	}
	if !strings.HasSuffix(g.Spec.State.DSN, ".agentic/state.db") {
		t.Fatalf("project state should win, got %q", g.Spec.State.DSN)
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
	writeYAML(t, filepath.Join(home, ".config", "agentctl", "config.yaml"), "defualts:\n  model: x\n")
	_, err := Resolve(ResolveOptions{ProjectRoot: root, HomeDir: home})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "defualts") {
		t.Fatalf("want typo in error: %v", err)
	}
}

func TestResolve_unknownFieldInProject(t *testing.T) {
	root := t.TempDir()
	writeYAML(t, filepath.Join(root, "project.yaml"), `
apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  defualts:
    model: x
`)
	_, err := Resolve(ResolveOptions{ProjectRoot: root})
	if err == nil {
		t.Fatal("expected error")
	}
	var le *spec.LoadError
	if !errors.As(err, &le) {
		t.Fatalf("want LoadError, got %T: %v", err, err)
	}
	if !errors.Is(err, spec.ErrUnknownField) {
		t.Fatalf("want ErrUnknownField: %v", err)
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
