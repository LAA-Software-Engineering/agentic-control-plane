package check

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/spec"
)

// TestCheck_WorkflowInputSchemaWired proves an .agent workflow's `input` parameter type is lowered
// onto the resource projection (ADR 007 parity): a resolved type carries the project-root-relative
// Schema ref (schemas/<Name>.json) AND the compiled document onto WorkflowSpec.Input, so the runtime
// validates the workflow's input JSON exactly as a YAML `spec.input.schema` did. Before this, .agent
// workflows silently skipped runtime input validation.
func TestCheck_WorkflowInputSchemaWired(t *testing.T) {
	t.Parallel()
	src := `
workflow review(input: ReviewRequest) {
    return input
}
`
	f := parseOrFatal(t, src)
	prog, diags := Check(f, Options{SchemaDir: "testdata"})
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %v", diagMessages(diags))
	}
	wr := prog.Graph.Workflows["review"]
	if wr == nil {
		t.Fatalf("no review workflow in the projection")
	}
	if wr.Spec.Input == nil || wr.Spec.Input.Schema != "schemas/ReviewRequest.json" {
		t.Fatalf("workflow input schema not wired: %+v", wr.Spec.Input)
	}
	if wr.Spec.Input.Resolved == nil {
		t.Fatalf("workflow input schema ref present but not resolved to a compiled document")
	}

	// The wired ref resolves cleanly under full validation, i.e. validate/runtime enforce input I/O.
	if errs := spec.ValidateProjectGraph(prog.Graph, "testdata"); errs != nil {
		t.Fatalf("wired workflow input schema should validate, got %v", errs)
	}
}

// TestCheck_WorkflowInputSchemaMissingStaysUntyped keeps the checker's leniency (mirrors the agent
// rule): an `input` parameter whose type ref has no schema file leaves WorkflowSpec.Input nil, so a
// typed input with no schema file is not forced to fail schema-file validation.
func TestCheck_WorkflowInputSchemaMissingStaysUntyped(t *testing.T) {
	t.Parallel()
	src := `
workflow loose(input: NoSuchType) {
    return input
}
`
	f := parseOrFatal(t, src)
	prog, diags := Check(f, Options{SchemaDir: "testdata"})
	if diags.HasErrors() {
		t.Fatalf("a missing workflow input schema must not be an error (leniency), got %v", diagMessages(diags))
	}
	wr := prog.Graph.Workflows["loose"]
	if wr == nil {
		t.Fatalf("no loose workflow")
	}
	if wr.Spec.Input != nil {
		t.Fatalf("an unresolved input type must stay untyped, got %+v", wr.Spec.Input)
	}
}
