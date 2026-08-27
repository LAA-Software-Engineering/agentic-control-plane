package project

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang/lower"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// agentExt is the authoring-surface source extension (ADR 002 / ADR 003).
const agentExt = ".agent"

// mergeAgentSources discovers every .agent file under rootAbs, lowers the whole
// set to its resource projection, and merges it into g (ADR 003 decision 1:
// ".agent -> in-memory resource graph"). It runs after the YAML resources are
// merged, so a .agent file may reference YAML-declared tools/policies and a
// single-identifier call resolves against YAML-declared workflows.
//
// This is the structural ingress: it surfaces parse and lowering diagnostics
// (with file:line:col positions) as load errors, mirroring what YAML decoding
// does. Type and effect checking of .agent (internal/lang/check) is a semantic
// pass run by `validate`, not the loader — and it cannot live here regardless,
// because internal/lang/check imports this package.
//
// Discovery scans the project tree and skips dot-directories (e.g. .agentic
// deployment state), so authors drop .agent files anywhere in the project rather
// than wiring each into spec.imports the way machine-generated YAML is.
func mergeAgentSources(g *spec.ProjectGraph, rootAbs string) error {
	paths, err := discoverAgentFiles(rootAbs)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return nil
	}

	var diags lang.Diagnostics
	parsed := make([]*lang.File, 0, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		f, d := lang.Parse(p, string(src))
		diags = append(diags, d...)
		parsed = append(parsed, f)
	}

	// Workflow names across the whole .agent set plus YAML workflows already in
	// g, so a single-identifier call classifies as a workflow: step rather than
	// defaulting to agent: (the same set internal/lang/check assembles).
	workflows := collectAgentWorkflowNames(parsed, g)

	for _, f := range parsed {
		res, d := lower.LowerFile(f, lower.Options{Workflows: workflows})
		diags = append(diags, d...)
		if res == nil {
			continue
		}
		if err := MergeLowered(g, res); err != nil {
			return err
		}
	}

	if diags.HasErrors() {
		return fmt.Errorf(".agent compilation failed:\n%s", formatDiagnostics(errorDiags(diags)))
	}
	return nil
}

// errorDiags returns only the error-severity diagnostics (warnings do not fail a
// load).
func errorDiags(diags lang.Diagnostics) lang.Diagnostics {
	var out lang.Diagnostics
	for _, d := range diags {
		if d.Severity == lang.SeverityError {
			out = append(out, d)
		}
	}
	return out
}

// ListAgentFiles returns every .agent file under root (sorted, skipping
// dot-directories) — the same discovery the loader uses, exposed for `agentctl
// fmt`.
func ListAgentFiles(root string) ([]string, error) {
	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, err
	}
	return discoverAgentFiles(abs)
}

// discoverAgentFiles returns every .agent file under rootAbs, sorted, skipping
// dot-directories.
func discoverAgentFiles(rootAbs string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip hidden directories (.git, .agentic, ...) but not the root.
			if path != rootAbs && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), agentExt) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// collectAgentWorkflowNames gathers workflow names declared in the .agent files
// and the YAML workflows already merged into g.
func collectAgentWorkflowNames(files []*lang.File, g *spec.ProjectGraph) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		if f == nil {
			continue
		}
		for _, decl := range f.Decls {
			if wd, ok := decl.(*lang.WorkflowDecl); ok && wd.Name != nil {
				out[wd.Name.Name] = true
			}
		}
	}
	if g != nil {
		for name := range g.Workflows {
			out[name] = true
		}
	}
	return out
}

// formatDiagnostics renders diagnostics as sorted "file:line:col: message" lines.
func formatDiagnostics(diags lang.Diagnostics) string {
	var b strings.Builder
	for i, d := range diags.Sorted() {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  ")
		b.WriteString(d.Pos.String())
		b.WriteString(": ")
		b.WriteString(d.Msg)
	}
	return b.String()
}
