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

func TestSnapshot_schemaBundleDigestRoundTrips(t *testing.T) {
	st, ctx := openTestStore(t)
	in := state.DeploymentSnapshot{
		Digest: "s1", FormatVersion: "v1", Environment: "local", GraphDigest: "g1",
		CapabilityManifestDigest: "m1", SchemaBundleDigest: "sb1",
	}
	if err := st.PutSnapshot(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSnapshot(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SchemaBundleDigest != "sb1" {
		t.Fatalf("schema_bundle_digest = %q, want sb1", got.SchemaBundleDigest)
	}
}

func TestCurrentSnapshot_pointerFollowsLastApplyIncludingRollback(t *testing.T) {
	st, ctx := openTestStore(t)
	// Content-addressed snapshot rows are immutable; the current pointer is a separate mutable row.
	for _, d := range []string{"A", "B"} {
		if err := st.PutSnapshot(ctx, state.DeploymentSnapshot{Digest: d, FormatVersion: "v1", Environment: "prod", GraphDigest: "g" + d}); err != nil {
			t.Fatal(err)
		}
	}
	mustCurrent := func(want string) {
		t.Helper()
		got, err := st.CurrentSnapshotDigestForEnv(ctx, "prod")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("current for prod = %q, want %q", got, want)
		}
	}
	// apply A -> B -> A (rollback). "latest by created_at" would wrongly report B after rollback;
	// the pointer must report A.
	if err := st.SetCurrentSnapshot(ctx, "prod", "A"); err != nil {
		t.Fatal(err)
	}
	mustCurrent("A")
	if err := st.SetCurrentSnapshot(ctx, "prod", "B"); err != nil {
		t.Fatal(err)
	}
	mustCurrent("B")
	if err := st.SetCurrentSnapshot(ctx, "prod", "A"); err != nil { // re-apply earlier digest
		t.Fatal(err)
	}
	mustCurrent("A")
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
	// A current-deployed snapshot that NO run references (e.g. applied, not yet run) must also
	// survive — the env identity superseded detection depends on.
	current := state.DeploymentSnapshot{Digest: "current", FormatVersion: "v1", Environment: "local", GraphDigest: "graphCurrent", CreatedAt: time.Now()}
	if err := st.PutSnapshot(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := st.PutArtifact(ctx, state.DeploymentArtifact{Digest: "graphCurrent", Kind: state.ArtifactKindResolvedGraph, FormatVersion: "v1", Payload: []byte("graphCurrent"), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCurrentSnapshot(ctx, "local", "current"); err != nil {
		t.Fatal(err)
	}
	// A surviving run references only "keep".
	if err := st.StartRun(ctx, state.Run{RunID: "r1", WorkflowName: "wf", Env: "local", Status: state.RunStatusInterrupted, StartedAt: time.Now(), DeploymentSnapshotDigest: "keep"}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.PruneUnreferencedArtifacts(ctx); err != nil {
		t.Fatal(err)
	}

	// The run-referenced snapshot and the current-env pointer snapshot survive with their artifacts;
	// the orphan and its artifact are gone.
	for _, keep := range []string{"keep", "current"} {
		if _, err := st.GetSnapshot(ctx, keep); err != nil {
			t.Fatalf("snapshot %q must survive prune: %v", keep, err)
		}
	}
	for _, keep := range []string{"graphKeep", "graphCurrent"} {
		if _, err := st.GetArtifact(ctx, keep); err != nil {
			t.Fatalf("artifact %q must survive prune: %v", keep, err)
		}
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
