package schema

import (
	"bytes"
	"fmt"
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

// capturedSchemaURL is a fixed, opaque base URL for a captured schema document. It deliberately does
// NOT embed the schema's project path: a base like "mem:///./schemas/input.json" would make the
// compiler normalize a same-document "#/$defs/..." ref to a *different* URL than the one registered,
// miss the in-memory resource, and fall through to the file loader. A stable opaque URL keeps every
// same-document ref inside the registered document.
const capturedSchemaURL = "mem://terfyn/captured-schema"

// noExternalLoader refuses to load any URL. A captured schema must be self-contained: following an
// external reference (a file://, http://, or another-document $ref) would re-read live state and
// defeat the drift-immunity that capturing the bytes exists to provide.
type noExternalLoader struct{}

func (noExternalLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference %q is not permitted in a captured schema (must be self-contained)", url)
}

// ValidateContent compiles a JSON Schema from schemaContent (rather than a file path) and validates
// instance against it (pinned-resume schema capture, issue #207 follow-up). ref is only an error
// label. The schema is compiled in isolation: it is registered under a fixed opaque URL and the
// compiler's loader cannot open files, so a same-document "#/$defs/..." ref resolves within the
// captured bytes and any external ref is a loud compile error — never a disk read. A pinned run
// therefore validates against exactly the bytes captured in its deployment snapshot.
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
	c.UseLoader(noExternalLoader{})
	if err := c.AddResource(capturedSchemaURL, doc); err != nil {
		return &CompileError{Path: label, Err: err}
	}
	sch, err := c.Compile(capturedSchemaURL)
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
