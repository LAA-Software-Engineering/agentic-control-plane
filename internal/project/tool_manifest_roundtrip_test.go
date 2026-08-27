package project

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/tools"
)

// ADR 003 acceptance for the closed-world capability manifest (#204 / PR #251 review): a
// declared-but-empty operations: {} manifest must round-trip through export → load unchanged, so
// the YAML interchange path agrees with plan/apply identity and CheckToolCall. Without
// ToolSpec.MarshalYAML the empty map is dropped and reload reopens every live tools/list name.
func TestExport_ClosedEmptyManifestRoundTrips(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "project.yaml", `apiVersion: agentic.dev/v0
kind: Project
metadata:
  name: demo
spec:
  imports:
    - resources
`)
	writeFile(t, root, "resources/tool-locked.yaml", `apiVersion: agentic.dev/v0
kind: Tool
metadata:
  name: locked
spec:
  type: native
  operations: {}
`)

	g, err := LoadProject(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if m := tools.ManifestFor(g, "locked"); !m.IsClosed() || m.Allows("delete_repo") {
		t.Fatalf("loaded closed-empty tool not closed: closed=%v allows=%v", m.IsClosed(), m.Allows("delete_repo"))
	}

	// Export the graph, then reload from the exported directory (ADR 003 identity contract).
	out := t.TempDir()
	if err := WriteProjectDir(out, g); err != nil {
		t.Fatalf("export: %v", err)
	}
	reloaded, err := LoadProject(out)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	m := tools.ManifestFor(reloaded, "locked")
	if !m.IsClosed() {
		t.Fatal("export → load reopened the closed-empty manifest")
	}
	if m.Allows("delete_repo") {
		t.Fatal("reloaded manifest must still deny operations outside it")
	}

	// The exported YAML must carry the operations key, not drop it.
	yamlBytes, err := ExportYAML(g)
	if err != nil {
		t.Fatalf("ExportYAML: %v", err)
	}
	if !strings.Contains(string(yamlBytes), "operations:") {
		t.Fatalf("exported YAML dropped the closed-empty manifest:\n%s", yamlBytes)
	}
}
