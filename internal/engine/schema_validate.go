package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/schema"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

// validateAgainstSchema validates instance against the schema referenced by sref. On a pinned
// resume (issue #207 follow-up) it uses the schema content captured in the run's deployment snapshot
// (e.Schemas), so it never re-reads a possibly-changed file — a schema absent from the bundle was
// not captured at run start and is treated as gradual (allowed). Otherwise it resolves and reads the
// schema file under ProjectRoot as before.
func (e *Executor) validateAgainstSchema(sref string, instance []byte) error {
	sref = strings.TrimSpace(sref)
	if sref == "" {
		return nil
	}
	if e.PinnedGraph {
		content, ok := e.Schemas[sref]
		if !ok {
			return nil
		}
		return schema.ValidateContent(sref, []byte(content), instance)
	}
	path, err := schema.ResolveSchemaPath(e.ProjectRoot, sref)
	if err != nil {
		return err
	}
	return schema.Validate(path, instance)
}

// validateWorkflowInputSchema validates a workflow's input against its declared input schema,
// choosing the pinned bundle or the on-disk file per [Executor.validateAgainstSchema].
func (e *Executor) validateWorkflowInputSchema(wf *spec.WorkflowResource, input map[string]any) error {
	if wf == nil || wf.Spec.Input == nil {
		return nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("engine: marshal workflow input: %w", err)
	}
	if err := e.validateAgainstSchema(wf.Spec.Input.Schema, raw); err != nil {
		return fmt.Errorf("engine: workflow input: %w", err)
	}
	return nil
}

// validateAgentOutputSchema validates an agent's output against its declared output schema.
func (e *Executor) validateAgentOutputSchema(agent *spec.AgentResource, content string) error {
	if agent == nil || agent.Spec.Output == nil {
		return nil
	}
	if err := e.validateAgainstSchema(agent.Spec.Output.Schema, []byte(strings.TrimSpace(content))); err != nil {
		return fmt.Errorf("engine: agent output: %w", err)
	}
	return nil
}
