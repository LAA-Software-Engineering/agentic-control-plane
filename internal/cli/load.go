package cli

import (
	"fmt"
	"path/filepath"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime/local"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// prepareProjectGraph loads the project from disk, applies defaults and optional environment
// overlays, and validates. projectRoot is the directory containing project.yaml (typically
// [Global.ProjectRoot]).
func prepareProjectGraph(projectRoot string, g *Global) (*spec.ProjectGraph, string, error) {
	root, err := filepath.Abs(filepath.Clean(projectRoot))
	if err != nil {
		return nil, "", fmt.Errorf("project root: %w", err)
	}
	graph, err := project.LoadProject(root)
	if err != nil {
		return nil, root, err
	}
	spec.NormalizeProjectGraph(graph)
	graph, err = local.ApplyEnvironment(graph, g.Env)
	if err != nil {
		return nil, root, err
	}
	if err := spec.ValidateProjectGraph(graph, root); err != nil {
		return nil, root, err
	}
	return graph, root, nil
}
