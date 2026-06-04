package telemetry

import (
	"context"
	"log"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Tracer emits optional gen_ai OpenTelemetry spans. When disabled, all methods are no-ops.
type Tracer struct {
	enabled      bool
	agentVersion string
	provider     *sdktrace.TracerProvider
	tracer       oteltrace.Tracer
	mu           sync.Mutex
}

// NewTracer builds a tracer from project telemetry config. When cfg.Enabled is false, returns a
// disabled tracer with no exporters. On init failure, logs a warning and returns a disabled tracer
// so runs never fail because of telemetry.
func NewTracer(cfg Config, agentVersion string) *Tracer {
	if !cfg.Enabled {
		return &Tracer{enabled: false, agentVersion: agentVersion}
	}
	tp, err := newProvider(cfg, agentVersion)
	if err != nil {
		log.Printf("telemetry: disabled after init error: %v", err)
		return &Tracer{enabled: false, agentVersion: agentVersion}
	}
	return newTracerFromProvider(tp, agentVersion)
}

// NewTracerWithProvider returns a tracer backed by an existing SDK provider (tests only).
func NewTracerWithProvider(tp *sdktrace.TracerProvider, agentVersion string) *Tracer {
	if tp == nil {
		return &Tracer{enabled: false, agentVersion: agentVersion}
	}
	return newTracerFromProvider(tp, agentVersion)
}

func newTracerFromProvider(tp *sdktrace.TracerProvider, agentVersion string) *Tracer {
	return &Tracer{
		enabled:      true,
		agentVersion: agentVersion,
		provider:     tp,
		tracer:       tp.Tracer(SystemName),
	}
}

// Enabled reports whether OpenTelemetry export is active.
func (t *Tracer) Enabled() bool {
	return t != nil && t.enabled
}

// Shutdown flushes and closes exporters. Safe to call on disabled tracers.
func (t *Tracer) Shutdown() {
	if t == nil || !t.enabled {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.provider == nil {
		return
	}
	flushProvider(t.provider)
	t.provider = nil
	t.enabled = false
}

// SpanRef stores trace/span identifiers for resume span links across process restarts.
type SpanRef struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

// RunHandle tracks the agent.run root span for one engine execution.
type RunHandle struct {
	tracer *Tracer
	ctx    context.Context
	span   oteltrace.Span
}

// RunStartAttrs are gen_ai attributes for an agent.run span.
type RunStartAttrs struct {
	RunID     string
	Workflow  string
	AgentName string
	ActorID   string
	Resume    bool
	LinkFrom  *SpanRef
}

// BeginRun starts the agent.run root span. The returned handle must be ended with [RunHandle.End].
func (t *Tracer) BeginRun(ctx context.Context, attrs RunStartAttrs) *RunHandle {
	if t == nil || !t.enabled {
		return nil
	}
	base := []attribute.KeyValue{
		attribute.String(AttrSystem, SystemName),
		attribute.String(AttrOperationName, OpRun),
		attribute.String(AttrRunID, attrs.RunID),
		attribute.String(AttrWorkflow, attrs.Workflow),
		attribute.String(AttrAgentVersion, t.agentVersion),
	}
	if attrs.AgentName != "" {
		base = append(base, attribute.String(AttrAgentName, attrs.AgentName))
	}
	if attrs.ActorID != "" {
		base = append(base, attribute.String(AttrActorID, attrs.ActorID))
	}
	if attrs.Resume {
		base = append(base, attribute.Bool(AttrHitlResumed, true))
	}
	opts := []oteltrace.SpanStartOption{oteltrace.WithAttributes(base...)}
	if attrs.LinkFrom != nil {
		if sc, ok := spanContextFromRef(*attrs.LinkFrom); ok {
			opts = append(opts, oteltrace.WithLinks(oteltrace.Link{SpanContext: sc}))
		}
	}
	ctx, span := t.tracer.Start(ctx, SpanAgentRun, opts...)
	if sc := span.SpanContext(); sc.IsValid() {
		span.SetAttributes(attribute.String(AttrRequestTraceID, sc.TraceID().String()))
	}
	return &RunHandle{tracer: t, ctx: ctx, span: span}
}

// Context returns the context carrying the active run span.
func (h *RunHandle) Context() context.Context {
	if h == nil {
		return context.Background()
	}
	if h.ctx == nil {
		return context.Background()
	}
	return h.ctx
}

// SpanRef returns the current run span identifiers for checkpoint persistence.
func (h *RunHandle) SpanRef() SpanRef {
	if h == nil || h.span == nil {
		return SpanRef{}
	}
	sc := h.span.SpanContext()
	return SpanRef{TraceID: sc.TraceID().String(), SpanID: sc.SpanID().String()}
}

// End finishes the agent.run span with gen_ai.response.status.
func (h *RunHandle) End(err error) {
	if h == nil || h.span == nil {
		return
	}
	defer h.span.End()
	if err != nil {
		h.span.SetStatus(codes.Error, err.Error())
		h.span.SetAttributes(attribute.String(AttrResponseStatus, StatusError))
		return
	}
	h.span.SetStatus(codes.Ok, "")
	h.span.SetAttributes(attribute.String(AttrResponseStatus, StatusOK))
}

// EndInterrupted ends a paused run span without error status (HITL / checkpoint interrupt).
// Call when the engine returns [engine.ErrInterrupted]; gen_ai.hitl.interrupted should already be set.
func (h *RunHandle) EndInterrupted() {
	if h == nil || h.span == nil {
		return
	}
	defer h.span.End()
	h.span.SetStatus(codes.Ok, "")
	h.span.SetAttributes(attribute.String(AttrResponseStatus, StatusOK))
}

// MarkInterrupted tags the run span before pausing for HITL.
func (h *RunHandle) MarkInterrupted() {
	if h == nil || h.span == nil {
		return
	}
	h.span.SetAttributes(attribute.Bool(AttrHitlInterrupted, true))
}

// ModelAttrs configures a model.chat child span.
type ModelAttrs struct {
	RunID     string
	StepID    string
	AgentName string
	ModelRef  string
}

// StartModel starts a model.chat child span; call the returned end function when the call completes.
func (h *RunHandle) StartModel(attrs ModelAttrs) (context.Context, func(err error)) {
	if h == nil || h.tracer == nil || !h.tracer.enabled {
		return h.Context(), func(error) {}
	}
	ctx, span := h.tracer.tracer.Start(h.Context(), SpanModelChat,
		oteltrace.WithAttributes(modelAttrs(attrs)...),
	)
	span.SetAttributes(
		attribute.Bool(AttrResponseHasToolCalls, false),
		attribute.Int(AttrResponseToolCallCount, 0),
	)
	return ctx, func(err error) {
		defer span.End()
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(attribute.String(AttrResponseStatus, StatusError))
			return
		}
		span.SetAttributes(attribute.String(AttrResponseStatus, StatusOK))
	}
}

// ToolAttrs configures a tool.exec child span.
type ToolAttrs struct {
	RunID            string
	StepID           string
	Uses             string
	Trusted          bool
	SideEffects      bool
	RequiresApproval bool
}

// StartTool starts a tool.exec child span.
func (h *RunHandle) StartTool(attrs ToolAttrs) (context.Context, func(err error)) {
	if h == nil || h.tracer == nil || !h.tracer.enabled {
		return h.Context(), func(error) {}
	}
	kvs := []attribute.KeyValue{
		attribute.String(AttrSystem, SystemName),
		attribute.String(AttrOperationName, OpToolExec),
		attribute.String(AttrRunID, attrs.RunID),
		attribute.Bool(AttrToolTrusted, attrs.Trusted),
		attribute.Bool(AttrToolSideEffects, attrs.SideEffects),
		attribute.Bool(AttrToolRequiresApproval, attrs.RequiresApproval),
	}
	if attrs.StepID != "" {
		kvs = append(kvs, attribute.String(AttrStepID, attrs.StepID))
	}
	if attrs.Uses != "" {
		kvs = append(kvs, attribute.String(AttrToolName, attrs.Uses))
	}
	ctx, span := h.tracer.tracer.Start(h.Context(), SpanToolExec, oteltrace.WithAttributes(kvs...))
	return ctx, func(err error) {
		defer span.End()
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(attribute.String(AttrResponseStatus, StatusError))
			return
		}
		span.SetAttributes(attribute.String(AttrResponseStatus, StatusOK))
	}
}

// ApprovalAttrs configures an approval span when HITL gates a tool call.
type ApprovalAttrs struct {
	RunID string
	Uses  string
}

// StartApproval starts an approval child span.
func (h *RunHandle) StartApproval(attrs ApprovalAttrs) func() {
	if h == nil || h.tracer == nil || !h.tracer.enabled {
		return func() {}
	}
	_, span := h.tracer.tracer.Start(h.Context(), SpanApproval,
		oteltrace.WithAttributes(
			attribute.String(AttrSystem, SystemName),
			attribute.String(AttrOperationName, OpApproval),
			attribute.String(AttrRunID, attrs.RunID),
			attribute.String(AttrToolName, attrs.Uses),
		),
	)
	return func() { span.End() }
}

func modelAttrs(attrs ModelAttrs) []attribute.KeyValue {
	kvs := []attribute.KeyValue{
		attribute.String(AttrSystem, SystemName),
		attribute.String(AttrOperationName, OpModelChat),
		attribute.String(AttrRunID, attrs.RunID),
	}
	if attrs.StepID != "" {
		kvs = append(kvs, attribute.String(AttrStepID, attrs.StepID))
	}
	if attrs.AgentName != "" {
		kvs = append(kvs, attribute.String(AttrAgentName, attrs.AgentName))
	}
	if attrs.ModelRef != "" {
		kvs = append(kvs, attribute.String(AttrRequestModel, attrs.ModelRef))
	}
	return kvs
}

func spanContextFromRef(ref SpanRef) (oteltrace.SpanContext, bool) {
	tid, err := oteltrace.TraceIDFromHex(ref.TraceID)
	if err != nil {
		return oteltrace.SpanContext{}, false
	}
	sid, err := oteltrace.SpanIDFromHex(ref.SpanID)
	if err != nil {
		return oteltrace.SpanContext{}, false
	}
	return oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: tid,
		SpanID:  sid,
	}), true
}
