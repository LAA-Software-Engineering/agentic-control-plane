package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/execir"
	"github.com/Terfyn/terfyn/internal/spec"
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
	// executables is the pinned execution IR per workflow (issue #260): on a fresh run from the
	// resolved config, on a pinned resume from the hydrated snapshot. The execir run path executes
	// the pinned program rather than re-lowering.
	executables map[string]*execir.Program
}

// workflowExecDigest returns the pinned/lowered program digest for wfName, or ""
// when no program exists for it. It is the execution-IR component folded into the
// #118 run-start-vs-resume drift hash (#277), so run-start and resume commit to
// the same identity: the program a workflow will actually execute, not only its
// resource projection. Run-start reads it from the resolved config's executables;
// a pinned resume reads it from the hydrated snapshot's executables (the SAME
// program), so the pinned path never false-drifts.
func workflowExecDigest(executables map[string]*execir.Program, wfName string) string {
	if prog := executables[strings.TrimSpace(wfName)]; prog != nil {
		return prog.Digest()
	}
	return ""
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
	return &preparedProject{root: root, graph: graph, executables: cfg.Executables()}, nil
}
