package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// preparedProject is a loaded, normalized, environment-overlaid, validated project graph.
type preparedProject struct {
	root  string
	graph *spec.ProjectGraph
}

// prepareProject loads the project, applies environment overrides, validates, and prunes old runs.
func (r *Runtime) prepareProject(ctx context.Context, environmentName string) (*preparedProject, error) {
	root := strings.TrimSpace(r.ProjectRoot)
	if root == "" {
		return nil, fmt.Errorf("local: empty project root")
	}
	graph, err := project.LoadProject(root)
	if err != nil {
		return nil, fmt.Errorf("local: load project: %w", err)
	}
	spec.NormalizeProjectGraph(graph)
	graph, err = ApplyEnvironment(graph, environmentName)
	if err != nil {
		return nil, err
	}
	if err := spec.ValidateProjectGraph(graph, root); err != nil {
		return nil, fmt.Errorf("local: validate project: %w", err)
	}
	if n := spec.TraceRetentionDays(graph); n > 0 {
		cutoff := r.now().UTC().AddDate(0, 0, -n)
		if _, err := r.Store.DeleteRunsStartedBefore(ctx, cutoff); err != nil {
			return nil, fmt.Errorf("local: prune trace runs: %w", err)
		}
	}
	return &preparedProject{root: root, graph: graph}, nil
}
