package project

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang/lower"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/util"
)

// YAML file suffixes loaded from directories (recursive) and explicit import paths.
const yamlExt = ".yaml"
const ymlExt = ".yml"

// LoadProject loads root/project.yaml (or project.yml), expands spec.imports, parses
// every YAML document with [internal/spec], and merges resources into a ProjectGraph.
// Duplicate kind/metadata.name pairs are rejected (§9.1). Only the root project file
// may define kind Project.
func LoadProject(root string) (*spec.ProjectGraph, error) {
	g, _, err := LoadProjectWithExecutables(root)
	return g, err
}

// LoadProjectWithExecutables loads the project graph and, alongside it, the
// execution IR of every workflow — the pinned program #260 folds into the
// workflow identity and persists in the deployment snapshot. `.agent` workflows
// use check.Check's checked program (positional-arg rebinds included); every
// other workflow lowers via lower.LowerWorkflowResource (#256). A workflow that
// cannot lower has no program (the DAG path still runs it); the map only omits it.
func LoadProjectWithExecutables(root string) (*spec.ProjectGraph, map[string]*execir.Program, error) {
	g, agentExecs, err := loadProjectGraph(root)
	if err != nil {
		return nil, nil, err
	}
	return g, buildExecutables(g, agentExecs), nil
}

// buildExecutables unions the checked `.agent` programs with a lowered program for
// every other (YAML) workflow.
func buildExecutables(g *spec.ProjectGraph, agentExecs map[string]*execir.Program) map[string]*execir.Program {
	if g == nil {
		return nil
	}
	out := make(map[string]*execir.Program, len(g.Workflows))
	for name, wf := range g.Workflows {
		if p, ok := agentExecs[name]; ok && p != nil {
			out[name] = p
			continue
		}
		if wf == nil {
			continue
		}
		prog, diags := lower.LowerWorkflowResource(wf)
		if diags.HasErrors() {
			continue
		}
		out[name] = prog
	}
	return out
}

func loadProjectGraph(root string) (*spec.ProjectGraph, map[string]*execir.Program, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, nil, fmt.Errorf("project root: %w", err)
	}

	projPath, err := findProjectFile(rootAbs)
	if err != nil {
		return nil, nil, err
	}

	dec, err := spec.LoadResourceFile(projPath)
	if err != nil {
		return nil, nil, err
	}
	pr, ok := dec.Resource.(*spec.ProjectResource)
	if !ok || dec.Kind() != spec.KindProject {
		return nil, nil, fmt.Errorf("%s: expected kind Project, got %q", projPath, dec.Kind())
	}
	relocateLoaded(rootAbs, projPath, pr)

	g := &spec.ProjectGraph{
		Meta:         pr.Metadata,
		Pos:          pr.Pos,
		Spec:         pr.Spec,
		Agents:       make(map[string]*spec.AgentResource),
		Tools:        make(map[string]*spec.ToolResource),
		Workflows:    make(map[string]*spec.WorkflowResource),
		Policies:     make(map[string]*spec.PolicyResource),
		Environments: make(map[string]*spec.EnvironmentResource),
	}

	seen := map[resourceKey]string{
		{kind: spec.KindProject, name: strings.TrimSpace(pr.Metadata.Name)}: projPath,
	}

	files, err := expandImports(rootAbs, projPath, g.Spec.Imports)
	if err != nil {
		return nil, nil, err
	}

	for _, path := range files {
		if path == projPath {
			continue
		}
		d, err := spec.LoadResourceFile(path)
		if err != nil {
			return nil, nil, err
		}
		relocateLoaded(rootAbs, path, d.Resource)
		if err := mergeDecoded(g, d, path, seen); err != nil {
			return nil, nil, err
		}
	}

	// .agent authoring surface (ADR 003): compile every .agent file under the
	// project root and merge its checked resource projection. Runs after YAML so
	// .agent may reference YAML-declared resources.
	agentExecs, err := compileAgentSources(g, rootAbs)
	if err != nil {
		return nil, nil, err
	}

	return g, agentExecs, nil
}

type resourceKey struct {
	kind string
	name string
}

// FindProjectFile returns the absolute path to project.yaml or project.yml under dir.
func FindProjectFile(dir string) (string, error) {
	return findProjectFile(dir)
}

func findProjectFile(dir string) (string, error) {
	for _, name := range []string{"project.yaml", "project.yml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no project.yaml or project.yml in %q", dir)
}

func expandImports(rootAbs, projPath string, imports []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string

	add := func(p string) {
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}

	add(projPath)

	for _, imp := range imports {
		imp = strings.TrimSpace(imp)
		if imp == "" {
			continue
		}
		if filepath.IsAbs(imp) {
			return nil, fmt.Errorf("import %q: absolute paths are not allowed", imp)
		}
		full := filepath.Join(rootAbs, filepath.FromSlash(imp))
		full = filepath.Clean(full)
		if !util.IsUnderRoot(rootAbs, full) {
			return nil, fmt.Errorf("import %q resolves outside project root", imp)
		}

		fi, err := os.Stat(full)
		if err != nil {
			return nil, fmt.Errorf("import %q: %w", imp, err)
		}

		if fi.IsDir() {
			list, err := walkYAMLFiles(full)
			if err != nil {
				return nil, fmt.Errorf("import %q: %w", imp, err)
			}
			for _, f := range list {
				add(f)
			}
		} else {
			add(full)
		}
	}

	sort.Strings(out)
	return out, nil
}

func walkYAMLFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == yamlExt || ext == ymlExt {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func mergeDecoded(g *spec.ProjectGraph, d *spec.Decoded, path string, seen map[resourceKey]string) error {
	kind := d.Kind()
	if kind == spec.KindProject {
		return fmt.Errorf("%s: kind Project must only be defined in the root project.yaml", path)
	}

	id := d.ResourceID()
	name := strings.TrimSpace(id.Name)
	if name == "" {
		return fmt.Errorf("%s: resource has empty metadata.name", path)
	}

	key := resourceKey{kind: kind, name: name}
	if prev, ok := seen[key]; ok {
		return &DuplicateResourceError{Kind: kind, Name: name, Paths: []string{prev, path}}
	}
	seen[key] = path

	switch kind {
	case spec.KindAgent:
		ar, ok := d.Resource.(*spec.AgentResource)
		if !ok {
			return fmt.Errorf("%s: internal error: wrong type for Agent", path)
		}
		g.Agents[name] = ar
	case spec.KindTool:
		tr, ok := d.Resource.(*spec.ToolResource)
		if !ok {
			return fmt.Errorf("%s: internal error: wrong type for Tool", path)
		}
		g.Tools[name] = tr
	case spec.KindWorkflow:
		wr, ok := d.Resource.(*spec.WorkflowResource)
		if !ok {
			return fmt.Errorf("%s: internal error: wrong type for Workflow", path)
		}
		g.Workflows[name] = wr
	case spec.KindPolicy:
		pr, ok := d.Resource.(*spec.PolicyResource)
		if !ok {
			return fmt.Errorf("%s: internal error: wrong type for Policy", path)
		}
		g.Policies[name] = pr
	case spec.KindEnvironment:
		er, ok := d.Resource.(*spec.EnvironmentResource)
		if !ok {
			return fmt.Errorf("%s: internal error: wrong type for Environment", path)
		}
		g.Environments[name] = er
	default:
		return fmt.Errorf("%s: unsupported kind %q", path, kind)
	}
	return nil
}

func relocateLoaded(rootAbs, path string, res any) {
	file := path
	if rel, err := filepath.Rel(rootAbs, path); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
		file = filepath.ToSlash(rel)
	}
	spec.RelocateFile(res, file)
}
