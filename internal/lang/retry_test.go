package lang

import (
	"strings"
	"testing"
)

func TestParse_retryUntil(t *testing.T) {
	src := "workflow w(input: T) -> T {\n" +
		"    state = input\n" +
		"    retry until state.approved limit 3 {\n" +
		"        state = Impl(state)\n" +
		"    }\n" +
		"    return state\n" +
		"}\n"
	f, diags := Parse("t.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse errors: %v", diags)
	}
	wf := f.Decls[0].(*WorkflowDecl)
	var rs *RetryStmt
	for _, st := range wf.Body {
		if r, ok := st.(*RetryStmt); ok {
			rs = r
		}
	}
	if rs == nil {
		t.Fatal("RetryStmt not parsed")
	}
	if rs.Limit != 3 || len(rs.Body) != 1 {
		t.Fatalf("retry = limit %d, body %d", rs.Limit, len(rs.Body))
	}
	out := Print(f)
	if !strings.Contains(out, "retry until state.approved limit 3 {") {
		t.Fatalf("printer did not round-trip retry:\n%s", out)
	}
	f2, d2 := Parse("t.agent", out)
	if d2.HasErrors() || Print(f2) != out {
		t.Fatalf("retry is not print-idempotent:\n%s", out)
	}
}

// `retry` and `until` are contextual: only the `retry until` shape is the construct, so `retry`
// stays usable as an ordinary binding name.
func TestParse_retryAsVariable(t *testing.T) {
	f, diags := Parse("t.agent", "workflow w() -> T {\n    retry = Foo()\n    return retry\n}\n")
	if diags.HasErrors() {
		t.Fatalf("`retry` as a variable must still parse: %v", diags)
	}
	if _, ok := f.Decls[0].(*WorkflowDecl).Body[0].(*AssignStmt); !ok {
		t.Fatalf("expected an assignment for `retry = ...`, got %T", f.Decls[0].(*WorkflowDecl).Body[0])
	}
}

func TestParse_retryUntilRequiresLimit(t *testing.T) {
	_, diags := Parse("t.agent", "workflow w() -> T {\n    retry until x {\n        y = Foo()\n    }\n}\n")
	if !diags.HasErrors() {
		t.Fatal("retry without a limit must be a diagnostic (an unbounded retry is not allowed)")
	}
}
