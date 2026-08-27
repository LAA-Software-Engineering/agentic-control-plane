package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/config"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// preparedProject holds the resolved graph snapshot for one execution.
type preparedProject struct {
	root  string
	graph *spec.ProjectGraph
	// pinned marks that graph was hydrated from a run's deployment snapshot (issue #207), so the
	// engine must take its policy and schema authority from graph, not from files under root.
	pinned bool
	// schemas is the schema ref→content bundle captured in the pinned snapshot; the engine validates
	// against it on resume instead of re-reading files. Nil on fresh runs.
	schemas map[string]string
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
		// #207: after deleting old runs, GC deployment artifacts no surviving run references and
		// that are not the current env pointer, so retention does not grow artifacts unbounded. The
		// prune is reference-guarded, so it can never orphan an artifact a surviving run still needs
		// to resume (nor delete the current env identity superseded detection depends on).
		if as, ok := r.artifactStore(); ok {
			if _, err := as.PruneUnreferencedArtifacts(ctx); err != nil {
				return nil, fmt.Errorf("local: prune deployment artifacts: %w", err)
			}
		}
	}
	return &preparedProject{root: root, graph: graph}, nil
}
