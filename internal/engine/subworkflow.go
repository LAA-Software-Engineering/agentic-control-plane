package engine

import (
	"fmt"
	"strings"
)

// qualifyStepID prefixes a step id with the enclosing subworkflow path so nested
// run steps get stable, collision-free ids (issue #194). The execir InvokeWorkflow
// path sets the prefix on the child Executor.
func qualifyStepID(prefix, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("engine: empty step id")
	}
	if strings.Contains(id, "/") {
		return "", fmt.Errorf("engine: step id %q must not contain '/'", id)
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return id, nil
	}
	return prefix + "/" + id, nil
}

func (e *Executor) qualID(id string) string {
	prefix := ""
	if e != nil {
		prefix = e.stepPrefix
	}
	out, err := qualifyStepID(prefix, id)
	if err != nil {
		return id
	}
	return out
}
