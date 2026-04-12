package policy

import (
	"fmt"
	"time"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func checkExecutionBudgets(run RunContext, ex *spec.PolicyExecution) error {
	if ex == nil {
		return nil
	}
	if ex.MaxWallClockSeconds > 0 {
		limit := time.Duration(ex.MaxWallClockSeconds) * time.Second
		if run.Elapsed > limit {
			return denied(
				ReasonMaxWallClock,
				fmt.Sprintf("policy: wall clock %s exceeds limit %s", run.Elapsed, limit),
				"",
				map[string]any{
					"maxWallClockSeconds": ex.MaxWallClockSeconds,
					"elapsedSeconds":      run.Elapsed.Seconds(),
				},
			)
		}
	}
	if ex.MaxTotalCostUsd > 0 && run.AccumulatedCostUSD > ex.MaxTotalCostUsd {
		return denied(
			ReasonMaxCost,
			fmt.Sprintf("policy: cost $%.4f exceeds ceiling $%.4f", run.AccumulatedCostUSD, ex.MaxTotalCostUsd),
			"",
			map[string]any{
				"maxTotalCostUsd": ex.MaxTotalCostUsd,
				"accumulatedUsd":  run.AccumulatedCostUSD,
			},
		)
	}
	return nil
}

func checkStructuredOutputRequired(step StepContext, ex *spec.PolicyExecution) error {
	if ex == nil || !ex.RequireStructuredOutput {
		return nil
	}
	if step.OutputIsStructured {
		return nil
	}
	return denied(
		ReasonStructuredOutput,
		"policy: structured output required for this step",
		"",
		map[string]any{"stepId": step.StepID},
	)
}
