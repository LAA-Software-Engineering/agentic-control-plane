package tools

import "fmt"

// UnknownOperationError is returned when a native (or registered) tool does not implement the operation.
type UnknownOperationError struct {
	Tool      string
	Operation string
}

func (e *UnknownOperationError) Error() string {
	return fmt.Sprintf("tools: unknown operation %q for tool %q", e.Operation, e.Tool)
}
