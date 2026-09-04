package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestYAMLDecodeCodec_scopedToCompat is the ADR 007 step-7 fitness guard: the YAML *decode* codec
// (LoadResourceFile / ParseResourceFromBytes) is a restricted isolated-compatibility internal, not a
// project-source ingress. Its production callers must stay limited to the codec itself and the
// `terfyn migrate` legacy-YAML reader; a new caller anywhere else would reintroduce YAML as a second
// source language, which this decision removed. If you are adding a legitimate compat caller, extend
// the allowlist deliberately — do not treat a YAML file as a resource source on a load path.
func TestYAMLDecodeCodec_scopedToCompat(t *testing.T) {
	root := repoRootForCodecTest(t)

	// Files permitted to call the decode codec (repo-relative, slash paths):
	//   - the codec's own definitions (spec)
	//   - internal/project's YAML-only reader used by `terfyn migrate` (loadYAMLGraph + ListProjectYAMLFiles)
	allow := map[string]bool{
		"internal/spec/loader.go":       true,
		"internal/spec/parser.go":       true,
		"internal/project/loader.go":    true, // loadYAMLGraph — migrate's LoadYAMLResources path
		"internal/project/yamlpaths.go": true, // ListProjectYAMLFiles — migrate's file listing
	}

	var offenders []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if allow[rel] {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		body := string(src)
		if strings.Contains(body, "LoadResourceFile(") || strings.Contains(body, "ParseResourceFromBytes(") {
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("YAML decode codec called outside its ADR 007 compat allowlist by: %v\n"+
			"YAML is not a project source — load .agent (project.LoadProject) or build the typed graph "+
			"(config.ResolveGraph). Extend the allowlist only for a deliberate migrate/compat caller.", offenders)
	}
}

// repoRootForCodecTest walks up from the test's working directory to the module root (the dir with
// go.mod), so the fitness scan is independent of which package the test runs in.
func repoRootForCodecTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root (go.mod) above the test working directory")
		}
		dir = parent
	}
}
