package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Terfyn/terfyn/internal/models"
	"github.com/Terfyn/terfyn/internal/policy"
	"github.com/Terfyn/terfyn/internal/spec"
	"github.com/Terfyn/terfyn/internal/telemetry"
	"github.com/Terfyn/terfyn/internal/tools"
	"github.com/Terfyn/terfyn/internal/tools/native"
	"github.com/Terfyn/terfyn/internal/trace"
)

func parseAgentJSONObject(content string) (map[string]any, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("engine: empty agent response")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return nil, fmt.Errorf("engine: agent response is not a JSON object: %w", err)
	}
	return m, nil
}

func (e *Executor) runToolStep(ctx context.Context, runHandle *telemetry.RunHandle, pol policy.PolicyEvaluator, wf *spec.WorkflowResource, runID string, step spec.WorkflowStep, with map[string]any, pctx policy.RunContext, usesOverride string, withOverride map[string]any) (map[string]any, tools.ToolCallMeta, error) {
	uses := strings.TrimSpace(usesOverride)
	if uses == "" {
		uses = strings.TrimSpace(step.Uses)
	}
	withArgs := with
	if withOverride != nil {
		withArgs = withOverride
	}
	tid := e.qualID(step.ID)
	var err error
	withArgs, err = e.enforceToolInput(ctx, wf, runID, tid, uses, withArgs)
	if err != nil {
		return nil, tools.ToolCallMeta{}, err
	}
	if err := pol.CheckToolCall(ctx, policy.ToolCallContext{Run: pctx, StepID: step.ID, Uses: uses, With: withArgs}); err != nil {
		// Fail-closed: the tool is never invoked, so we skip tool_selection/tool_execution and
		// record system_error instead (same as workflow uses: steps).
		if e.Trace != nil {
			if d, ok := policy.AsDenied(err); ok {
				_, _ = e.Trace.Append(ctx, runID, tid, trace.EventSystemError, trace.ActorSystem, d.TraceData())
			}
		}
		return nil, tools.ToolCallMeta{}, err
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, tid, trace.EventToolSelection, trace.ActorAgent, toolSelectionData(uses, withArgs))
	}
	started := e.now()
	if e.Tools == nil {
		err := fmt.Errorf("engine: nil tool executor")
		meta := tools.ToolCallMeta{DurationMs: e.now().Sub(started).Milliseconds()}
		e.appendToolExecution(ctx, runID, tid, uses, meta, err)
		return nil, meta, err
	}
	toolCtx := ctx
	var endTool func(error)
	if runHandle != nil {
		safety := e.toolSafetyForUses(uses)
		toolCtx, endTool = runHandle.StartTool(telemetry.ToolAttrs{
			RunID: runID, StepID: tid, Uses: uses,
			Trusted: safety.Trusted, SideEffects: safety.SideEffects, RequiresApproval: safety.RequiresApproval,
		})
	}
	// Bound the actual invocation by the remaining wall-clock budget, AFTER StartTool (which
	// returns its own span context) so the deadline reaches the transport — otherwise a hung
	// tool blocks the run forever despite maxWallClockSeconds (#394).
	toolCtx, cancelWC := e.wallClockDeadline(toolCtx, pol, pctx)
	defer cancelWC()
	resp, err := e.Tools.Call(toolCtx, tools.ToolCallRequest{Uses: uses, With: withArgs})
	if endTool != nil {
		endTool(err)
	}
	meta := resp.Meta
	if meta.DurationMs == 0 {
		meta.DurationMs = e.now().Sub(started).Milliseconds()
	}
	if err != nil {
		e.appendToolExecution(ctx, runID, tid, uses, meta, err)
		return nil, meta, err
	}
	e.appendToolExecution(ctx, runID, tid, uses, meta, nil)
	out, err := e.enforceToolOutput(ctx, wf, runID, tid, uses, resp.Output)
	if err != nil {
		return nil, meta, err
	}
	if err := pol.CheckStep(ctx, policy.StepContext{StepID: step.ID, OutputIsStructured: true}); err != nil {
		return nil, meta, err
	}
	return out, meta, nil
}

func (e *Executor) runAgentStep(ctx context.Context, runHandle *telemetry.RunHandle, pol policy.PolicyEvaluator, wf *spec.WorkflowResource, runID string, step spec.WorkflowStep, with map[string]any, pctx policy.RunContext, agent *spec.AgentResource) (map[string]any, models.GenerateMeta, error) {
	if agent == nil {
		return nil, models.GenerateMeta{}, fmt.Errorf("engine: nil agent resource")
	}
	modelRef := strings.TrimSpace(agent.Spec.Model)
	cli, modelID, err := e.modelClient(modelRef)
	if err != nil {
		return nil, models.GenerateMeta{}, err
	}
	sec := 0
	if agent.Spec.Constraints != nil {
		sec = agent.Spec.Constraints.TimeoutSeconds
	}
	ctx2, cancel := withSecondsTimeout(ctx, sec)
	defer cancel()
	// Also bound the model call by the remaining wall-clock budget, so an agent step without
	// constraints.timeoutSeconds still cannot hang a run past maxWallClockSeconds (#394).
	ctx2, cancelWC := e.wallClockDeadline(ctx2, pol, pctx)
	defer cancelWC()

	payload, err := json.Marshal(with)
	if err != nil {
		return nil, models.GenerateMeta{}, err
	}
	instructions := strings.TrimSpace(agent.Spec.Instructions)
	messages := []models.ChatMessage{
		{Role: "system", Content: instructions},
		{Role: "user", Content: string(payload)},
	}

	toolDefs, usesByName, err := e.advertisedAgentTools(agent)
	if err != nil {
		return nil, models.GenerateMeta{}, err
	}
	temperature := agentTemperature(agent)
	if len(toolDefs) == 0 {
		return e.finishAgentTurn(ctx, ctx2, runHandle, pol, cli, modelRef, modelID, runID, step, pctx, agent, models.GenerateRequest{
			Model:       modelID,
			Messages:    messages,
			Temperature: temperature,
		})
	}
	return e.runAgentToolLoop(ctx, ctx2, runHandle, pol, wf, cli, modelRef, modelID, runID, step, pctx, agent, messages, toolDefs, usesByName, temperature)
}

// agentTemperature returns the sampling temperature to send for agent, or nil to leave the provider
// default. constraints.temperature is a *float64, so an explicit 0 (deterministic sampling) is
// honored and sent; only an unset constraint (nil) falls back to the provider default (issue #388).
func agentTemperature(agent *spec.AgentResource) *float64 {
	if agent == nil || agent.Spec.Constraints == nil {
		return nil
	}
	if t := agent.Spec.Constraints.Temperature; t != nil {
		v := *t
		return &v
	}
	return nil
}

func (e *Executor) runAgentToolLoop(
	ctx, ctx2 context.Context,
	runHandle *telemetry.RunHandle,
	pol policy.PolicyEvaluator,
	wf *spec.WorkflowResource,
	cli models.ModelClient,
	modelRef, modelID, runID string,
	step spec.WorkflowStep,
	pctx policy.RunContext,
	agent *spec.AgentResource,
	messages []models.ChatMessage,
	toolDefs []models.ToolDef,
	advertised map[string]string,
	temperature *float64,
) (map[string]any, models.GenerateMeta, error) {
	// maxIter counts Generate turns. tool_use on the last turn fails without executing those calls
	// (maxIterations: 1 is a single completion; tools never run). HITL interrupt is not consulted
	// inside this loop: inner uses must already be pre-approved (--approve / ApprovedActions) or
	// CheckToolCall fails closed (approval_required).
	maxIter := agentMaxIterations(agent)
	var acc models.GenerateMeta
	loopPctx := pctx

	for i := 1; i <= maxIter; i++ {
		// Check already-accumulated cost (prior steps + prior turns) before the next Generate.
		if _, err := e.checkAgentLoopRun(ctx2, pol, runID, step.ID, pctx, acc); err != nil {
			return nil, acc, err
		}
		req := models.GenerateRequest{
			Model:       modelID,
			Messages:    messages,
			Tools:       toolDefs,
			ToolChoice:  models.ToolChoiceAuto,
			Temperature: temperature,
		}
		resp, err := e.generateAgentTurn(ctx, ctx2, runHandle, cli, modelRef, runID, step, req)
		if err != nil {
			return nil, acc, err
		}
		addGenerateMeta(&acc, resp.Meta)
		loopPctx, err = e.checkAgentLoopRun(ctx2, pol, runID, step.ID, pctx, acc)
		if err != nil {
			return nil, acc, err
		}

		switch resp.StopReason {
		case models.StopReasonEndTurn, "":
			// Empty stop is treated as end_turn only when the model did not request tools.
			if resp.StopReason == "" && len(resp.ToolCalls) > 0 {
				resp.StopReason = models.StopReasonToolUse
			} else {
				return e.completeAgentOutput(ctx, pol, agent, step, resp.Content, acc)
			}
		}
		if resp.StopReason != models.StopReasonToolUse {
			return nil, acc, fmt.Errorf("engine: agent %q stop reason %q is not end_turn or tool_use", agent.Metadata.Name, resp.StopReason)
		}
		if len(resp.ToolCalls) == 0 {
			return nil, acc, fmt.Errorf("engine: agent %q returned tool_use without tool calls", agent.Metadata.Name)
		}
		if i == maxIter {
			if e.Trace != nil {
				_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventLimitHit, trace.ActorSystem, map[string]any{
					"kind":        "max_iterations",
					"max":         maxIter,
					"iterations":  i,
					"stepId":      step.ID,
					"agent":       step.Agent,
					"stopReason":  resp.StopReason,
					"toolCallIds": toolCallIDs(resp.ToolCalls),
				})
			}
			return nil, acc, fmt.Errorf("engine: agent %q reached maxIterations (%d)", agent.Metadata.Name, maxIter)
		}

		results := make([]models.ToolResult, 0, len(resp.ToolCalls))
		for _, call := range resp.ToolCalls {
			content, tmeta, fatalErr := e.runAgentToolCall(ctx2, runHandle, pol, wf, runID, step, pctx, loopPctx, acc, advertised, call)
			if fatalErr != nil {
				return nil, acc, fatalErr
			}
			addToolMeta(&acc, tmeta)
			var err error
			loopPctx, err = e.checkAgentLoopRun(ctx2, pol, runID, step.ID, pctx, acc)
			if err != nil {
				return nil, acc, err
			}
			results = append(results, models.ToolResult{ToolCallID: call.ID, Content: content})
		}
		messages = append(messages,
			models.ChatMessage{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls},
			models.ChatMessage{Role: "user", ToolResults: results},
		)
	}
	return nil, acc, fmt.Errorf("engine: agent %q reached maxIterations (%d)", agent.Metadata.Name, maxIter)
}

// runAgentToolCall executes one tool call from the agent loop. A per-call error — the model naming
// an unadvertised tool, malformed arguments, or a tool MISS such as read_file on a path that does
// not exist — is returned to the agent as a recoverable error OBSERVATION it can reason over (list
// the directory, try a neighbor, narrow down), NOT propagated as a fatal run error (#451). Only
// genuinely unrecoverable conditions abort the run (fatalErr non-nil): a policy/capability denial,
// a run cancel/timeout, or an adapter misconfiguration (native.ErrFatalTool). The returned content
// is the tool-result payload for the model (an {"error": …} object on a recoverable miss); tmeta is
// the step metadata to fold into the loop's cost.
func (e *Executor) runAgentToolCall(
	ctx2 context.Context,
	runHandle *telemetry.RunHandle,
	pol policy.PolicyEvaluator,
	wf *spec.WorkflowResource,
	runID string,
	step spec.WorkflowStep,
	pctx, loopPctx policy.RunContext,
	acc models.GenerateMeta,
	advertised map[string]string,
	call models.ToolCall,
) (content string, tmeta tools.ToolCallMeta, fatalErr error) {
	// Resolving the call name and parsing its arguments are PRE-execution: an unadvertised tool name
	// is a capability-boundary violation (ADR 002 — no operation is agent-callable unless advertised)
	// and malformed arguments are a broken call, both fatal. Only the tool EXECUTION below is a "tool
	// error" #451 makes recoverable.
	uses, err := resolveAgentToolCall(call.Name, advertised)
	if err != nil {
		return "", tools.ToolCallMeta{}, err
	}
	args, err := parseToolCallArgs(call.Arguments)
	if err != nil {
		return "", tools.ToolCallMeta{}, fmt.Errorf("engine: tool call %q: %w", call.Name, err)
	}
	if _, err := e.checkAgentLoopRun(ctx2, pol, runID, step.ID, pctx, acc); err != nil {
		return "", tools.ToolCallMeta{}, err // run-level budget breach — fatal
	}
	out, tmeta, err := e.runToolStep(ctx2, runHandle, pol, wf, runID, step, args, loopPctx, uses, args)
	if err != nil {
		if isFatalToolError(err) {
			return "", tmeta, err
		}
		return recoverableToolObservation(err), tmeta, nil
	}
	return encodeToolResultContent(out), tmeta, nil
}

// maxRecoverableObservationBytes bounds the error text handed back to the agent so a pathological
// tool error cannot balloon the next prompt.
const maxRecoverableObservationBytes = 4 << 10

// recoverableToolObservation renders a tool error as the {"error": …} tool result an agent receives,
// so it can correct course (list the directory, try a neighbor) instead of the run dying on the miss
// (issue #451). The message is bounded; the tool_execution trace event for the failed call is still
// emitted (with success=false and a redacted reason) by runToolStep, so the audit record is intact.
func recoverableToolObservation(err error) string {
	msg := err.Error()
	if len(msg) > maxRecoverableObservationBytes {
		msg = msg[:maxRecoverableObservationBytes] + "…"
	}
	return encodeToolResultContent(map[string]any{"error": msg})
}

// isFatalToolError reports whether a tool error must ABORT the run rather than be delivered to the
// agent as a recoverable observation: a policy/capability denial, a run cancel/timeout, or an
// adapter misconfiguration marked by the native adapter (issue #451). Everything else — a missing
// file, a bad argument, a failed exec, an invalid-input schema rejection — is recoverable.
func isFatalToolError(err error) bool {
	if err == nil {
		return false
	}
	if _, denied := policy.AsDenied(err); denied {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	return errors.Is(err, native.ErrFatalTool)
}

func (e *Executor) finishAgentTurn(
	ctx, ctx2 context.Context,
	runHandle *telemetry.RunHandle,
	pol policy.PolicyEvaluator,
	cli models.ModelClient,
	modelRef, modelID, runID string,
	step spec.WorkflowStep,
	pctx policy.RunContext,
	agent *spec.AgentResource,
	req models.GenerateRequest,
) (map[string]any, models.GenerateMeta, error) {
	resp, err := e.generateAgentTurn(ctx, ctx2, runHandle, cli, modelRef, runID, step, req)
	if err != nil {
		return nil, models.GenerateMeta{}, err
	}
	if _, err := e.checkAgentLoopRun(ctx2, pol, runID, step.ID, pctx, resp.Meta); err != nil {
		return nil, resp.Meta, err
	}
	return e.completeAgentOutput(ctx, pol, agent, step, resp.Content, resp.Meta)
}

func (e *Executor) generateAgentTurn(
	ctx, ctx2 context.Context,
	runHandle *telemetry.RunHandle,
	cli models.ModelClient,
	modelRef, runID string,
	step spec.WorkflowStep,
	req models.GenerateRequest,
) (models.GenerateResponse, error) {
	var resp models.GenerateResponse
	err := withAgentRetry(ctx2, func() error {
		callCtx := ctx2
		var endModel func(error)
		if runHandle != nil {
			callCtx, endModel = runHandle.StartModel(telemetry.ModelAttrs{
				RunID: runID, StepID: step.ID, AgentName: step.Agent, ModelRef: modelRef,
			})
		}
		r, genErr := cli.Generate(callCtx, req)
		if endModel != nil {
			endModel(genErr)
		}
		if genErr != nil {
			return genErr
		}
		resp = r
		return nil
	})
	if err != nil {
		return models.GenerateResponse{}, err
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventLLMCompletion, trace.ActorAgent,
			trace.LLMCompletionData(step.Agent, modelRef, resp.Meta.CostUSD))
	}
	return resp, nil
}

func (e *Executor) checkAgentLoopRun(ctx context.Context, pol policy.PolicyEvaluator, runID, stepID string, base policy.RunContext, acc models.GenerateMeta) (policy.RunContext, error) {
	loop := base
	loop.AccumulatedCostUSD = base.AccumulatedCostUSD + acc.CostUSD
	if !base.StartedAt.IsZero() {
		loop.Elapsed = e.now().Sub(base.StartedAt)
	}
	if pol == nil {
		return loop, nil
	}
	if err := pol.CheckRun(ctx, loop); err != nil {
		if e.Trace != nil {
			if d, ok := policy.AsDenied(err); ok {
				_, _ = e.Trace.Append(ctx, runID, stepID, trace.EventSystemError, trace.ActorSystem, d.TraceData())
			}
		}
		e.appendCostLimitHit(ctx, runID, stepID, err)
		return loop, err
	}
	return loop, nil
}

func (e *Executor) appendCostLimitHit(ctx context.Context, runID, stepID string, err error) {
	if e == nil || e.Trace == nil {
		return
	}
	d, ok := policy.AsDenied(err)
	if !ok || d.Reason != policy.ReasonMaxCost {
		return
	}
	_, _ = e.Trace.Append(ctx, runID, stepID, trace.EventLimitHit, trace.ActorSystem, costLimitHitData(d, stepID))
}

// costLimitHitData is the limit_hit payload for execution.maxTotalCostUsd (issue #163).
// This is not [trace.LimitHitTraceData], which is byte-oriented (issue #117).
func costLimitHitData(d *policy.DeniedError, stepID string) map[string]any {
	var extra map[string]any
	if d != nil {
		extra = d.Extra
	}
	return trace.LimitHitData("max_cost", stepID, extra)
}

func (e *Executor) completeAgentOutput(ctx context.Context, pol policy.PolicyEvaluator, agent *spec.AgentResource, step spec.WorkflowStep, content string, meta models.GenerateMeta) (map[string]any, models.GenerateMeta, error) {
	// Validates against the pinned schema bundle on resume, or the on-disk schema on a fresh run.
	if err := e.validateAgentOutputSchema(agent, content); err != nil {
		return nil, meta, err
	}
	out, err := parseAgentJSONObject(content)
	if err != nil {
		return nil, meta, err
	}
	if err := pol.CheckStep(ctx, policy.StepContext{StepID: step.ID, OutputIsStructured: true}); err != nil {
		return nil, meta, err
	}
	return out, meta, nil
}

func toolCallIDs(calls []models.ToolCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if id := strings.TrimSpace(c.ID); id != "" {
			out = append(out, id)
		}
	}
	return out
}

func (e *Executor) appendToolExecution(ctx context.Context, runID, stepID, uses string, meta tools.ToolCallMeta, callErr error) {
	if e == nil || e.Trace == nil {
		return
	}
	_, _ = e.Trace.Append(ctx, runID, stepID, trace.EventToolExecution, trace.ActorAgent, toolExecutionData(uses, meta, callErr))
}

// toolSelectionData / toolExecutionData / argumentsDigest delegate to the shared trace builders
// (issue #341) so the internal loop and the external runtime emit structurally identical events.
func toolSelectionData(uses string, args map[string]any) map[string]any {
	return trace.ToolSelectionData(uses, toolNameFromUses(uses), args)
}

func toolExecutionData(uses string, meta tools.ToolCallMeta, callErr error) map[string]any {
	return trace.ToolExecutionData(uses, toolNameFromUses(uses), meta.DurationMs, meta.CostUSD, callErr != nil)
}

func argumentsDigest(args map[string]any) string { return trace.ArgumentsDigest(args) }

// toolCallFailedReason is the redacted tool_execution error value (see trace.ToolCallFailedReason).
const toolCallFailedReason = trace.ToolCallFailedReason

func toolNameFromUses(uses string) string {
	name, _, err := tools.ParseUses(uses)
	if err != nil {
		return ""
	}
	return name
}
