package local

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/engine"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func buildEngineHitlOptions(cfg engineRunConfig) (engine.HitlRunOptions, error) {
	out := engine.HitlRunOptions{
		AutoApprove: cfg.autoApprove,
		Actor:       cfg.hitlActor,
	}
	if cfg.hitlDecision == nil {
		return out, nil
	}
	if !spec.IsValidHitlDecisionKind(cfg.hitlDecision.Kind) {
		return out, fmt.Errorf("local: invalid hitl decision kind %q", cfg.hitlDecision.Kind)
	}
	out.Decision = &policy.HitlDecisionInput{
		Kind:         cfg.hitlDecision.Kind,
		Actor:        cfg.hitlActor,
		EditedWith:   cfg.hitlDecision.EditedWith,
		SwitchTarget: cfg.hitlDecision.SwitchTarget,
	}
	return out, nil
}
