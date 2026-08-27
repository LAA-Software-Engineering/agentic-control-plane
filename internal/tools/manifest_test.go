package tools

import (
	"context"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

func toolSpecWithOps(ops map[string][]string) *spec.ToolSpec {
	m := make(map[string]spec.ToolOperation, len(ops))
	for name, eff := range ops {
		m[name] = spec.ToolOperation{Effects: eff}
	}
	return &spec.ToolSpec{Type: "native", Operations: m}
}

func TestDeriveManifest_sortedOperationsAndEffects(t *testing.T) {
	ts := toolSpecWithOps(map[string][]string{
		"read_pr":  {"github.read"},
		"merge_pr": {"github.write", "destructive"},
	})
	m := DeriveManifest("github", ts)
	if m.Tool != "github" {
		t.Fatalf("tool = %q", m.Tool)
	}
	if len(m.Operations) != 2 {
		t.Fatalf("operations = %+v", m.Operations)
	}
	// Operations sorted by name: merge_pr < read_pr.
	if m.Operations[0].Name != "merge_pr" || m.Operations[1].Name != "read_pr" {
		t.Fatalf("operation order = %+v", m.Operations)
	}
	// Effects sorted and unique.
	if got := m.Operations[0].Effects; len(got) != 2 || got[0] != "destructive" || got[1] != "github.write" {
		t.Fatalf("merge_pr effects = %v", got)
	}
}

func TestManifest_IsClosedAndAllows(t *testing.T) {
	closed := DeriveManifest("github", toolSpecWithOps(map[string][]string{"read_pr": {"github.read"}}))
	if !closed.IsClosed() {
		t.Fatal("tool with declared operations must be closed-world")
	}
	if !closed.Allows("read_pr") {
		t.Fatal("declared operation must be allowed")
	}
	if closed.Allows("delete_repo") {
		t.Fatal("undeclared operation must not be allowed by a closed manifest")
	}

	open := DeriveManifest("slack", &spec.ToolSpec{Type: "mcp"})
	if open.IsClosed() {
		t.Fatal("tool with no operations is open")
	}
	if !open.Allows("anything") {
		t.Fatal("open manifest allows every operation")
	}
}

func TestManifestDigest_stableAndDriftSensitive(t *testing.T) {
	a := DeriveManifest("github", toolSpecWithOps(map[string][]string{"read_pr": {"github.read"}}))
	b := DeriveManifest("github", toolSpecWithOps(map[string][]string{"read_pr": {"github.read"}}))
	if a.Digest() != b.Digest() {
		t.Fatal("identical manifests must digest equally")
	}
	// A new operation is manifest drift.
	c := DeriveManifest("github", toolSpecWithOps(map[string][]string{
		"read_pr":     {"github.read"},
		"delete_repo": {"destructive"},
	}))
	if a.Digest() == c.Digest() {
		t.Fatal("adding an operation must change the manifest digest")
	}
	// A changed effect on an existing operation is manifest drift.
	d := DeriveManifest("github", toolSpecWithOps(map[string][]string{"read_pr": {"github.write"}}))
	if a.Digest() == d.Digest() {
		t.Fatal("changing an operation's effects must change the manifest digest")
	}
}

func TestGraphManifestDigest_driftOnOperationChange(t *testing.T) {
	base := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"github": {Metadata: spec.Metadata{Name: "github"}, Spec: *toolSpecWithOps(map[string][]string{"read_pr": {"github.read"}})},
	}}
	widened := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"github": {Metadata: spec.Metadata{Name: "github"}, Spec: *toolSpecWithOps(map[string][]string{
			"read_pr":     {"github.read"},
			"delete_repo": {"destructive"},
		})},
	}}
	if GraphManifestDigest(base) == GraphManifestDigest(widened) {
		t.Fatal("plan-visible manifest digest must change when the callable set widens")
	}
}

func TestManifest_emptyDeclaredManifestIsClosed(t *testing.T) {
	// operations: {} (declared but empty) is a closed world that denies every operation — distinct
	// from an omitted operations key. Shrinking a manifest to empty must not widen it to the universe.
	closedEmpty := DeriveManifest("locked", &spec.ToolSpec{Type: "mcp", OperationsDeclared: true})
	if !closedEmpty.IsClosed() {
		t.Fatal("operations: {} must be a closed manifest")
	}
	if closedEmpty.Allows("anything") {
		t.Fatal("an empty closed manifest must deny every operation")
	}

	omitted := DeriveManifest("legacy", &spec.ToolSpec{Type: "mcp"})
	if omitted.IsClosed() {
		t.Fatal("an omitted operations key is an open manifest (backward compatible)")
	}
	if !omitted.Allows("anything") {
		t.Fatal("an open manifest allows every operation")
	}

	// The two must not share a digest: locking down to empty is not the same as never declaring.
	if closedEmpty.Digest() == omitted.Digest() {
		t.Fatal("closed-empty and omitted manifests must digest differently")
	}
}

func TestManifest_closedBitSurvivesResolveFreeze(t *testing.T) {
	// config.Resolve freezes the graph via CloneProjectGraph (a JSON round-trip). Operations is
	// omitempty, so an empty map would serialize away — the OperationsDeclared presence bit must
	// carry closedness across the freeze, or shrink-to-empty silently reopens the world.
	g := &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"locked": {
			APIVersion: spec.APIVersionV0,
			Kind:       spec.KindTool,
			Metadata:   spec.Metadata{Name: "locked"},
			Spec:       spec.ToolSpec{Type: "mcp", OperationsDeclared: true, Operations: map[string]spec.ToolOperation{}},
		},
	}}
	frozen, err := spec.CloneProjectGraph(g)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	m := ManifestFor(frozen, "locked")
	if !m.IsClosed() {
		t.Fatalf("closed-empty manifest lost its presence bit across the resolve-freeze: %+v", m)
	}
	if m.Allows("delete_repo") {
		t.Fatal("closed-empty manifest must still deny after the freeze")
	}
}

// Discovery may populate a desired manifest during authoring, but it is never an authority source:
// a live tools/list must not widen the manifest. Here the mock MCP server advertises read_file and
// write_file, yet the tool declares only read_file — the manifest still permits only read_file.
func TestManifest_liveDiscoveryNeverWidens(t *testing.T) {
	mcpStdioMu.Lock()
	defer mcpStdioMu.Unlock()
	bin := mockMCPBinary(t)
	g := &spec.ProjectGraph{
		Tools: map[string]*spec.ToolResource{
			"fs": {
				Metadata: spec.Metadata{Name: "fs"},
				Spec: spec.ToolSpec{
					Type: "mcp",
					MCP:  &spec.ToolMCP{Transport: "stdio", Command: bin},
					// Deployed manifest: only read_file is a declared operation.
					Operations: map[string]spec.ToolOperation{
						"read_file": {Effects: []string{"fs.read"}},
					},
				},
			},
		},
	}

	warnings := ApplyMCPSafetyDiscovery(context.Background(), g)
	if len(warnings) != 0 {
		t.Fatalf("unexpected discovery warnings: %+v", warnings)
	}

	m := ManifestFor(g, "fs")
	if !m.Allows("read_file") {
		t.Fatal("declared operation read_file must remain allowed")
	}
	// write_file is advertised by the live server but absent from the deployed manifest.
	if m.Allows("write_file") {
		t.Fatal("live-discovered write_file must not enter the deployed manifest")
	}
	if len(m.Operations) != 1 {
		t.Fatalf("discovery widened the manifest: %+v", m.Operations)
	}
}
