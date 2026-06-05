package telemetry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
)

func TestNewTracer_disabledZeroCost(t *testing.T) {
	tr := telemetry.NewTracer(telemetry.Config{Enabled: false}, "1.0.0")
	if tr.Enabled() {
		t.Fatal("expected disabled")
	}
	h := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{RunID: "r1", Workflow: "wf"})
	if h != nil {
		t.Fatal("disabled tracer should not return handle")
	}
	tr.Shutdown()
}

func TestNewTracer_consoleExport_emitsRunSpan(t *testing.T) {
	tr := telemetry.NewTracer(telemetry.Config{
		Enabled:       true,
		ServiceName:   "test-svc",
		ConsoleExport: true,
	}, "0.1.0")
	if !tr.Enabled() {
		t.Fatal("expected enabled console tracer")
	}
	defer tr.Shutdown()

	ctx := context.Background()
	h := tr.BeginRun(ctx, telemetry.RunStartAttrs{
		RunID: "run-abc", Workflow: "demo", AgentName: "agent-a",
		TenantID: "acme", ThreadID: "thread-9", ActorID: "ci-bot", RequestID: "req-1",
	})
	if h == nil {
		t.Fatal("nil handle")
	}
	ref := h.SpanRef()
	if ref.TraceID == "" || ref.SpanID == "" {
		t.Fatalf("empty span ref: %+v", ref)
	}
	endModel, end := h.StartModel(telemetry.ModelAttrs{RunID: "run-abc", AgentName: "agent-a", ModelRef: "mock/x"})
	_ = endModel
	end(nil)
	h.End(nil)
}

func TestRunHandle_EndInterrupted_notErrorStatus(t *testing.T) {
	tr := telemetry.NewTracer(telemetry.Config{
		Enabled: true, ServiceName: "test-svc", ConsoleExport: true,
	}, "0.1.0")
	defer tr.Shutdown()

	h := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{RunID: "r", Workflow: "w"})
	h.MarkInterrupted()
	h.EndInterrupted()
}

func TestRunHandle_resumeLink(t *testing.T) {
	tr := telemetry.NewTracer(telemetry.Config{
		Enabled:       true,
		ServiceName:   "test-svc",
		ConsoleExport: true,
	}, "0.1.0")
	if !tr.Enabled() {
		t.Fatal("expected enabled")
	}
	defer tr.Shutdown()

	first := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{
		RunID: "run-1", Workflow: "wf",
	})
	ref := first.SpanRef()
	first.MarkInterrupted()
	endApproval := first.StartApproval(telemetry.ApprovalAttrs{RunID: "run-1", Uses: "tool.x.y"})
	endApproval()
	first.End(nil)

	second := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{
		RunID: "run-1", Workflow: "wf", Resume: true, LinkFrom: &ref,
	})
	if second == nil {
		t.Fatal("nil resume handle")
	}
	second.End(nil)
}

func TestRunHandle_toolSpanRecordsSafety(t *testing.T) {
	tr := telemetry.NewTracer(telemetry.Config{
		Enabled:       true,
		ServiceName:   "test-svc",
		ConsoleExport: true,
	}, "0.1.0")
	defer tr.Shutdown()

	h := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{RunID: "r", Workflow: "w"})
	_, end := h.StartTool(telemetry.ToolAttrs{
		RunID: "r", Uses: "tool.helper.echo",
		Trusted: false, SideEffects: true, RequiresApproval: true,
	})
	end(errors.New("boom"))
	h.End(errors.New("run failed"))
}

func TestSpanRef_roundTrip(t *testing.T) {
	tr := telemetry.NewTracer(telemetry.Config{
		Enabled: true, ServiceName: "t", ConsoleExport: true,
	}, "v")
	defer tr.Shutdown()
	h := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{RunID: "x", Workflow: "y"})
	ref := h.SpanRef()
	h2 := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{
		RunID: "x", Workflow: "y", Resume: true, LinkFrom: &ref,
	})
	if h2 == nil {
		t.Fatal("expected resume handle")
	}
	h2.End(nil)
}

func TestConfigFromGraph_nil(t *testing.T) {
	cfg := telemetry.ConfigFromGraph(nil)
	if cfg.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestRunHandle_EndInterrupted_okStatus(t *testing.T) {
	tr := telemetry.NewTracer(telemetry.Config{
		Enabled: true, ServiceName: "test-svc", ConsoleExport: true,
	}, "0.1.0")
	defer tr.Shutdown()

	h := tr.BeginRun(context.Background(), telemetry.RunStartAttrs{RunID: "r", Workflow: "w"})
	h.MarkInterrupted()
	h.EndInterrupted()
}
