package project

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// exportResourcesDir is the directory the exported project.yaml imports; it
// holds one YAML file per non-Project resource. Each file is a single document —
// the loader rejects multi-document files (spec.ErrMultipleDocuments) — so the
// on-disk form is one resource per file even though the stdout stream is a
// single multi-document stream.
const exportResourcesDir = "resources"

// ExportYAML materializes a resource graph as a multi-document YAML stream (ADR
// 003 decision 1: YAML is compilation output produced on demand, never written
// by default). The stream is deterministic — Project first, then Agents, Tools,
// Policies, Workflows, Environments, each sorted by name — so re-exporting an
// unchanged graph is byte-stable. Source positions are excluded (they are
// `yaml:"-"`), matching the ADR rule that positions are diagnostic metadata,
// never identity.
//
// The emitted Project clears spec.imports: every resource is inline in the
// stream, so the import list (which named the original source files) would be
// stale. This stream is for inspection and handoff; WriteProjectDir produces the
// loadable on-disk form.
func ExportYAML(g *spec.ProjectGraph) ([]byte, error) {
	if g == nil {
		return nil, fmt.Errorf("project: nil graph")
	}
	docs := make([]any, 0, 1+len(g.Agents)+len(g.Tools)+len(g.Workflows)+len(g.Policies)+len(g.Environments))

	proj := projectResource(g)
	proj.Spec.Imports = nil
	docs = append(docs, proj)
	docs = append(docs, nonProjectResources(g)...)

	return marshalDocs(docs)
}

// WriteProjectDir writes the graph as a loadable project under dir: a
// project.yaml holding the Project resource (importing the resources/ directory)
// and one single-document YAML file per other resource under resources/.
// LoadProject(dir) reconstructs an identical graph (modulo source positions and
// the rewritten import list, which are not identity). Each resource is its own
// file because the loader rejects multi-document files.
//
// dir is treated as generated output, so it must form a CLOSED set on reload:
//
//   - a dir that already contains .agent sources is refused — LoadProject scans
//     the whole tree for .agent and would merge those alongside the exported
//     YAML, duplicating every resource;
//   - the resources/ directory is fully replaced, so re-exporting a smaller graph
//     into the same dir cannot leave an orphaned resource file that reloads;
//   - two resources that would sanitize to the same filename are an error rather
//     than a silent overwrite.
func WriteProjectDir(dir string, g *spec.ProjectGraph) error {
	if g == nil {
		return fmt.Errorf("project: nil graph")
	}
	if agents, err := discoverAgentFiles(dir); err == nil && len(agents) > 0 {
		return fmt.Errorf("project: refusing to export into %q: it contains .agent sources, which LoadProject would merge alongside the exported YAML (duplicate resources) — export to an empty or non-source directory", dir)
	}

	resDir := filepath.Join(dir, exportResourcesDir)
	// Treat resources/ as generated output: remove any prior contents so a
	// re-export of a smaller graph leaves no orphaned resource files.
	if err := os.RemoveAll(resDir); err != nil {
		return err
	}
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return err
	}

	proj := projectResource(g)
	proj.Spec.Imports = []string{exportResourcesDir}
	projBytes, err := marshalDocs([]any{proj})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "project.yaml"), projBytes, 0o644); err != nil {
		return err
	}

	written := map[string]string{}
	for _, r := range nonProjectResourceEntries(g) {
		name := r.kind + "-" + sanitizeFilename(r.name) + ".yaml"
		if prev, clash := written[name]; clash {
			return fmt.Errorf("project: resources %q and %q both map to file %q; rename one to export", prev, r.kind+"/"+r.name, name)
		}
		written[name] = r.kind + "/" + r.name
		body, err := marshalDocs([]any{r.resource})
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(resDir, name), body, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// resourceEntry pairs a resource with its kind and name for deterministic
// per-file emission.
type resourceEntry struct {
	kind     string
	name     string
	resource any
}

// sanitizeFilename replaces characters unsafe in a filename. Resource names are
// DNS-style identifiers in practice, but a defensive replacement keeps the write
// robust for any metadata.name.
func sanitizeFilename(name string) string {
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}
	out := strings.Map(repl, name)
	if out == "" {
		return "unnamed"
	}
	return out
}

// projectResource reconstructs the Project resource envelope from the graph's
// project-level metadata and spec.
func projectResource(g *spec.ProjectGraph) *spec.ProjectResource {
	return &spec.ProjectResource{
		APIVersion: spec.APIVersionV0,
		Kind:       spec.KindProject,
		Metadata:   g.Meta,
		Spec:       g.Spec,
	}
}

// nonProjectResources returns every non-Project resource in deterministic
// kind-then-name order.
func nonProjectResources(g *spec.ProjectGraph) []any {
	entries := nonProjectResourceEntries(g)
	docs := make([]any, 0, len(entries))
	for _, e := range entries {
		docs = append(docs, e.resource)
	}
	return docs
}

// nonProjectResourceEntries returns every non-Project resource with its kind and
// name, in deterministic kind-then-name order.
func nonProjectResourceEntries(g *spec.ProjectGraph) []resourceEntry {
	var out []resourceEntry
	for _, name := range sortedKeys(g.Agents) {
		out = append(out, resourceEntry{spec.KindAgent, name, g.Agents[name]})
	}
	for _, name := range sortedKeys(g.Tools) {
		out = append(out, resourceEntry{spec.KindTool, name, g.Tools[name]})
	}
	for _, name := range sortedKeys(g.Policies) {
		out = append(out, resourceEntry{spec.KindPolicy, name, g.Policies[name]})
	}
	for _, name := range sortedKeys(g.Workflows) {
		out = append(out, resourceEntry{spec.KindWorkflow, name, g.Workflows[name]})
	}
	for _, name := range sortedKeys(g.Environments) {
		out = append(out, resourceEntry{spec.KindEnvironment, name, g.Environments[name]})
	}
	return out
}

// marshalDocs encodes docs as a multi-document YAML stream separated by `---`.
func marshalDocs(docs []any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			_ = enc.Close()
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
