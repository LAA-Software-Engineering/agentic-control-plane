package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/config"
	"github.com/LAA-Software-Engineering/terfyn/internal/deploy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

// artifactStore returns the store as a [state.ArtifactStore] when the backend supports deployment
// snapshots (issue #207). In-memory or partial stores that do not implement it disable snapshot
// pinning, so resume falls back to current config for those (unchanged pre-#207 behavior).
func (r *Runtime) artifactStore() (state.ArtifactStore, bool) {
	if r == nil {
		return nil, false
	}
	as, ok := r.Store.(state.ArtifactStore)
	return as, ok
}

// prepareForResume builds execution state for a resume. When the run pins a deployment snapshot
// (#207) and the backend retains artifacts, it hydrates the graph from that snapshot so the run
// resumes under the exact configuration and authority it started with — never re-resolved current
// config (ADR 001 already forbids the runtime from re-reading user YAML). pinned reports whether the
// pinned path was taken; the caller then skips input re-validation, which would re-read possibly
// changed schema files. Runs created before #207 (empty digest) fall back to cfg.
func (r *Runtime) prepareForResume(ctx context.Context, run *state.Run, cfg *config.ResolvedConfig) (prep *preparedProject, pinned bool, err error) {
	digest := ""
	if run != nil {
		digest = strings.TrimSpace(run.DeploymentSnapshotDigest)
	}
	store, ok := r.artifactStore()
	if digest == "" || !ok {
		p, perr := r.prepareFromConfig(ctx, cfg)
		return p, false, perr
	}
	h, err := deploy.HydrateGraph(ctx, store, digest)
	if err != nil {
		return nil, false, err
	}
	root := ""
	if cfg != nil {
		root = cfg.ProjectRoot()
	}
	return &preparedProject{root: root, graph: h.Graph, pinned: true}, true, nil
}

// pinDeploymentSnapshot builds and persists the deployment snapshot for graph and returns its
// digest, so the run row can pin the exact configuration and authority it starts under. Returns an
// empty digest (and no error) when the backend does not support artifacts.
func (r *Runtime) pinDeploymentSnapshot(ctx context.Context, graph *spec.ProjectGraph, env string) (digest string, warnings []string, err error) {
	store, ok := r.artifactStore()
	if !ok {
		return "", nil, nil
	}
	digest, warnings, err = deploy.BuildAndPersist(ctx, store, graph, env, r.agentVersion())
	if err != nil {
		return "", nil, fmt.Errorf("local: pin deployment snapshot: %w", err)
	}
	return digest, warnings, nil
}
