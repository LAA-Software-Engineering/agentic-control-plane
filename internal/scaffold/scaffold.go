package scaffold

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ErrResourceExists is returned when a resource file or name already exists in the project.
var ErrResourceExists = errors.New("scaffold: resource already exists")

// ErrInvalidName is returned when a resource name is empty or not a single path segment.
var ErrInvalidName = errors.New("scaffold: invalid resource name")

// ResourceKind identifies scaffolded resource types.
type ResourceKind string

const (
	KindTool     ResourceKind = "tool"
	KindPolicy   ResourceKind = "policy"
	KindWorkflow ResourceKind = "workflow"
	KindAgent    ResourceKind = "agent"
)

// Options configures a scaffold operation.
type Options struct {
	ProjectRoot string
	DryRun      bool
	// testFailAfter injects a commit failure after N successful renames when non-nil (tests only).
	testFailAfter *int
}

// Plan describes files that would be written without mutating the project.
type Plan struct {
	ResourceKind   ResourceKind
	ResourceName   string
	ResourcePath   string
	ImportPath     string
	ResourceYAML   []byte
	ProjectPath    string
	ProjectBefore  []byte
	ProjectAfter   []byte
	ImportAppended bool
}

// GenerateTool plans a new Tool resource and project.yaml import update.
func GenerateTool(opts Options, name, kind string) (*Plan, error) {
	return generate(opts, KindTool, name, func(defaultPolicy string) ([]byte, error) {
		return renderToolYAML(name, kind)
	})
}

// GeneratePolicy plans a new Policy resource with a built-in preset base.
func GeneratePolicy(opts Options, name, preset string) (*Plan, error) {
	return generate(opts, KindPolicy, name, func(defaultPolicy string) ([]byte, error) {
		return renderPolicyYAML(name, preset)
	})
}

// GenerateWorkflow plans a new Workflow resource using the project default policy when set.
func GenerateWorkflow(opts Options, name string) (*Plan, error) {
	return generate(opts, KindWorkflow, name, func(defaultPolicy string) ([]byte, error) {
		return renderWorkflowYAML(name, defaultPolicy), nil
	})
}

// GenerateAgent plans a new Agent resource.
func GenerateAgent(opts Options, name string) (*Plan, error) {
	return generate(opts, KindAgent, name, func(defaultPolicy string) ([]byte, error) {
		return renderAgentYAML(name), nil
	})
}

// Apply writes the planned files atomically. No-op when DryRun is set on the originating Options.
func Apply(plan *Plan, opts Options) error {
	if plan == nil {
		return fmt.Errorf("scaffold: nil plan")
	}
	if opts.DryRun {
		return nil
	}
	root, err := absProjectRoot(opts.ProjectRoot)
	if err != nil {
		return err
	}

	edits := []fileEdit{{path: plan.ResourcePath, content: plan.ResourceYAML}}
	if plan.ImportAppended {
		edits = append(edits, fileEdit{path: plan.ProjectPath, content: plan.ProjectAfter})
	}

	failAfter := -1
	if opts.testFailAfter != nil {
		failAfter = *opts.testFailAfter
	}
	c := &committer{root: root, failAfter: failAfter}
	return c.commitFiles(edits)
}

// ValidateResourceName checks that name is a non-empty single path segment.
func ValidateResourceName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("%w: name is empty", ErrInvalidName)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: invalid name %q", ErrInvalidName, name)
	}
	if strings.ContainsAny(name, `/\`) || filepath.Base(name) != name {
		return fmt.Errorf("%w: name must be a single path segment (no slashes)", ErrInvalidName)
	}
	return nil
}

func generate(opts Options, kind ResourceKind, name string, render func(defaultPolicy string) ([]byte, error)) (*Plan, error) {
	if err := ValidateResourceName(name); err != nil {
		return nil, err
	}
	root, err := absProjectRoot(opts.ProjectRoot)
	if err != nil {
		return nil, err
	}

	graph, err := project.LoadProject(root)
	if err != nil {
		return nil, fmt.Errorf("scaffold: load project: %w", err)
	}
	if err := assertNameAvailable(graph, kind, name); err != nil {
		return nil, err
	}

	defaultPolicy := "default"
	if graph.Spec.Defaults != nil && strings.TrimSpace(graph.Spec.Defaults.Policy) != "" {
		defaultPolicy = strings.TrimSpace(graph.Spec.Defaults.Policy)
	}

	resourceYAML, err := render(defaultPolicy)
	if err != nil {
		return nil, err
	}

	relDir, importRel := resourcePaths(kind, name)
	resourcePath := filepath.Join(root, relDir, name+".yaml")
	if _, err := os.Stat(resourcePath); err == nil {
		return nil, fmt.Errorf("%w: %s", ErrResourceExists, filepath.Join(relDir, name+".yaml"))
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("scaffold: stat resource: %w", err)
	}

	projPath, err := project.FindProjectFile(root)
	if err != nil {
		return nil, err
	}
	projBefore, err := os.ReadFile(projPath)
	if err != nil {
		return nil, fmt.Errorf("scaffold: read project.yaml: %w", err)
	}

	projAfter, appended, err := appendProjectImport(projBefore, importRel)
	if err != nil {
		return nil, err
	}

	return &Plan{
		ResourceKind:   kind,
		ResourceName:   name,
		ResourcePath:   resourcePath,
		ImportPath:     importRel,
		ResourceYAML:   resourceYAML,
		ProjectPath:    projPath,
		ProjectBefore:  projBefore,
		ProjectAfter:   projAfter,
		ImportAppended: appended,
	}, nil
}

func assertNameAvailable(g *spec.ProjectGraph, kind ResourceKind, name string) error {
	if g == nil {
		return nil
	}
	switch kind {
	case KindTool:
		if _, ok := g.Tools[name]; ok {
			return fmt.Errorf("%w: Tool/%s", ErrResourceExists, name)
		}
	case KindPolicy:
		if _, ok := g.Policies[name]; ok {
			return fmt.Errorf("%w: Policy/%s", ErrResourceExists, name)
		}
	case KindWorkflow:
		if _, ok := g.Workflows[name]; ok {
			return fmt.Errorf("%w: Workflow/%s", ErrResourceExists, name)
		}
	case KindAgent:
		if _, ok := g.Agents[name]; ok {
			return fmt.Errorf("%w: Agent/%s", ErrResourceExists, name)
		}
	}
	return nil
}

func resourcePaths(kind ResourceKind, name string) (dir string, importRel string) {
	switch kind {
	case KindTool:
		dir = "tools"
	case KindPolicy:
		dir = "policies"
	case KindWorkflow:
		dir = "workflows"
	case KindAgent:
		dir = "agents"
	default:
		dir = "resources"
	}
	importRel = "./" + dir + "/" + name + ".yaml"
	return dir, importRel
}

func absProjectRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("scaffold: empty project root")
	}
	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("scaffold: project root: %w", err)
	}
	return abs, nil
}
