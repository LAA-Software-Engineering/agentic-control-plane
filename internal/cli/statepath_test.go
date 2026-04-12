package cli

import (
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestResolveStateSQLitePath_overrideRelative(t *testing.T) {
	root := t.TempDir()
	g := &spec.ProjectGraph{}
	got, err := resolveStateSQLitePath(root, g, "custom.db")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(root, "custom.db"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveStateSQLitePath_projectSpecDSN(t *testing.T) {
	root := t.TempDir()
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			State: &spec.ProjectStateConfig{DSN: "./mine.db"},
		},
	}
	got, err := resolveStateSQLitePath(root, g, "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(root, "mine.db"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveStateSQLitePath_defaultUnderProject(t *testing.T) {
	root := t.TempDir()
	got, err := resolveStateSQLitePath(root, &spec.ProjectGraph{}, "")
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(filepath.Join(root, ".agentic", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
