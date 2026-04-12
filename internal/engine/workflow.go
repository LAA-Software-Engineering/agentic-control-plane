package engine

import (
	"encoding/json"
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/schema"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func lookupWorkflow(g *spec.ProjectGraph, name string) (*spec.WorkflowResource, error) {
	if g == nil || g.Workflows == nil {
		return nil, fmt.Errorf("engine: unknown workflow %q", name)
	}
	wf, ok := g.Workflows[name]
	if !ok || wf == nil {
		return nil, fmt.Errorf("engine: unknown workflow %q", name)
	}
	return wf, nil
}

func validateWorkflowInput(projectRoot string, wf *spec.WorkflowResource, input map[string]any) error {
	if wf == nil || wf.Spec.Input == nil {
		return nil
	}
	sref := wf.Spec.Input.Schema
	if sref == "" {
		return nil
	}
	path, err := schema.ResolveSchemaPath(projectRoot, sref)
	if err != nil {
		return fmt.Errorf("engine: workflow input schema: %w", err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("engine: marshal workflow input: %w", err)
	}
	if err := schema.Validate(path, raw); err != nil {
		return fmt.Errorf("engine: workflow input: %w", err)
	}
	return nil
}

func buildWorkflowOutput(wf *spec.WorkflowResource, ictx Context) (map[string]any, error) {
	if wf == nil || wf.Spec.Output == nil || wf.Spec.Output.Value == nil {
		return map[string]any{}, nil
	}
	v, err := InterpolateWalk(wf.Spec.Output.Value, ictx)
	if err != nil {
		return nil, err
	}
	out, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("engine: workflow output value must interpolate to an object")
	}
	return out, nil
}
