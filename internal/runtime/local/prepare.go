package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// preparedProject holds the resolved graph snapshot for one execution.
type preparedProject struct {
	root  string
	graph *spec.ProjectGraph
}

// prepareFromConfig builds execution state from a resolved config snapshot.
// The runtime must not reload project YAML/TOML; cfg is the sole configuration source.
func (r *Runtime) prepareFromConfig(ctx context.Context, cfg *config.ResolvedConfig) (*preparedProject, error) {
	if cfg == nil {
		return nil, fmt.Errorf("local: nil resolved config")
	}
	graph := cfg.Graph()
	if graph == nil {
		return nil, fmt.Errorf("local: resolved config has no graph")
	}
	root := cfg.ProjectRoot()
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("local: empty project root in resolved config")
	}
	if n := spec.TraceRetentionDays(graph); n > 0 {
		cutoff := r.now().UTC().AddDate(0, 0, -n)
		if _, err := r.Store.DeleteRunsStartedBefore(ctx, cutoff); err != nil {
			return nil, fmt.Errorf("local: prune trace runs: %w", err)
		}
	}
	return &preparedProject{root: root, graph: graph}, nil
}
