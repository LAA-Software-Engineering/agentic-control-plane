package project

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestListProjectYAMLFiles_planCLIFixture(t *testing.T) {
	// fmt_messy is a still-YAML fixture (project.yaml + policy.yaml + tool.yaml) — the codec-level
	// ListProjectYAMLFiles helper (used by migrate) reads YAML directly, unaffected by the ADR 007
	// source reject.
	root := filepath.Join("..", "cli", "testdata", "fmt_messy")
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
