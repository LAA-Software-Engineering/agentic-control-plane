package schema

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/util"
)

// ResolveSchemaPath joins schemaRef to projectRoot for Agent/Workflow schema fields
// (paths like ./schemas/input.json in design doc §7.2, §7.4).
//
// Rule: all paths are resolved under projectRoot. Absolute schemaRef is allowed only
// if it still lies under projectRoot after filepath.Clean (same containment check as
// relative joins). This matches a single-root workspace layout.
func ResolveSchemaPath(projectRoot, schemaRef string) (string, error) {
	projectRoot = filepath.Clean(projectRoot)
	schemaRef = strings.TrimSpace(schemaRef)
	if schemaRef == "" {
		return "", fmt.Errorf("empty schema path")
	}
	var full string
	if filepath.IsAbs(schemaRef) {
		full = filepath.Clean(schemaRef)
	} else {
		full = filepath.Join(projectRoot, filepath.FromSlash(schemaRef))
		full = filepath.Clean(full)
	}
	if !util.IsUnderRoot(projectRoot, full) {
		return "", fmt.Errorf("schema path %q resolves outside project root %q", schemaRef, projectRoot)
	}
	return full, nil
}

// FileError is returned when the schema file cannot be read or does not exist.
type FileError struct {
	Path string
	Op   string
	Err  error
}

func (e *FileError) Error() string {
	return fmt.Sprintf("%s %q: %v", e.Op, e.Path, e.Err)
}

func (e *FileError) Unwrap() error { return e.Err }

// CompileError wraps schema compilation failures (invalid schema document).
type CompileError struct {
	Path string
	Err  error
}

func (e *CompileError) Error() string {
	return fmt.Sprintf("compile JSON Schema %q: %v", e.Path, e.Err)
}

func (e *CompileError) Unwrap() error { return e.Err }

// InstanceError means instance bytes are not valid JSON.
type InstanceError struct {
	Path string
	Err  error
}

func (e *InstanceError) Error() string {
	return fmt.Sprintf("parse JSON instance for schema %q: %v", e.Path, e.Err)
}

func (e *InstanceError) Unwrap() error { return e.Err }

// ValidationError means the instance does not satisfy the schema.
type ValidationError struct {
	Path string
	Err  error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("JSON Schema validation failed (%q): %v", e.Path, e.Err)
}

func (e *ValidationError) Unwrap() error { return e.Err }

var defaultReg = NewRegistry()

// Validate loads the schema at schemaPath, parses instance as JSON, and validates.
// schemaPath is cleaned and passed through filepath.Abs. Results are easier to reason
// about if callers pass the path returned by ResolveSchemaPath.
//
// Compiled schemas are cached in a package-level registry (idempotent for the same path).
func Validate(schemaPath string, instance []byte) error {
	return defaultReg.Validate(schemaPath, instance)
}
