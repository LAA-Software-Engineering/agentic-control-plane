package engine

import (
	"context"
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/trace"
)

func (e *Executor) redactionOpts() trace.RedactionOptions {
	if e != nil && e.Trace != nil {
		return e.Trace.Redaction
	}
	return trace.DefaultRedactionOptions()
}

func (e *Executor) resolveToolLimits(wf *spec.WorkflowResource, uses string) spec.ResolvedExecutionLimits {
	var project *spec.ProjectSpec
	var wfSpec *spec.WorkflowSpec
	var toolSpec *spec.ToolSpec
	if e != nil && e.Graph != nil {
		project = &e.Graph.Spec
	}
	if wf != nil {
		wfSpec = &wf.Spec
	}
	if tn, ok := spec.ParseToolUses(uses); ok && e != nil && e.Graph != nil {
		if tr, found := e.Graph.Tools[tn]; found && tr != nil {
			toolSpec = &tr.Spec
		}
	}
	return spec.ResolveExecutionLimits(project, wfSpec, toolSpec)
}

func (e *Executor) resolveCheckpointLimits(wf *spec.WorkflowResource) spec.ResolvedExecutionLimits {
	var project *spec.ProjectSpec
	var wfSpec *spec.WorkflowSpec
	if e != nil && e.Graph != nil {
		project = &e.Graph.Spec
	}
	if wf != nil {
		wfSpec = &wf.Spec
	}
	return spec.ResolveExecutionLimits(project, wfSpec, nil)
}

func (e *Executor) enforceMapLimit(
	ctx context.Context,
	runID, stepID, uses string,
	kind spec.LimitKind,
	v map[string]any,
	maxBytes int,
	policy spec.LimitExceedPolicy,
) (map[string]any, error) {
	orig, err := trace.JSONByteLen(v)
	if err != nil {
		return nil, fmt.Errorf("engine: measure %s bytes: %w", kind, err)
	}
	if maxBytes <= 0 || orig <= maxBytes {
		return v, nil
	}
	truncated := policy == spec.LimitExceedTruncate
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, stepID, trace.EventLimitHit, trace.ActorSystem,
			trace.LimitHitTraceData(kind, maxBytes, orig, policy, truncated, stepID, uses))
	}
	if policy == spec.LimitExceedFail {
		return nil, fmt.Errorf("engine: %s exceeds limit (%d > %d bytes)", kind, orig, maxBytes)
	}
	out, _, _, err := trace.TruncateMapValue(v, maxBytes, e.redactionOpts())
	if err != nil {
		return nil, fmt.Errorf("engine: truncate %s: %w", kind, err)
	}
	return out, nil
}

func (e *Executor) enforceToolInput(
	ctx context.Context,
	wf *spec.WorkflowResource,
	runID, stepID, uses string,
	with map[string]any,
) (map[string]any, error) {
	limits := e.resolveToolLimits(wf, uses)
	return e.enforceMapLimit(ctx, runID, stepID, uses, spec.LimitKindToolInput, with,
		limits.MaxToolInputBytes, limits.ToolInputExceedPolicy)
}

func (e *Executor) enforceToolOutput(
	ctx context.Context,
	wf *spec.WorkflowResource,
	runID, stepID, uses string,
	out map[string]any,
) (map[string]any, error) {
	limits := e.resolveToolLimits(wf, uses)
	return e.enforceMapLimit(ctx, runID, stepID, uses, spec.LimitKindToolOutput, out,
		limits.MaxToolOutputBytes, limits.ToolOutputExceedPolicy)
}

func (e *Executor) enforceCheckpointSize(
	ctx context.Context,
	wf *spec.WorkflowResource,
	runID, stepID string,
	contextJSON string,
) error {
	limits := e.resolveCheckpointLimits(wf)
	orig := len(contextJSON)
	if orig <= limits.MaxCheckpointBytes {
		return nil
	}
	if e.Trace != nil {
		_, _ = e.Trace.Append(ctx, runID, stepID, trace.EventLimitHit, trace.ActorSystem,
			trace.LimitHitTraceData(spec.LimitKindCheckpoint, limits.MaxCheckpointBytes, orig,
				spec.LimitExceedFail, false, stepID, ""))
	}
	return fmt.Errorf("engine: checkpoint context exceeds %d bytes (got %d)", limits.MaxCheckpointBytes, orig)
}
