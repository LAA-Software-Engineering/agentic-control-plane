package engine

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/execir"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang"
	"github.com/LAA-Software-Engineering/terfyn/internal/lang/lower"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// flagshipProgram lowers the ImplementAndReview workflow from the checked-in
// examples/implement-review-loop/main.agent to its execution IR, so these tests
// exercise the ACTUAL example program (a bounded `while` around two agents), not a
// hand-built copy.
func flagshipProgram(t *testing.T) *execir.Program {
	t.Helper()
	path := filepath.Join("..", "..", "examples", "implement-review-loop", "main.agent")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	f, diags := lang.Parse(path, string(src))
	if diags.HasErrors() {
		t.Fatalf("parse example: %v", diags)
	}
	for _, d := range f.Decls {
		wd, ok := d.(*lang.WorkflowDecl)
		if !ok || wd.Name == nil || wd.Name.Name != "ImplementAndReview" {
			continue
		}
		prog, ld := lower.LowerExec(wd, nil)
		if ld.HasErrors() {
			t.Fatalf("lower example workflow: %v", ld)
		}
		return prog
	}
	t.Fatalf("ImplementAndReview workflow not found in the example")
	return nil
}

// reviewStub is an execir.Invoker standing in for the two agents: the Implementer
// echoes a working CodingState (never approved), and the Reviewer approves only on
// its Nth review, so the loop's `!state.approved` condition is driven by real agent
// output. It counts each agent invocation.
type reviewStub struct {
	mu               sync.Mutex
	approveOnReview  int // the Reviewer approves on this review number (1-based); 0 = never
	implementerCalls int
	reviewerCalls    int
}

func (s *reviewStub) InvokeTool(context.Context, execir.CallSite, string, map[string]any) (any, error) {
	return map[string]any{}, nil
}

func (s *reviewStub) InvokeAgent(_ context.Context, _ execir.CallSite, agent string, args map[string]any) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch agent {
	case "Implementer":
		s.implementerCalls++
		return map[string]any{"task": "t", "approved": false, "feedback": []any{}, "summary": "implemented"}, nil
	case "Reviewer":
		s.reviewerCalls++
		approved := s.approveOnReview > 0 && s.reviewerCalls >= s.approveOnReview
		fb := []any{}
		if !approved {
			fb = []any{"needs work"}
		}
		return map[string]any{"task": "t", "approved": approved, "feedback": fb, "summary": "reviewed"}, nil
	default:
		return map[string]any{}, nil
	}
}

func (s *reviewStub) InvokeWorkflow(context.Context, execir.CallSite, string, map[string]any) (any, error) {
	return nil, nil
}
func (s *reviewStub) InvokeApproval(_ context.Context, _ execir.CallSite, _ execir.ApprovalInfo, args map[string]any) (any, error) {
	return args, nil
}

func runFlagship(t *testing.T, stub *reviewStub) map[string]any {
	t.Helper()
	prog := flagshipProgram(t)
	in := &execir.Interp{Invoker: stub}
	out, err := in.Run(context.Background(), prog, map[string]any{"task": "t", "approved": false, "feedback": []any{}, "summary": ""})
	if err != nil {
		t.Fatalf("run flagship: %v", err)
	}
	m, _ := out.(map[string]any)
	return m
}

// TestImplementReviewLoop_ApprovedFirstPass: the Reviewer approves on the first
// review, so the loop exits immediately after one round.
func TestImplementReviewLoop_ApprovedFirstPass(t *testing.T) {
	t.Parallel()
	stub := &reviewStub{approveOnReview: 1}
	out := runFlagship(t, stub)
	if stub.implementerCalls != 1 || stub.reviewerCalls != 1 {
		t.Fatalf("want one round, got implementer=%d reviewer=%d", stub.implementerCalls, stub.reviewerCalls)
	}
	if out["approved"] != true {
		t.Fatalf("final state should be approved, got %v", out["approved"])
	}
}

// TestImplementReviewLoop_RejectThenApprove: the first review rejects, the second
// approves — exactly two rounds.
func TestImplementReviewLoop_RejectThenApprove(t *testing.T) {
	t.Parallel()
	stub := &reviewStub{approveOnReview: 2}
	out := runFlagship(t, stub)
	if stub.implementerCalls != 2 || stub.reviewerCalls != 2 {
		t.Fatalf("want two rounds, got implementer=%d reviewer=%d", stub.implementerCalls, stub.reviewerCalls)
	}
	if out["approved"] != true {
		t.Fatalf("final state should be approved after the second review, got %v", out["approved"])
	}
}

// TestImplementReviewLoop_CapsAtThreeAttempts: never approved — the bound stops the
// loop after exactly three reviews (no silent fourth attempt), returning the final
// un-approved state.
func TestImplementReviewLoop_CapsAtThreeAttempts(t *testing.T) {
	t.Parallel()
	stub := &reviewStub{approveOnReview: 0} // never approves
	out := runFlagship(t, stub)
	if stub.implementerCalls != 3 || stub.reviewerCalls != 3 {
		t.Fatalf("limit 3 must cap the loop, got implementer=%d reviewer=%d (want 3/3)", stub.implementerCalls, stub.reviewerCalls)
	}
	if out["approved"] != false {
		t.Fatalf("final state should still be un-approved, got %v", out["approved"])
	}
}

// TestImplementReviewLoop_ReviewerCannotWrite is the capability boundary: the
// Implementer may invoke write_file; the Reviewer may not — it holds only read_file
// and run_tests, so write_file is denied at tool resolution regardless of the prompt.
func TestImplementReviewLoop_ReviewerCannotWrite(t *testing.T) {
	t.Parallel()
	e := &Executor{Graph: &spec.ProjectGraph{Tools: map[string]*spec.ToolResource{
		"workspace": {Metadata: spec.Metadata{Name: "workspace"}, Spec: spec.ToolSpec{Type: "native"}},
	}}}
	implementer := &spec.AgentResource{Metadata: spec.Metadata{Name: "Implementer"}, Spec: spec.AgentSpec{
		Tools: []string{"tool.workspace.read_file", "tool.workspace.write_file", "tool.workspace.run_tests"},
	}}
	reviewer := &spec.AgentResource{Metadata: spec.Metadata{Name: "Reviewer"}, Spec: spec.AgentSpec{
		Tools: []string{"tool.workspace.read_file", "tool.workspace.run_tests"},
	}}

	_, implUses, err := e.advertisedAgentTools(implementer)
	if err != nil {
		t.Fatalf("implementer advertise: %v", err)
	}
	if uses, err := resolveAgentToolCall("workspace.write_file", implUses); err != nil || uses != "tool.workspace.write_file" {
		t.Fatalf("Implementer must be able to write, got uses=%q err=%v", uses, err)
	}

	_, revUses, err := e.advertisedAgentTools(reviewer)
	if err != nil {
		t.Fatalf("reviewer advertise: %v", err)
	}
	if _, err := resolveAgentToolCall("workspace.read_file", revUses); err != nil {
		t.Fatalf("Reviewer must be able to read, got %v", err)
	}
	if _, err := resolveAgentToolCall("workspace.write_file", revUses); err == nil {
		t.Fatalf("Reviewer write_file MUST be denied at the capability boundary")
	}
}
