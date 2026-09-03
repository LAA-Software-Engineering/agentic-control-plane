package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newScaffoldProject writes a minimal valid .agent-only project and returns its root.
func newScaffoldProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	src := `agent seed {
    model mock/default
    instructions "seed"
}

policy default {
    preset shell_safe
}

workflow hello(input: any) -> any {
    return input
}
`
	if err := os.WriteFile(filepath.Join(root, "main.agent"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runScaffold(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ResetGlobalsForTest()
	cmd := NewRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	return func() (string, error) { err := cmd.Execute(); return out.String(), err }()
}

// TestNew_scaffoldsEachKindAndValidates is issue #439: terfyn new appends a valid starter .agent
// declaration for every resource kind, and the project still validates afterward — no YAML.
func TestNew_scaffoldsEachKindAndValidates(t *testing.T) {
	root := newScaffoldProject(t)
	for _, tc := range []struct{ kind, name string }{
		{"agent", "reviewer"},
		{"workflow", "pipeline"},
		{"tool", "fetcher"},
		{"policy", "guarded"},
	} {
		if out, err := runScaffold(t, "new", tc.kind, tc.name, "--project", root); err != nil {
			t.Fatalf("new %s %s: %v\n%s", tc.kind, tc.name, err, out)
		}
	}

	body, err := os.ReadFile(filepath.Join(root, "main.agent"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"agent reviewer", "workflow pipeline", "tool fetcher", "policy guarded"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("missing %q in main.agent:\n%s", want, body)
		}
	}
	// No YAML was ever created.
	if _, err := os.Stat(filepath.Join(root, "project.yaml")); !os.IsNotExist(err) {
		t.Fatalf("new must not create project.yaml, stat err = %v", err)
	}
	// The project still validates.
	if out, err := runScaffold(t, "validate", "--project", root); err != nil {
		t.Fatalf("validate after scaffolding: %v\n%s", err, out)
	}
}

// TestNew_refusesCollision: a resource of the same kind and name that already exists is refused.
func TestNew_refusesCollision(t *testing.T) {
	root := newScaffoldProject(t)
	_, err := runScaffold(t, "new", "agent", "seed", "--project", root) // "seed" already exists
	if err == nil {
		t.Fatal("expected a collision error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("got %v", err)
	}
}

// TestNew_dryRunDoesNotWrite: --dry-run prints the declaration and leaves the file untouched.
func TestNew_dryRunDoesNotWrite(t *testing.T) {
	root := newScaffoldProject(t)
	target := filepath.Join(root, "main.agent")
	before, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runScaffold(t, "new", "agent", "brandnew", "--dry-run", "--project", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "agent brandnew") || !strings.Contains(out, "Dry run") {
		t.Fatalf("dry-run output:\n%s", out)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("main.agent changed during --dry-run")
	}
}

// TestNew_targetFileFlag: --file directs the declaration to another .agent file under the root.
func TestNew_targetFileFlag(t *testing.T) {
	root := newScaffoldProject(t)
	if out, err := runScaffold(t, "new", "agent", "helper", "--file", "agents.agent", "--project", root); err != nil {
		t.Fatalf("new --file: %v\n%s", err, out)
	}
	body, err := os.ReadFile(filepath.Join(root, "agents.agent"))
	if err != nil {
		t.Fatalf("expected agents.agent to be created: %v", err)
	}
	if !strings.Contains(string(body), "agent helper") {
		t.Fatalf("declaration missing from agents.agent:\n%s", body)
	}
	if out, err := runScaffold(t, "validate", "--project", root); err != nil {
		t.Fatalf("validate after scaffolding to a new file: %v\n%s", err, out)
	}
}

func TestNew_rejectsInvalidName(t *testing.T) {
	root := newScaffoldProject(t)
	_, err := runScaffold(t, "new", "agent", "1bad", "--project", root)
	if err == nil {
		t.Fatal("expected an invalid-name error")
	}
	if ExitCodeOf(err) != ExitValidationError {
		t.Fatalf("exit=%d err=%v", ExitCodeOf(err), err)
	}
}
