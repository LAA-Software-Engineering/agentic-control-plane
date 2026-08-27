package project

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang/check"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// agentExt is the authoring-surface source extension (ADR 002 / ADR 003).
const agentExt = ".agent"

// IsAgentSource reports whether path names a .agent authoring source. It is the
// single predicate discovery, the loader, and `terfyn fmt` share, so the set
// of files formatted is exactly the set the loader ingests (case-insensitive on
// the extension).
func IsAgentSource(path string) bool {
	return strings.EqualFold(filepath.Ext(path), agentExt)
}

// compileAgentSources discovers every .agent file under rootAbs, compiles the
// whole set through internal/lang/check (type and effect checking, plus the
// positional workflow-argument rebind), and merges the CHECKED resource
// projection into g (ADR 003 decision 1: ".agent -> in-memory resource graph").
// It runs after the YAML resources are merged, so a .agent file may reference
// YAML-declared tools/policies and a single-identifier call resolves against
// YAML-declared workflows.
//
// Why the checker, not just lowering: the graph produced here is what
// validate/plan/apply/run consume, so it must be the EXECUTABLE form. Bare
// lower.LowerFile produces the effect-analysis over-approximation — positional
// workflow: arguments are placeholder-keyed (arg0/arg1) and never rebound to the
// callee's parameter names — which the engine cannot run correctly. check.Check
// applies those rebinds (applyRebinds) and reports type/effect errors, which the
// loader surfaces as compilation failures. This does not create an import cycle:
// check no longer imports this package (MergeLowered moved to internal/lang/lower).
//
// Control flow is refused (see controlFlowGate): the resource projection cannot
// represent if/for — it flattens both arms into steps — and the execution IR that
// can (internal/execir) is not wired into the engine yet. Merging a flattened
// control-flow workflow would put a program on the run path that executes every
// arm and returns whichever the merge wrote last, so such workflows are a load
// error until execir executes on the engine (#207 follow-up).
//
// Discovery scans the project tree and skips dot-directories (e.g. .agentic
// deployment state), so authors drop .agent files anywhere in the project rather
// than wiring each into spec.imports the way machine-generated YAML is.
func compileAgentSources(g *spec.ProjectGraph, rootAbs string) error {
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

	// Refuse workflows the engine cannot execute yet, before type checking, so
	// the diagnostic names the construct rather than a downstream symptom.
	diags = append(diags, controlFlowGate(parsed)...)

	// Compile the whole unit: the checker lowers every file, merges onto a clone
	// of g (the YAML resources), rebinds positional workflow arguments, and
	// type/effect-checks. prog.Graph is the executable projection.
	prog, checkDiags := check.Check(parsed[0], check.Options{
		Files:     parsed[1:],
		Project:   g,
		SchemaDir: rootAbs,
	})
	diags = append(diags, checkDiags...)

	if diags.HasErrors() {
		return fmt.Errorf(".agent compilation failed:\n%s", formatDiagnostics(errorDiags(diags)))
	}

	// Fold the checked .agent resources into g. Names already present are YAML
	// resources check cloned by pointer; a genuine cross-ingress duplicate was
	// already reported by check above (MergeLowered inside Check), so only new,
	// checked resources reach here.
	if prog != nil && prog.Graph != nil {
		for name, a := range prog.Graph.Agents {
			if _, ok := g.Agents[name]; !ok {
				g.Agents[name] = a
			}
		}
		for name, w := range prog.Graph.Workflows {
			if _, ok := g.Workflows[name]; !ok {
				g.Workflows[name] = w
			}
		}
	}
	return nil
}

// controlFlowGate reports a diagnostic for every workflow that uses a
// conditional or loop. The outermost control-flow construct is always at
// statement level in the workflow body (an inner if/for is nested inside an
// outer one), so a single top-level scan finds every control-flow workflow.
func controlFlowGate(files []*lang.File) lang.Diagnostics {
	var diags lang.Diagnostics
	for _, f := range files {
		if f == nil {
			continue
		}
		for _, decl := range f.Decls {
			wd, ok := decl.(*lang.WorkflowDecl)
			if !ok {
				continue
			}
			pos, found := firstControlFlow(wd.Body)
			if !found {
				continue
			}
			name := "?"
			if wd.Name != nil {
				name = wd.Name.Name
			}
			diags = append(diags, lang.Diagnostic{
				Pos: pos,
				Msg: fmt.Sprintf("workflow %q uses control flow (if/for), which is not executable yet: "+
					"the execution IR that represents conditionals and loops (internal/execir) is not wired into "+
					"the engine (#207 follow-up). Use straight-line steps and parallel { } here, or keep the "+
					"branching inside an agent, until .agent control flow executes end-to-end.", name),
			})
		}
	}
	return diags
}

// firstControlFlow returns the position of the first if/for statement in body.
func firstControlFlow(body []lang.Stmt) (spec.Pos, bool) {
	for _, st := range body {
		switch s := st.(type) {
		case *lang.IfStmt:
			return s.Pos, true
		case *lang.ForStmt:
			return s.Pos, true
		}
	}
	return spec.Pos{}, false
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
// dot-directories) — the same discovery the loader uses, exposed for `terfyn
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
		if IsAgentSource(path) {
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
