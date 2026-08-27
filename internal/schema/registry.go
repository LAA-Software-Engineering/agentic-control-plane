package schema

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Registry caches compiled schemas by absolute path for repeated validation.
type Registry struct {
	mu       sync.Mutex
	compiler *jsonschema.Compiler
	compiled map[string]*jsonschema.Schema
}

// NewRegistry constructs an empty registry with a dedicated compiler.
func NewRegistry() *Registry {
	return &Registry{
		compiler: newCompiler(),
		compiled: make(map[string]*jsonschema.Schema),
	}
}

func (r *Registry) getOrCompile(abs string) (*jsonschema.Schema, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sch, ok := r.compiled[abs]; ok {
		return sch, nil
	}
	sch, err := r.compiler.Compile(abs)
	if err != nil {
		return nil, err
	}
	r.compiled[abs] = sch
	return sch, nil
}

// ValidateContent compiles a JSON Schema from schemaContent (rather than a file path) and validates
// instance against it (issue: pinned-resume schema capture). ref is a stable label used for the
// synthetic resource URL and in error paths. A pinned run resumes against the schema bytes captured
// in its deployment snapshot, so it never re-reads a possibly-changed schema file on disk.
//
// Self-contained schemas only: an external `$ref` to another file is not resolved from bytes.
func ValidateContent(ref string, schemaContent, instance []byte) error {
	label := strings.TrimSpace(ref)
	if label == "" {
		label = "schema"
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaContent))
	if err != nil {
		return &CompileError{Path: label, Err: err}
	}
	c := newCompiler()
	url := "mem:///" + label
	if err := c.AddResource(url, doc); err != nil {
		return &CompileError{Path: label, Err: err}
	}
	sch, err := c.Compile(url)
	if err != nil {
		return &CompileError{Path: label, Err: err}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(instance))
	if err != nil {
		return &InstanceError{Path: label, Err: err}
	}
	if err := sch.Validate(inst); err != nil {
		return &ValidationError{Path: label, Err: err}
	}
	return nil
}

// Validate compiles the schema at schemaPath (if needed), parses instance as JSON, and validates.
// schemaPath may be relative; it is resolved with filepath.Abs before open/compile.
func (r *Registry) Validate(schemaPath string, instance []byte) error {
	abs, err := filepath.Abs(filepath.Clean(schemaPath))
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return &FileError{Path: abs, Op: "stat schema", Err: err}
	}
	sch, err := r.getOrCompile(abs)
	if err != nil {
		return &CompileError{Path: abs, Err: err}
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(instance))
	if err != nil {
		return &InstanceError{Path: abs, Err: err}
	}
	if err := sch.Validate(inst); err != nil {
		return &ValidationError{Path: abs, Err: err}
	}
	return nil
}
