package scaffold

import (
	"strings"
	"testing"
)

func TestAppendProjectImport_preservesExistingContent(t *testing.T) {
	src := []byte(`apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  # team-maintained imports
  imports:
    - ./policies/default.yaml
  defaults:
    policy: default
`)
	out, added, err := appendProjectImport(src, "./tools/new.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !added {
		t.Fatal("expected import append")
	}
	text := string(out)
	if !strings.Contains(text, "./tools/new.yaml") {
		t.Fatalf("missing new import:\n%s", text)
	}
	if !strings.Contains(text, "name: demo") {
		t.Fatal("metadata name missing after append")
	}
	if !strings.Contains(text, "./policies/default.yaml") {
		t.Fatal("existing import missing after append")
	}
	if !strings.Contains(text, "policy: default") {
		t.Fatal("defaults block missing after append")
	}
	// yaml.v3 node encoding should retain this comment when attached to the imports key.
	if !strings.Contains(text, "team-maintained imports") {
		t.Fatalf("expected imports comment to survive append:\n%s", text)
	}
}
