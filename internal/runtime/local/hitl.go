package local

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func buildEngineHitlOptions(opts runtime.WorkflowRunOptions) (engine.HitlRunOptions, error) {
	out := engine.HitlRunOptions{
		AutoApprove: opts.AutoApprove,
		Actor:       opts.HitlActor,
	}
	if opts.HitlDecision == nil {
		return out, nil
	}
	if !spec.IsValidHitlDecisionKind(opts.HitlDecision.Kind) {
		return out, fmt.Errorf("local: invalid hitl decision kind %q", opts.HitlDecision.Kind)
	}
	out.Decision = &policy.HitlDecisionInput{
		Kind:         opts.HitlDecision.Kind,
		Actor:        opts.HitlActor,
		EditedWith:   opts.HitlDecision.EditedWith,
		SwitchTarget: opts.HitlDecision.SwitchTarget,
	}
	return out, nil
}
