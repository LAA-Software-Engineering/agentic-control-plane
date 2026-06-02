package local

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func buildEngineHitlOptions(opts runtime.WorkflowRunOptions) engine.HitlRunOptions {
	out := engine.HitlRunOptions{
		AutoApprove: opts.AutoApprove,
		Actor:       opts.HitlActor,
	}
	if opts.HitlDecision == nil {
		return out
	}
	kind, err := spec.ParseHitlDecisionKind(opts.HitlDecision.Kind)
	if err != nil {
		return out
	}
	out.Decision = &policy.HitlDecisionInput{
		Kind:         kind,
		Actor:        opts.HitlActor,
		EditedWith:   opts.HitlDecision.EditedWith,
		SwitchTarget: opts.HitlDecision.SwitchTarget,
	}
	return out
}
