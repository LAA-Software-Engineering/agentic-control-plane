package check

import (
	"path/filepath"
	"testing"

	"github.com/Terfyn/terfyn/internal/schema"
	"github.com/Terfyn/terfyn/internal/spec"
)

// TestCheck_WorkflowInputSchemaWired proves an .agent workflow's single input parameter type is
// lowered onto the resource projection (ADR 007 parity), so the runtime validates the workflow's input
// JSON exactly as a YAML `spec.input.schema` did. The identity is arity-structural (lower.newEnv): a
// single parameter — whatever its name — is the whole input. This case uses the conventional name
// `input`.
func TestCheck_WorkflowInputSchemaWired(t *testing.T) {
	t.Parallel()
	src := `
workflow review(input: ReviewRequest) {
    return input
}
`
	wr := checkWorkflow(t, src, "review")
	if wr.Spec.Input == nil || wr.Spec.Input.Schema != "schemas/ReviewRequest.json" || wr.Spec.Input.Resolved == nil {
		t.Fatalf("workflow input schema not wired: %+v", wr.Spec.Input)
	}
}

// TestCheck_WorkflowInputSchemaWired_NonInputName is the regression that proves the fail-open is
// actually closed: the runtime input is the SOLE parameter regardless of its spelling, so a single
// parameter named `req` (not `input`) must still wire — and the wired schema must reject malformed
// input end to end (ReviewRequest has additionalProperties:false). A name-based selector would leave
// this workflow unvalidated.
func TestCheck_WorkflowInputSchemaWired_NonInputName(t *testing.T) {
	t.Parallel()
	src := `
workflow review(req: ReviewRequest) {
    return req
}
`
	wr := checkWorkflow(t, src, "review")
	if wr.Spec.Input == nil || wr.Spec.Input.Schema != "schemas/ReviewRequest.json" || wr.Spec.Input.Resolved == nil {
		t.Fatalf("a single non-`input`-named parameter is the whole input and must wire: %+v", wr.Spec.Input)
	}
	// End to end: the wired schema is the one the runtime resolves, and it rejects malformed input.
	schemaPath := filepath.Join("testdata", filepath.FromSlash(wr.Spec.Input.Schema))
	if err := schema.Validate(schemaPath, []byte(`{"bogus":true}`)); err == nil {
		t.Fatalf("wired workflow input schema must reject malformed input (fail-open not closed)")
	}
	if err := schema.Validate(schemaPath, []byte(`{"repo":"acme/x","number":7}`)); err != nil {
		t.Fatalf("valid input must pass the wired schema, got %v", err)
	}
}

// TestCheck_WorkflowMultiParam_NotWired proves the arity rule: a workflow with MULTIPLE parameters
// takes a synthesized composite `{a, b}` input for which no single author-provided schema exists, so
// WorkflowSpec.Input stays nil — the runtime must never validate the composite against one field's
// schema (which a name-based selector picking a param named `input` would wrongly do).
func TestCheck_WorkflowMultiParam_NotWired(t *testing.T) {
	t.Parallel()
	src := `
workflow combine(input: ReviewRequest, other: Review) {
    return input
}
`
	wr := checkWorkflow(t, src, "combine")
	if wr.Spec.Input != nil {
		t.Fatalf("a multi-parameter workflow has no single input schema; must stay nil, got %+v", wr.Spec.Input)
	}
}

// TestCheck_WorkflowInputSchemaMissingStaysUntyped keeps the checker's leniency (mirrors the agent
// rule): a sole parameter whose type ref has no schema file leaves WorkflowSpec.Input nil, so a typed
// input with no schema file is not forced to fail schema-file validation.
func TestCheck_WorkflowInputSchemaMissingStaysUntyped(t *testing.T) {
	t.Parallel()
	src := `
workflow loose(input: NoSuchType) {
    return input
}
`
	wr := checkWorkflow(t, src, "loose")
	if wr.Spec.Input != nil {
		t.Fatalf("an unresolved input type must stay untyped, got %+v", wr.Spec.Input)
	}
}

// checkWorkflow checks src (SchemaDir=testdata), fails on diagnostics, and returns the named workflow's
// resource projection (also asserting the wired graph validates cleanly).
func checkWorkflow(t *testing.T, src, name string) *spec.WorkflowResource {
	t.Helper()
	f := parseOrFatal(t, src)
	prog, diags := Check(f, Options{SchemaDir: "testdata"})
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diagMessages(diags))
	}
	wr := prog.Graph.Workflows[name]
	if wr == nil {
		t.Fatalf("no %s workflow in the projection", name)
	}
	if errs := spec.ValidateProjectGraph(prog.Graph, "testdata"); errs != nil {
		t.Fatalf("wired workflow graph should validate, got %v", errs)
	}
	return wr
}
