package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// preparedProject is a loaded, normalized, environment-overlaid, validated project graph.
type preparedProject struct {
	root  string
	graph *spec.ProjectGraph
}

// prepareProject resolves configuration (user-local, project, environment), validates, and prunes old runs.
func (r *Runtime) prepareProject(ctx context.Context, environmentName string) (*preparedProject, error) {
	root := strings.TrimSpace(r.ProjectRoot)
	if root == "" {
		return nil, fmt.Errorf("local: empty project root")
	}
	rc, err := config.Resolve(config.ResolveOptions{
		ProjectRoot: root,
		Env:         environmentName,
	})
	if err != nil {
		return nil, fmt.Errorf("local: resolve config: %w", err)
	}
	graph := rc.Graph()
	if n := spec.TraceRetentionDays(graph); n > 0 {
		cutoff := r.now().UTC().AddDate(0, 0, -n)
		if _, err := r.Store.DeleteRunsStartedBefore(ctx, cutoff); err != nil {
			return nil, fmt.Errorf("local: prune trace runs: %w", err)
		}
	}
	return &preparedProject{root: rc.ProjectRoot(), graph: graph}, nil
}
