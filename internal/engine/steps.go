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

func (e *Executor) runToolStep(ctx context.Context, pol policy.PolicyEvaluator, runID string, step spec.WorkflowStep, with map[string]any, pctx policy.RunContext) (map[string]any, tools.ToolCallMeta, error) {
	uses := strings.TrimSpace(step.Uses)
	if err := pol.CheckToolCall(ctx, policy.ToolCallContext{Run: pctx, StepID: step.ID, Uses: uses}); err != nil {
		return nil, tools.ToolCallMeta{}, err
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventToolCalled, map[string]any{"uses": uses})
	}
	if e.Tools == nil {
		return nil, tools.ToolCallMeta{}, fmt.Errorf("engine: nil tool executor")
	}
	resp, err := e.Tools.Call(ctx, tools.ToolCallRequest{Uses: uses, With: with})
	if err != nil {
		return nil, tools.ToolCallMeta{}, err
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventToolCompleted, map[string]any{"uses": uses, "costUsd": resp.Meta.CostUSD})
	}
	if err := pol.CheckStep(ctx, policy.StepContext{StepID: step.ID, OutputIsStructured: true}); err != nil {
		return nil, resp.Meta, err
	}
	return resp.Output, resp.Meta, nil
}

func (e *Executor) runAgentStep(ctx context.Context, pol policy.PolicyEvaluator, runID string, step spec.WorkflowStep, with map[string]any, pctx policy.RunContext, agent *spec.AgentResource) (map[string]any, models.GenerateMeta, error) {
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

	var resp models.GenerateResponse
	err = withAgentRetry(ctx2, func() error {
		if e.Trace != nil {
			_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventModelCalled, map[string]any{"agent": step.Agent, "model": modelRef})
		}
		r, genErr := cli.Generate(ctx2, models.GenerateRequest{Model: modelID, Messages: messages})
		if genErr != nil {
			return genErr
		}
		resp = r
		return nil
	})
	if err != nil {
		return nil, models.GenerateMeta{}, err
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, step.ID, trace.EventModelCompleted, map[string]any{"agent": step.Agent, "costUsd": resp.Meta.CostUSD})
	}
	if err := validateAgentOutput(e.ProjectRoot, agent, resp.Content); err != nil {
		return nil, resp.Meta, err
	}
	out, err := parseAgentJSONObject(resp.Content)
	if err != nil {
		return nil, resp.Meta, err
	}
	structured := true
	if err := pol.CheckStep(ctx, policy.StepContext{StepID: step.ID, OutputIsStructured: structured}); err != nil {
		return nil, resp.Meta, err
	}
	return out, resp.Meta, nil
}
