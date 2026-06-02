package local

import (
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestBuildEngineHitlOptions_invalidKind(t *testing.T) {
	t.Helper()
	_, err := buildEngineHitlOptions(runtime.WorkflowRunOptions{
		HitlDecision: &runtime.HitlDecisionOptions{Kind: spec.HitlDecisionKind("maybe")},
	})
	if err == nil {
		t.Fatal("expected error for invalid decision kind")
	}
}

func TestBuildEngineHitlOptions_validDecision(t *testing.T) {
	t.Helper()
	out, err := buildEngineHitlOptions(runtime.WorkflowRunOptions{
		HitlDecision: &runtime.HitlDecisionOptions{Kind: spec.HitlDecisionApprove},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Decision == nil || out.Decision.Kind != spec.HitlDecisionApprove {
		t.Fatalf("decision = %+v", out.Decision)
	}
}
