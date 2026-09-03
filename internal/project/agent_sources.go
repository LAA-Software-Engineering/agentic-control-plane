package project

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/lang/check"
	"github.com/Terfyn/terfyn/internal/spec"
)

// resolveInstructionFiles reads every `instructions file("path")` reference (#360) in a parsed
// .agent file and fills its Resolved text, so lowering copies the file contents into
// AgentSpec.Instructions verbatim. Paths resolve relative to the .agent file's directory and must
// stay within the project root; the result is pinned into the deployment snapshot like any inline
// instruction, so a changed prompt file surfaces as a plan diff rather than a silent change.
func resolveInstructionFiles(f *lang.File, agentPath, rootAbs string) error {
	if f == nil {
		return nil
	}
	baseDir := filepath.Dir(agentPath)
	for _, decl := range f.Decls {
		ad, ok := decl.(*lang.AgentDecl)
		if !ok || ad.InstructionsFile == nil || ad.InstructionsFile.Path == nil {
			continue
		}
		text, err := readInstructionFile(ad.InstructionsFile.Path.Value, baseDir, rootAbs)
		if err != nil {
			return fmt.Errorf("%s: agent instructions file(%q): %w",
				ad.InstructionsFile.Path.Pos, ad.InstructionsFile.Path.Value, err)
		}
		ad.InstructionsFile.Resolved = &text
	}
	return nil
}

// readInstructionFile resolves rel (relative to the .agent file's directory) within the project
// root and returns its UTF-8 contents. An absolute path or one escaping the root is rejected,
// consistent with the closed-world stance; the read is symlink-safe (os.OpenInRoot), so a symlink
// inside the project cannot redirect the read outside it.
func readInstructionFile(rel, baseDir, rootAbs string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("must be a relative path")
	}
	relFromRoot, err := filepath.Rel(rootAbs, filepath.Join(baseDir, rel))
	if err != nil || relFromRoot == ".." || strings.HasPrefix(relFromRoot, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("escapes the project root")
	}
	file, err := os.OpenInRoot(rootAbs, relFromRoot)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("not valid UTF-8 text")
	}
	return string(data), nil
}

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
// Control flow (if/for/parallel for) is compiled and RUN (issue #259): the
// checked execution IR (Program.Executables, pinned into the deployment snapshot
// per #260) is what the run path executes for a control-flow workflow, via the
// execir interpreter rather than the flattened resource DAG. The resource
// projection still flattens both arms — but only for effect analysis
// (effects.Compute), where the union over arms is exactly the sound bound; it is
// never executed for such a workflow.
//
// Discovery scans the project tree and skips dot-directories (e.g. .agentic
// deployment state), so authors drop .agent files anywhere in the project rather
// than wiring each into spec.imports the way machine-generated YAML is.
func compileAgentSources(g *spec.ProjectGraph, rootAbs string) (map[string]*execir.Program, error) {
	paths, err := discoverAgentFiles(rootAbs)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}

	var diags lang.Diagnostics
	parsed := make([]*lang.File, 0, len(paths))
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		f, d := lang.Parse(p, string(src))
		diags = append(diags, d...)
		if err := resolveInstructionFiles(f, p, rootAbs); err != nil {
			return nil, err
		}
		parsed = append(parsed, f)
	}

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
		return nil, fmt.Errorf(".agent compilation failed:\n%s", formatDiagnostics(errorDiags(diags)))
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
		// Inline tool/policy declarations (ADR 005, issue #333) fold back like agents/workflows: a
		// name already in g is a YAML resource cloned by pointer; a genuine cross-ingress duplicate
		// was already reported by MergeLowered inside Check.
		if g.Tools == nil {
			g.Tools = map[string]*spec.ToolResource{}
		}
		for name, t := range prog.Graph.Tools {
			if _, ok := g.Tools[name]; !ok {
				g.Tools[name] = t
			}
		}
		if g.Policies == nil {
			g.Policies = map[string]*spec.PolicyResource{}
		}
		for name, pol := range prog.Graph.Policies {
			if _, ok := g.Policies[name]; !ok {
				g.Policies[name] = pol
			}
		}
		// Inline `environment` declarations (issue #440) fold back like the other kinds: a name already
		// in g is a YAML Environment cloned by pointer; a genuine cross-ingress duplicate was already
		// reported by MergeLowered inside Check. Without this, a .agent-declared environment reaches
		// prog.Graph but is dropped from the returned graph, so `--env <name>` finds nothing.
		if g.Environments == nil {
			g.Environments = map[string]*spec.EnvironmentResource{}
		}
		for name, e := range prog.Graph.Environments {
			if _, ok := g.Environments[name]; !ok {
				g.Environments[name] = e
			}
		}
		// Inline `provider` declarations (issue #440) lower into ProjectSpec.Providers.Models (project
		// config, not a resource map), so they fold back through the spec rather than a graph map. A name
		// already in g is a YAML providers.models entry; a genuine cross-ingress duplicate was already
		// reported by MergeLowered inside Check. Without this, a .agent-declared provider reaches
		// prog.Graph but is dropped from the returned graph, so the model registry never sees the alias.
		if prog.Graph.Spec.Providers != nil {
			for name, cfg := range prog.Graph.Spec.Providers.Models {
				if _, ok := existingProviderModel(g, name); !ok {
					ensureProviderModels(g)[name] = cfg
				}
			}
		}
	}
	// The checked execution IR (positional-arg rebinds included) is the pinned
	// program for every .agent workflow (issue #260); the loader previously
	// dropped it.
	if prog != nil {
		return prog.Executables, nil
	}
	return nil, nil
}

// existingProviderModel looks up a provider alias in g's project spec (nil-safe).
func existingProviderModel(g *spec.ProjectGraph, name string) (spec.ModelProviderConfig, bool) {
	if g.Spec.Providers == nil || g.Spec.Providers.Models == nil {
		return spec.ModelProviderConfig{}, false
	}
	cfg, ok := g.Spec.Providers.Models[name]
	return cfg, ok
}

// ensureProviderModels returns g's provider-alias map, allocating the nested structs if absent.
func ensureProviderModels(g *spec.ProjectGraph) map[string]spec.ModelProviderConfig {
	if g.Spec.Providers == nil {
		g.Spec.Providers = &spec.ProjectProviders{}
	}
	if g.Spec.Providers.Models == nil {
		g.Spec.Providers.Models = map[string]spec.ModelProviderConfig{}
	}
	return g.Spec.Providers.Models
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
