package project

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestListProjectYAMLFiles_planCLIFixture(t *testing.T) {
	root := filepath.Join("..", "cli", "testdata", "plan_project")
	paths, err := ListProjectYAMLFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths=%v", paths)
	}
}

func TestListProjectYAMLFiles_invalidRoot(t *testing.T) {
	_, err := ListProjectYAMLFiles(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "project.yaml") {
		t.Fatalf("got %v", err)
	}
}
