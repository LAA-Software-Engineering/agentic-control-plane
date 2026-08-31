package deploy

import (
	"context"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

func sampleExecutables() map[string]*execir.Program {
	return map[string]*execir.Program{
		"wf": {Workflow: "wf", Params: []string{"input"}, Body: []execir.Node{
			&execir.InvokeTool{Bind: "a", Uses: "tool.t.op", Args: map[string]execir.Value{"n": execir.Lit{V: int64(5)}}},
			&execir.Return{Value: execir.Ref{Path: []string{"a"}}},
		}},
	}
}

// TestBuild_PinsAndHydratesExecutables proves the pinned program round-trips
// through Build → Persist → HydrateGraph with a non-empty ExecutionIRDigest and
// an identical program digest (issue #260).
func TestBuild_PinsAndHydratesExecutables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := graphWithPolicy(nil)
	execs := sampleExecutables()

	b, err := Build(g, "local", "v1", nil, execs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if b.Snapshot.ExecutionIRDigest == "" {
		t.Fatalf("expected a non-empty ExecutionIRDigest when programs are pinned")
	}
	var hasExecArtifact bool
	for _, a := range b.Artifacts {
		if a.Kind == state.ArtifactKindExecutionIR {
			hasExecArtifact = true
			if a.FormatVersion != FormatExecutionIRV1 {
				t.Fatalf("execution_ir artifact format = %q", a.FormatVersion)
			}
		}
	}
	if !hasExecArtifact {
		t.Fatalf("expected an execution_ir artifact")
	}

	store := newMemStore()
	digest, err := Persist(ctx, store, b)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	h, err := HydrateGraph(ctx, store, digest)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	if len(h.Executables) != 1 || h.Executables["wf"] == nil {
		t.Fatalf("hydrated executables = %#v", h.Executables)
	}
	if got, want := h.Executables["wf"].Digest(), execs["wf"].Digest(); got != want {
		t.Fatalf("hydrated program digest mismatch:\n want %s\n got  %s", want, got)
	}
}

// TestBuild_ExecLessSnapshotUnchanged proves a project with no programs keeps an
// empty ExecutionIRDigest, so its snapshot digest is byte-identical to before
// #260 (the identity field was always "").
func TestBuild_ExecLessSnapshotUnchanged(t *testing.T) {
	t.Parallel()
	g := graphWithPolicy(nil)
	withNil, err := Build(g, "local", "v1", nil, nil)
	if err != nil {
		t.Fatalf("build nil: %v", err)
	}
	withEmpty, err := Build(g, "local", "v1", nil, map[string]*execir.Program{})
	if err != nil {
		t.Fatalf("build empty: %v", err)
	}
	if withNil.Snapshot.ExecutionIRDigest != "" || withEmpty.Snapshot.ExecutionIRDigest != "" {
		t.Fatalf("exec-less ExecutionIRDigest must be empty")
	}
	if withNil.Snapshot.Digest != withEmpty.Snapshot.Digest {
		t.Fatalf("nil vs empty execs must yield the same snapshot digest")
	}
	// Pinning programs must change the snapshot digest (they are execution identity).
	withProgs, err := Build(g, "local", "v1", nil, sampleExecutables())
	if err != nil {
		t.Fatalf("build progs: %v", err)
	}
	if withProgs.Snapshot.Digest == withNil.Snapshot.Digest {
		t.Fatalf("pinning a program must change the snapshot digest")
	}
}

// TestHydrate_UnknownExecFormatRejected proves an unknown execution_ir format
// fails resume loudly (S8) rather than being reinterpreted.
func TestHydrate_UnknownExecFormatRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	g := graphWithPolicy(nil)
	b, err := Build(g, "local", "v1", nil, sampleExecutables())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Corrupt the execution_ir artifact's format version before persisting.
	for i := range b.Artifacts {
		if b.Artifacts[i].Kind == state.ArtifactKindExecutionIR {
			b.Artifacts[i].FormatVersion = "agentic.dev/executionir/v99"
		}
	}
	store := newMemStore()
	digest, err := Persist(ctx, store, b)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if _, err := HydrateGraph(ctx, store, digest); err == nil {
		t.Fatalf("expected an unsupported-format error hydrating a bad execution_ir artifact")
	}
}
