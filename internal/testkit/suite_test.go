package testkit

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseSuiteBytes_minimal(t *testing.T) {
	data := []byte(`workflow: w
cases:
  - name: a
    input:
      x: 1
    expect:
      outputContains:
        - ok
`)
	s, err := ParseSuiteBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if s.Workflow != "w" || len(s.Cases) != 1 || s.Cases[0].Name != "a" {
		t.Fatalf("%+v", s)
	}
}

func TestParseSuiteBytes_errors(t *testing.T) {
	_, err := ParseSuiteBytes([]byte(`cases: []`))
	if err == nil || !strings.Contains(err.Error(), "workflow") {
		t.Fatalf("%v", err)
	}
}

func TestDiscoverSuitePaths_repoExample(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Caller")
	}
	// internal/testkit -> internal/cli/testdata/wf_tests
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "cli", "testdata", "wf_tests"))
	paths, err := DiscoverSuitePaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "demo.yaml") {
		t.Fatalf("%v", paths)
	}
}

func TestDiscoverSuitePaths_missing(t *testing.T) {
	paths, err := DiscoverSuitePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("%v", paths)
	}
}
