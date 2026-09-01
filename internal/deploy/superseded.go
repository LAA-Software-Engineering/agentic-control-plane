package deploy

import (
	"context"

	"github.com/Terfyn/terfyn/internal/state"
	"github.com/Terfyn/terfyn/internal/statejson"
)

// MarkSupersededRuns flags run records that are executing a deployment snapshot no longer current
// for their environment (issue #207): an apply has landed since the run started. It resolves the
// latest snapshot per distinct environment once, then delegates to [statejson.MarkSuperseded]. A
// nil store or a lookup error leaves records unmarked (advisory, never fatal).
func MarkSupersededRuns(ctx context.Context, store state.ArtifactStore, records []statejson.RunRecord) {
	if store == nil {
		return
	}
	latest := map[string]string{}
	for i := range records {
		if records[i].DeploymentSnapshotDigest == "" {
			continue
		}
		env := records[i].Env
		if _, seen := latest[env]; seen {
			continue
		}
		d, err := store.CurrentSnapshotDigestForEnv(ctx, env)
		if err != nil {
			d = ""
		}
		latest[env] = d
	}
	statejson.MarkSuperseded(records, latest)
}
