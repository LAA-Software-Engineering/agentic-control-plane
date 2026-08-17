package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/models"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/schema"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/tools"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func validateAgentOutput(projectRoot string, agent *spec.AgentResource, content string) error {
	if agent == nil || agent.Spec.Output == nil {
		return nil
	}
	sref := strings.TrimSpace(agent.Spec.Output.Schema)
	if sref == "" {
		return nil
	}
	path, err := schema.ResolveSchemaPath(projectRoot, sref)
	if err != nil {
		return fmt.Errorf("engine: agent output schema: %w", err)
	}
	if err := schema.Validate(path, []byte(strings.TrimSpace(content))); err != nil {
		return fmt.Errorf("engine: agent output: %w", err)
	}
	return nil
}

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
	var err error
	withArgs, err = e.enforceToolInput(ctx, wf, runID, step.ID, uses, withArgs)
	if err != nil {
		return nil, tools.ToolCallMeta{}, err
	}
	if err := pol.CheckToolCall(ctx, policy.ToolCallContext{Run: pctx, StepID: step.ID, Uses: uses, With: withArgs}); err != nil {
		if e.Trace != nil {
			if d, ok := policy.AsDenied(err); ok {
				_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventSystemError, trace.ActorSystem, d.TraceData())
			}
		}
		return nil, tools.ToolCallMeta{}, err
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventToolSelection, trace.ActorAgent, map[string]any{"uses": uses})
	}
	if e.Tools == nil {
		return nil, tools.ToolCallMeta{}, fmt.Errorf("engine: nil tool executor")
	}
	toolCtx := ctx
	var endTool func(error)
	if runHandle != nil {
		safety := e.toolSafetyForUses(uses)
		toolCtx, endTool = runHandle.StartTool(telemetry.ToolAttrs{
			RunID: runID, StepID: step.ID, Uses: uses,
			Trusted: safety.Trusted, SideEffects: safety.SideEffects, RequiresApproval: safety.RequiresApproval,
		})
	}
	resp, err := e.Tools.Call(toolCtx, tools.ToolCallRequest{Uses: uses, With: withArgs})
	if endTool != nil {
		endTool(err)
	}
	if err != nil {
		return nil, tools.ToolCallMeta{}, err
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventToolExecution, trace.ActorAgent, map[string]any{"uses": uses, "costUsd": resp.Meta.CostUSD})
	}
	out, err := e.enforceToolOutput(ctx, wf, runID, step.ID, uses, resp.Output)
	if err != nil {
		return nil, resp.Meta, err
	}
	if err := pol.CheckStep(ctx, policy.StepContext{StepID: step.ID, OutputIsStructured: true}); err != nil {
		return nil, resp.Meta, err
	}
	return out, resp.Meta, nil
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

	payload, err := json.Marshal(with)
	if err != nil {
		return nil, models.GenerateMeta{}, err
	}
	instructions := strings.TrimSpace(agent.Spec.Instructions)
	messages := []models.ChatMessage{
		{Role: "system", Content: instructions},
		{Role: "user", Content: string(payload)},
	}

	toolDefs, err := e.agentToolDefs(agent)
	if err != nil {
		return nil, models.GenerateMeta{}, err
	}
	if len(toolDefs) == 0 {
		return e.finishAgentTurn(ctx, ctx2, runHandle, pol, cli, modelRef, modelID, runID, step, agent, models.GenerateRequest{
			Model:    modelID,
			Messages: messages,
		})
	}
	return e.runAgentToolLoop(ctx, ctx2, runHandle, pol, wf, cli, modelRef, modelID, runID, step, pctx, agent, messages, toolDefs)
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
) (map[string]any, models.GenerateMeta, error) {
	declared := declaredAgentTools(agent)
	maxIter := agentMaxIterations(agent)
	var acc models.GenerateMeta
	loopPctx := pctx

	for i := 1; i <= maxIter; i++ {
		req := models.GenerateRequest{
			Model:      modelID,
			Messages:   messages,
			Tools:      toolDefs,
			ToolChoice: models.ToolChoiceAuto,
		}
		resp, err := e.generateAgentTurn(ctx, ctx2, runHandle, cli, modelRef, runID, step, req)
		if err != nil {
			return nil, acc, err
		}
		addGenerateMeta(&acc, resp.Meta)
		loopPctx.AccumulatedCostUSD = pctx.AccumulatedCostUSD + acc.CostUSD

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
			uses, err := resolveAgentToolCall(call.Name, declared)
			if err != nil {
				return nil, acc, err
			}
			args, err := parseToolCallArgs(call.Arguments)
			if err != nil {
				return nil, acc, fmt.Errorf("engine: tool call %q: %w", call.Name, err)
			}
			out, tmeta, err := e.runToolStep(ctx, runHandle, pol, wf, runID, step, args, loopPctx, uses, args)
			if err != nil {
				return nil, acc, err
			}
			addToolMeta(&acc, tmeta)
			loopPctx.AccumulatedCostUSD = pctx.AccumulatedCostUSD + acc.CostUSD
			results = append(results, models.ToolResult{
				ToolCallID: call.ID,
				Content:    encodeToolResultContent(out),
			})
		}
		messages = append(messages,
			models.ChatMessage{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls},
			models.ChatMessage{Role: "user", ToolResults: results},
		)
	}
	return nil, acc, fmt.Errorf("engine: agent %q reached maxIterations (%d)", agent.Metadata.Name, maxIter)
}

func (e *Executor) finishAgentTurn(
	ctx, ctx2 context.Context,
	runHandle *telemetry.RunHandle,
	pol policy.PolicyEvaluator,
	cli models.ModelClient,
	modelRef, modelID, runID string,
	step spec.WorkflowStep,
	agent *spec.AgentResource,
	req models.GenerateRequest,
) (map[string]any, models.GenerateMeta, error) {
	resp, err := e.generateAgentTurn(ctx, ctx2, runHandle, cli, modelRef, runID, step, req)
	if err != nil {
		return nil, models.GenerateMeta{}, err
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
		_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventLLMCompletion, trace.ActorAgent, map[string]any{
			"agent": step.Agent, "model": modelRef, "costUsd": resp.Meta.CostUSD,
		})
	}
	return resp, nil
}

func (e *Executor) completeAgentOutput(ctx context.Context, pol policy.PolicyEvaluator, agent *spec.AgentResource, step spec.WorkflowStep, content string, meta models.GenerateMeta) (map[string]any, models.GenerateMeta, error) {
	if err := validateAgentOutput(e.ProjectRoot, agent, content); err != nil {
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
