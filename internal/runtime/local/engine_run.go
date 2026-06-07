package local

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
)

// engineRunConfig carries execution options shared by Invoke and Resume engine paths.
type engineRunConfig struct {
	approvedActions []string
	autoApprove     bool
	hitlActor       string
	hitlDecision    *runtime.HitlDecisionOptions
}

func engineRunConfigFromInvoke(opts runtime.InvokeOptions) engineRunConfig {
	return engineRunConfig{
		approvedActions: opts.ApprovedActions,
		autoApprove:     opts.AutoApprove,
		hitlActor:       opts.HitlActor,
	}
}

func engineRunConfigFromResume(opts runtime.ResumeOptions) engineRunConfig {
	return engineRunConfig{
		approvedActions: opts.ApprovedActions,
		autoApprove:     opts.AutoApprove,
		hitlActor:       opts.HitlActor,
		hitlDecision:    opts.HitlDecision,
	}
}
