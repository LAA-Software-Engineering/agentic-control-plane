package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupHitlExecutorWithTelemetry(t *testing.T) (*Executor, *tracetest.SpanRecorder, string, time.Time) {
	t.Helper()
	ex, _, runID, started := setupHitlExecutor(t)
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	ex.Telemetry = telemetry.NewTracerWithProvider(tp, "test-1.0")
	return ex, sr, runID, started
}

func TestTelemetry_hitlInterruptRootSpanNotError(t *testing.T) {
	ex, sr, runID, started := setupHitlExecutorWithTelemetry(t)
	ctx := context.Background()

	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("first run: %v", err)
	}
	ex.Telemetry.Shutdown()

	runSpans := endedSpansNamed(sr, telemetry.SpanAgentRun)
	if len(runSpans) != 1 {
		t.Fatalf("agent.run count = %d, want 1", len(runSpans))
	}
	sp := runSpans[0]
	if sp.Status().Code == codes.Error {
		t.Fatalf("interrupted agent.run should not be Error, got %v", sp.Status())
	}
	if !attrBool(sp, telemetry.AttrHitlInterrupted) {
		t.Fatalf("missing %s on interrupt span", telemetry.AttrHitlInterrupted)
	}
	if got := attrString(sp, telemetry.AttrResponseStatus); got != telemetry.StatusOK {
		t.Fatalf("%s = %q, want %q", telemetry.AttrResponseStatus, got, telemetry.StatusOK)
	}

	approvals := endedSpansNamed(sr, telemetry.SpanApproval)
	if len(approvals) != 1 {
		t.Fatalf("approval span count = %d, want 1", len(approvals))
	}
}

func TestTelemetry_runSpanCarriesAttribution(t *testing.T) {
	ex, sr, runID, started := setupHitlExecutorWithTelemetry(t)
	ctx := context.Background()

	err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		TenantID: "acme", ThreadID: "thread-1", ActorID: "alice", RequestID: "req-42",
	})
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("run: %v", err)
	}
	ex.Telemetry.Shutdown()

	runSpans := endedSpansNamed(sr, telemetry.SpanAgentRun)
	if len(runSpans) != 1 {
		t.Fatalf("agent.run count = %d", len(runSpans))
	}
	sp := runSpans[0]
	if got := attrString(sp, telemetry.AttrTenantID); got != "acme" {
		t.Fatalf("tenant = %q", got)
	}
	if got := attrString(sp, telemetry.AttrThreadID); got != "thread-1" {
		t.Fatalf("thread = %q", got)
	}
	if got := attrString(sp, telemetry.AttrActorID); got != "alice" {
		t.Fatalf("actor = %q", got)
	}
	if got := attrString(sp, telemetry.AttrRequestID); got != "req-42" {
		t.Fatalf("request = %q", got)
	}
}

func TestTelemetry_hitlResumeLinksPriorRunSpan(t *testing.T) {
	ex, sr, runID, started := setupHitlExecutorWithTelemetry(t)
	ctx := context.Background()

	if err := ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
	}); !errors.Is(err, ErrInterrupted) {
		t.Fatalf("interrupt: %v", err)
	}

	cp, err := ex.Store.GetLatestCheckpoint(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OtelInterrupt *telemetry.SpanRef `json:"otelInterrupt"`
	}
	if err := json.Unmarshal([]byte(cp.ContextJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.OtelInterrupt == nil || payload.OtelInterrupt.TraceID == "" {
		t.Fatal("checkpoint missing otelInterrupt ref")
	}
	interruptTraceID := payload.OtelInterrupt.TraceID

	sr.Reset()
	err = ex.Run(ctx, RunInput{
		RunID: runID, WorkflowName: "hitl", Env: "local", StartedAt: started, Input: map[string]any{},
		Resume:   true,
		TenantID: "persisted-tenant", ThreadID: "persisted-thread", ActorID: "persisted-actor", RequestID: "persisted-req",
		Hitl: HitlRunOptions{
			Decision: &policy.HitlDecisionInput{Kind: spec.HitlDecisionApprove, Actor: "alice"},
		},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	ex.Telemetry.Shutdown()

	runSpans := endedSpansNamed(sr, telemetry.SpanAgentRun)
	if len(runSpans) != 1 {
		t.Fatalf("resume agent.run count = %d, want 1", len(runSpans))
	}
	resume := runSpans[0]
	if !attrBool(resume, telemetry.AttrHitlResumed) {
		t.Fatalf("missing %s on resume span", telemetry.AttrHitlResumed)
	}
	if got := attrString(resume, telemetry.AttrTenantID); got != "persisted-tenant" {
		t.Fatalf("resume tenant = %q", got)
	}
	if got := attrString(resume, telemetry.AttrThreadID); got != "persisted-thread" {
		t.Fatalf("resume thread = %q", got)
	}
	if got := attrString(resume, telemetry.AttrActorID); got != "persisted-actor" {
		t.Fatalf("resume actor = %q", got)
	}
	if got := attrString(resume, telemetry.AttrRequestID); got != "persisted-req" {
		t.Fatalf("resume request = %q", got)
	}
	if resume.Status().Code == codes.Error {
		t.Fatal("resumed agent.run should not be Error")
	}
	links := resume.Links()
	if len(links) == 0 {
		t.Fatal("resume span missing link to interrupted run")
	}
	if links[0].SpanContext.TraceID().String() != interruptTraceID {
		t.Fatalf("link trace id = %s, want %s", links[0].SpanContext.TraceID(), interruptTraceID)
	}

	toolSpans := endedSpansNamed(sr, telemetry.SpanToolExec)
	if len(toolSpans) != 1 {
		t.Fatalf("tool.exec count = %d, want 1", len(toolSpans))
	}
	if !attrBool(toolSpans[0], telemetry.AttrToolRequiresApproval) {
		t.Fatalf("tool span missing %s", telemetry.AttrToolRequiresApproval)
	}
}

func endedSpansNamed(sr *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, sp := range sr.Ended() {
		if sp.Name() == name {
			out = append(out, sp)
		}
	}
	return out
}

func attrString(sp sdktrace.ReadOnlySpan, key string) string {
	for _, a := range sp.Attributes() {
		if string(a.Key) == key {
			return a.Value.AsString()
		}
	}
	return ""
}

func attrBool(sp sdktrace.ReadOnlySpan, key string) bool {
	for _, a := range sp.Attributes() {
		if string(a.Key) == key {
			return a.Value.AsBool()
		}
	}
	return false
}
