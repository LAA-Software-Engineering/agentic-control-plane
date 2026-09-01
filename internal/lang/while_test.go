package lang

import "testing"

func firstWorkflow(t *testing.T, src string) (*WorkflowDecl, Diagnostics) {
	t.Helper()
	f, diags := Parse("w.agent", src)
	for _, d := range f.Decls {
		if wf, ok := d.(*WorkflowDecl); ok {
			return wf, diags
		}
	}
	return nil, diags
}

func TestParseWhile_Valid(t *testing.T) {
	src := "workflow W(input: S) -> S {\n" +
		"    state = input\n" +
		"    while !state.approved limit 3 {\n" +
		"        state = Review(state)\n" +
		"    }\n" +
		"    return state\n" +
		"}\n"
	wf, diags := firstWorkflow(t, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %s", diags.Error())
	}
	var w *WhileStmt
	for _, s := range wf.Body {
		if ws, ok := s.(*WhileStmt); ok {
			w = ws
		}
	}
	if w == nil {
		t.Fatalf("no WhileStmt parsed")
	}
	if w.Limit != 3 {
		t.Fatalf("limit: got %d, want 3", w.Limit)
	}
	if len(w.Body) != 1 {
		t.Fatalf("body: got %d stmts, want 1", len(w.Body))
	}
}

func TestParseWhile_InvalidLimits(t *testing.T) {
	cases := map[string]string{
		"missing limit":  "workflow W() {\n    while cond {\n    }\n}\n",
		"zero limit":     "workflow W() {\n    while cond limit 0 {\n    }\n}\n",
		"negative limit": "workflow W() {\n    while cond limit -3 {\n    }\n}\n",
		"fractional":     "workflow W() {\n    while cond limit 3.5 {\n    }\n}\n",
		"dynamic bound":  "workflow W(input: S) {\n    while cond limit input.max {\n    }\n}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, diags := Parse("w.agent", src)
			if len(diags) == 0 {
				t.Fatalf("expected a diagnostic for %s, got none", name)
			}
		})
	}
}

func TestParseWhile_Nesting(t *testing.T) {
	// while inside if, if inside while, for inside while, and nested while all parse.
	ok := "workflow W(input: S) {\n" +
		"    state = input\n" +
		"    while !state.done limit 3 {\n" +
		"        if state.retry {\n" +
		"            state = A(state)\n" +
		"        }\n" +
		"        for x in state.items {\n" +
		"            B(x)\n" +
		"        }\n" +
		"        while !state.inner limit 2 {\n" +
		"            state = C(state)\n" +
		"        }\n" +
		"    }\n" +
		"    return state\n" +
		"}\n"
	_, diags := Parse("w.agent", ok)
	if len(diags) != 0 {
		t.Fatalf("nesting should parse cleanly, got: %s", diags.Error())
	}
	// while inside if:
	wInIf := "workflow W(input: S) {\n" +
		"    state = input\n" +
		"    if input.go {\n" +
		"        while !state.done limit 2 {\n" +
		"            state = A(state)\n" +
		"        }\n" +
		"    }\n" +
		"    return state\n" +
		"}\n"
	if _, d := Parse("w.agent", wInIf); len(d) != 0 {
		t.Fatalf("while inside if should parse, got: %s", d.Error())
	}
}

func TestWhile_FormatterRoundTrip(t *testing.T) {
	src := "workflow W(input: S) -> S {\n" +
		"    state = input\n" +
		"    while !state.approved limit 3 {\n" +
		"        state = Review(state)\n" +
		"    }\n" +
		"    return state\n" +
		"}\n"
	once, diags := Format("w.agent", src)
	if len(diags) != 0 {
		t.Fatalf("format diags: %s", diags.Error())
	}
	twice, diags2 := Format("w.agent", once)
	if len(diags2) != 0 {
		t.Fatalf("reformat diags: %s", diags2.Error())
	}
	if once != twice {
		t.Fatalf("format not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	// The canonical form carries the mandatory bound.
	if want := "while !state.approved limit 3 {"; !contains(once, want) {
		t.Fatalf("canonical output missing %q:\n%s", want, once)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
