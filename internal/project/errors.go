package project

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// MissingRefError is reported when a graph reference does not resolve (see [spec.MissingRefError]).
type MissingRefError = spec.MissingRefError

// DuplicateResourceError is returned when two files define the same kind/name (§9.1).
type DuplicateResourceError struct {
	Kind  string
	Name  string
	Paths []string // typically [firstFile, secondFile]
}

func (e *DuplicateResourceError) Error() string {
	if e == nil {
		return ""
	}
	if len(e.Paths) >= 2 {
		return fmt.Sprintf("duplicate resource %s/%s: first %q, second %q", e.Kind, e.Name, e.Paths[0], e.Paths[1])
	}
	return fmt.Sprintf("duplicate resource %s/%s", e.Kind, e.Name)
}
