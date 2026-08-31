package local

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/config"
	"github.com/LAA-Software-Engineering/terfyn/internal/plan"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

// resolveConfigForResume picks the environment overlay name for resume and rejects CLI conflicts.
func resolveConfigForResume(run *state.Run, cliEnv string) (string, error) {
	if run == nil {
		return strings.TrimSpace(cliEnv), nil
	}
	pinned := strings.TrimSpace(run.EnvironmentName)
	cli := strings.TrimSpace(cliEnv)
	if pinned == "" {
		return cli, nil
	}
	if cli != "" && cli != pinned {
		return "", fmt.Errorf("local: environment %q does not match run %q", cli, pinned)
	}
	return pinned, nil
}

// ResolvedConfigForRun resolves configuration for resume using the run's pinned environment.
//
// Since #207 this is no longer the authority source for a pinned run: [Runtime.Resume] hydrates the
// graph (policy, tools, capability manifest) from the run's deployment snapshot, so a policy/tool
// edit landing between suspend and resume cannot widen an in-flight run's authority. The resolved
// config returned here supplies the project root and is the fallback graph for legacy runs created
// before #207 (empty snapshot digest).
func ResolvedConfigForRun(run *state.Run, base config.ResolveOptions, cliEnv string) (*config.ResolvedConfig, error) {
	env, err := resolveConfigForResume(run, cliEnv)
	if err != nil {
		return nil, err
	}
	opts := base
	opts.Env = env
	return config.Resolve(opts)
}

// validateResumeWorkflowSpec ensures the workflow definition has not changed since
// the run started. The drift hash commits to the workflow's execution IR as well
// as its resource projection (#277): execDigest is the pinned program's digest
// (from the hydrated snapshot on the pinned path, or the re-lowered config on the
// legacy fallback), folded exactly as run-start stored it, so a lowering-only
// change to a control-flow workflow is drift rather than invisible.
func validateResumeWorkflowSpec(run *state.Run, wf *spec.WorkflowResource, execDigest string) error {
	stored := strings.TrimSpace(run.WorkflowSpecHash)
	if stored == "" {
		return nil
	}
	current, err := plan.WorkflowSpecHashWithExec(wf, execDigest)
	if err != nil {
		return fmt.Errorf("local: hash workflow: %w", err)
	}
	if current == stored {
		return nil
	}
	// Migration: a run row created before #277 stored the resource-only (bare)
	// hash. When a program exists, the folded hash differs from the bare one, so
	// also accept a match against the bare hash — pre-fold runs still resume. (As
	// before #277, a lowering-only change is not reported as drift for such a row.)
	if execDigest != "" {
		bare, err := plan.WorkflowSpecHash(wf)
		if err != nil {
			return fmt.Errorf("local: hash workflow: %w", err)
		}
		if bare == stored {
			return nil
		}
	}
	return fmt.Errorf("local: workflow spec changed since run started")
}
