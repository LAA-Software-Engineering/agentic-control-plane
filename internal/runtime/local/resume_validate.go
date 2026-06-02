package local

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/plan"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/runtime"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
)

// resumeEnvironmentName returns the environment overlay to apply when resuming a run.
// When the run row pins a non-empty name, the CLI must not pass a conflicting -e value.
func resumeEnvironmentName(run *state.Run, opts runtime.WorkflowRunOptions) (string, error) {
	pinned := strings.TrimSpace(run.EnvironmentName)
	cli := strings.TrimSpace(opts.EnvironmentName)
	if pinned == "" {
		return cli, nil
	}
	if cli != "" && cli != pinned {
		return "", fmt.Errorf("local: environment %q does not match run %q", cli, pinned)
	}
	return pinned, nil
}

// validateResumeWorkflowSpec ensures the workflow definition has not changed since the run started.
func validateResumeWorkflowSpec(run *state.Run, wf *spec.WorkflowResource) error {
	stored := strings.TrimSpace(run.WorkflowSpecHash)
	if stored == "" {
		return nil
	}
	current, err := plan.WorkflowSpecHash(wf)
	if err != nil {
		return fmt.Errorf("local: hash workflow: %w", err)
	}
	if current != stored {
		return fmt.Errorf("local: workflow spec changed since run started")
	}
	return nil
}
