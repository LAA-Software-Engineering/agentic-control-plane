package project

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// YAML file suffixes loaded from directories (recursive) and explicit import paths.
const yamlExt = ".yaml"
const ymlExt = ".yml"

// LoadProject loads root/project.yaml (or project.yml), expands spec.imports, parses
// every YAML document with [internal/spec], and merges resources into a ProjectGraph.
// Duplicate kind/metadata.name pairs are rejected (§9.1). Only the root project file
// may define kind Project.
func LoadProject(root string) (*spec.ProjectGraph, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("project root: %w", err)
	}

	projPath, err := findProjectFile(rootAbs)
	if err != nil {
		return nil, err
	}

	dec, err := spec.LoadResourceFile(projPath)
	if err != nil {
		return nil, err
	}
	pr, ok := dec.Resource.(*spec.ProjectResource)
	if !ok || dec.Kind() != spec.KindProject {
		return nil, fmt.Errorf("%s: expected kind Project, got %q", projPath, dec.Kind())
	}

	g := &spec.ProjectGraph{
		Meta:         pr.Metadata,
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
		return nil, err
	}

	for _, path := range files {
		if path == projPath {
			continue
		}
		d, err := spec.LoadResourceFile(path)
		if err != nil {
			return nil, err
		}
		if err := mergeDecoded(g, d, path, seen); err != nil {
			return nil, err
		}
	}

	return g, nil
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
		if !isUnderRoot(rootAbs, full) {
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

func isUnderRoot(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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
