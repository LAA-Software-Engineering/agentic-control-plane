package lang

import "testing"

// TestDiagnosticsAsError guards the exact failure mode a #198 code review
// found: Diagnostics is a slice-based error implementation, so a bare
// interface conversion, len(ds) != 0, or fmt.Errorf("%w", ds) all report
// "failed" for a warning-only Diagnostics regardless of what Error()'s string
// says. AsError is the one safe conversion, and this test pins its contract.
func TestDiagnosticsAsError(t *testing.T) {
	warningOnly := Diagnostics{{Msg: "over-broad", Severity: SeverityWarning}}
	if warningOnly.HasErrors() {
		t.Fatalf("a warning-only Diagnostics must not report HasErrors")
	}
	if err := warningOnly.AsError(); err != nil {
		t.Fatalf("AsError must be nil for a warning-only Diagnostics, got %v", err)
	}
	// The classic footgun this guards against: len()/interface-truthiness says
	// "non-empty", but that is not the same question as "did this fail".
	if len(warningOnly) == 0 {
		t.Fatalf("test fixture must be non-empty to exercise the footgun")
	}

	withError := Diagnostics{
		{Msg: "over-broad", Severity: SeverityWarning},
		{Msg: "exceeds clause", Severity: SeverityError},
	}
	if !withError.HasErrors() {
		t.Fatalf("expected HasErrors to be true once an error diagnostic is present")
	}
	err := withError.AsError()
	if err == nil {
		t.Fatalf("expected a non-nil error once an error diagnostic is present")
	}
	if err.Error() == "" {
		t.Fatalf("expected AsError's message to render diagnostics")
	}
}

func TestDiagnosticErrorPrefixesWarnings(t *testing.T) {
	d := Diagnostic{Msg: "declares an unreachable effect", Severity: SeverityWarning}
	if got := d.Error(); got != "warning: declares an unreachable effect" {
		t.Fatalf("got %q", got)
	}
	e := Diagnostic{Msg: "exceeds the clause"}
	if got := e.Error(); got != "exceeds the clause" {
		t.Fatalf("SeverityError (zero value) must not be prefixed, got %q", got)
	}
}
