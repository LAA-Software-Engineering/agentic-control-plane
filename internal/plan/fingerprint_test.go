package plan

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state/sqlite"
)

func TestDeploymentStateFingerprint_emptyStore(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "fp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	fp, err := DeploymentStateFingerprint(ctx, st, "dev", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if fp == "" {
		t.Fatal("empty fingerprint")
	}
	fp2, err := DeploymentStateFingerprint(ctx, st, "dev", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if fp != fp2 {
		t.Fatalf("not stable: %q vs %q", fp, fp2)
	}
}

func TestDeploymentStateFingerprint_changesWhenRowAdded(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "fp2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	before, err := DeploymentStateFingerprint(ctx, st, "dev", "acme")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.UpsertAppliedResource(ctx, state.AppliedResource{
		Kind: spec.KindProject, Name: "acme", Env: "dev",
		SpecHash: "abc", NormalizedSpecJSON: `{}`, AppliedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAppliedProject(ctx, state.AppliedProject{
		ProjectName: "acme", Env: "dev", Version: "v1", AppliedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	after, err := DeploymentStateFingerprint(ctx, st, "dev", "acme")
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("fingerprint should change after upsert")
	}
}
