package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

func openTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	st, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, ctx
}

func TestArtifact_putGetDedupe(t *testing.T) {
	st, ctx := openTestStore(t)
	a := state.DeploymentArtifact{Digest: "d1", Kind: state.ArtifactKindResolvedGraph, FormatVersion: "v1", Payload: []byte("payload-A"), CreatedAt: time.Now()}
	if err := st.PutArtifact(ctx, a); err != nil {
		t.Fatal(err)
	}
	// Re-put with a different payload but the same digest must NOT overwrite (content-addressed).
	if err := st.PutArtifact(ctx, state.DeploymentArtifact{Digest: "d1", Kind: a.Kind, FormatVersion: "v1", Payload: []byte("payload-B"), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetArtifact(ctx, "d1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Payload) != "payload-A" {
		t.Fatalf("immutable artifact was overwritten: %q", got.Payload)
	}
}

func TestSnapshot_latestForEnv(t *testing.T) {
	st, ctx := openTestStore(t)
	old := state.DeploymentSnapshot{Digest: "s1", FormatVersion: "v1", Environment: "prod", GraphDigest: "g1", CreatedAt: time.Now().Add(-time.Hour)}
	newer := state.DeploymentSnapshot{Digest: "s2", FormatVersion: "v1", Environment: "prod", GraphDigest: "g2", CreatedAt: time.Now()}
	other := state.DeploymentSnapshot{Digest: "s3", FormatVersion: "v1", Environment: "dev", GraphDigest: "g3", CreatedAt: time.Now()}
	for _, s := range []state.DeploymentSnapshot{old, newer, other} {
		if err := st.PutSnapshot(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := st.LatestSnapshotDigestForEnv(ctx, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if latest != "s2" {
		t.Fatalf("latest for prod = %q, want s2", latest)
	}
}

func TestPruneUnreferencedArtifacts_referencedSurvive(t *testing.T) {
	st, ctx := openTestStore(t)

	// A referenced snapshot: a run pins it, and it references a graph artifact.
	referenced := state.DeploymentSnapshot{Digest: "keep", FormatVersion: "v1", Environment: "local", GraphDigest: "graphKeep", CreatedAt: time.Now()}
	orphan := state.DeploymentSnapshot{Digest: "orphan", FormatVersion: "v1", Environment: "local", GraphDigest: "graphOrphan", CreatedAt: time.Now()}
	for _, s := range []state.DeploymentSnapshot{referenced, orphan} {
		if err := st.PutSnapshot(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"graphKeep", "graphOrphan"} {
		if err := st.PutArtifact(ctx, state.DeploymentArtifact{Digest: d, Kind: state.ArtifactKindResolvedGraph, FormatVersion: "v1", Payload: []byte(d), CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	// A surviving run references only "keep".
	if err := st.StartRun(ctx, state.Run{RunID: "r1", WorkflowName: "wf", Env: "local", Status: state.RunStatusInterrupted, StartedAt: time.Now(), DeploymentSnapshotDigest: "keep"}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.PruneUnreferencedArtifacts(ctx); err != nil {
		t.Fatal(err)
	}

	// The referenced snapshot and its artifact survive; the orphan and its artifact are gone.
	if _, err := st.GetSnapshot(ctx, "keep"); err != nil {
		t.Fatalf("referenced snapshot must survive prune: %v", err)
	}
	if _, err := st.GetArtifact(ctx, "graphKeep"); err != nil {
		t.Fatalf("artifact of a referenced snapshot must survive prune: %v", err)
	}
	if _, err := st.GetSnapshot(ctx, "orphan"); err == nil {
		t.Fatal("unreferenced snapshot should have been pruned")
	}
	if _, err := st.GetArtifact(ctx, "graphOrphan"); err == nil {
		t.Fatal("artifact of an unreferenced snapshot should have been pruned")
	}
}

func TestRun_deploymentSnapshotDigestRoundTrips(t *testing.T) {
	st, ctx := openTestStore(t)
	if err := st.StartRun(ctx, state.Run{RunID: "r1", WorkflowName: "wf", Env: "local", Status: state.RunStatusRunning, StartedAt: time.Now(), DeploymentSnapshotDigest: "snap-xyz"}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetRun(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.DeploymentSnapshotDigest != "snap-xyz" {
		t.Fatalf("snapshot digest = %q, want snap-xyz", got.DeploymentSnapshotDigest)
	}
}
