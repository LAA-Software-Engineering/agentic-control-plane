package lang

import "testing"

// lexOne returns the first token's literal and any diagnostics for src.
func lexOne(src string) (Token, Diagnostics) {
	l := NewLexer("t.agent", src)
	return l.Next(), l.Diagnostics()
}

func TestSingleLineStringsUnchanged(t *testing.T) {
	// Backward compatibility: `"..."` with escapes still decodes as before, and a
	// two-quote empty string is not mistaken for the start of a triple quote.
	cases := map[string]string{
		`"hello"`:       "hello",
		`"a\nb"`:        "a\nb",
		`""`:            "",
		`"quote:\"x\""`: `quote:"x"`,
	}
	for src, want := range cases {
		tok, diags := lexOne(src)
		if len(diags) != 0 {
			t.Fatalf("%s: unexpected diags: %s", src, diags.Error())
		}
		if tok.Kind != KindString || tok.Lit != want {
			t.Fatalf("%s: got (%v, %q), want (KindString, %q)", src, tok.Kind, tok.Lit, want)
		}
	}
}

func TestMultilineNormalization(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "canonical indentation + blank line",
			src:  "\"\"\"\n    line one\n\n    line two\n\"\"\"",
			want: "line one\n\nline two",
		},
		{
			name: "closing delimiter on its own indented line",
			src:  "\"\"\"\n        hello\n        world\n    \"\"\"",
			want: "hello\nworld",
		},
		{
			name: "relative indentation preserved",
			src:  "\"\"\"\n    if x:\n        do()\n\"\"\"",
			want: "if x:\n    do()",
		},
		{
			name: "single line triple quoted",
			src:  `"""abc"""`,
			want: "abc",
		},
		{
			name: "empty multiline",
			src:  "\"\"\"\n\"\"\"",
			want: "",
		},
		{
			name: "raw: backslashes and braces are literal (no escapes, no interpolation)",
			src:  "\"\"\"\n    a\\nb ${x}\n\"\"\"",
			want: "a\\nb ${x}",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok, diags := lexOne(tc.src)
			if len(diags) != 0 {
				t.Fatalf("unexpected diags: %s", diags.Error())
			}
			if tok.Kind != KindString {
				t.Fatalf("kind: got %v, want KindString", tok.Kind)
			}
			if tok.Lit != tc.want {
				t.Fatalf("normalized value:\n got %q\nwant %q", tok.Lit, tc.want)
			}
		})
	}
}

func TestMultilineUnterminated(t *testing.T) {
	_, diags := lexOne("\"\"\"\nno closing delimiter\n")
	if len(diags) == 0 {
		t.Fatalf("want a diagnostic for an unterminated multiline string")
	}
}

func TestInstructionsParse(t *testing.T) {
	src := "agent A {\n    model mock/gpt-4\n    instructions \"\"\"\n    Do the work.\n\n    Then stop.\n    \"\"\"\n}\n"
	file, diags := Parse("a.agent", src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %s", diags.Error())
	}
	a := file.Decls[0].(*AgentDecl)
	if a.Instructions == nil {
		t.Fatalf("instructions not parsed")
	}
	if got, want := a.Instructions.Value, "Do the work.\n\nThen stop."; got != want {
		t.Fatalf("instructions value:\n got %q\nwant %q", got, want)
	}
}

func TestInstructionsSingleLine(t *testing.T) {
	file, diags := Parse("a.agent", "agent A {\n    instructions \"be brief\"\n}\n")
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %s", diags.Error())
	}
	if got := file.Decls[0].(*AgentDecl).Instructions.Value; got != "be brief" {
		t.Fatalf("value: got %q", got)
	}
}

func TestDuplicateInstructionsDiagnostic(t *testing.T) {
	src := "agent A {\n    instructions \"first\"\n    instructions \"second\"\n}\n"
	file, diags := Parse("a.agent", src)
	if len(diags) == 0 {
		t.Fatalf("want a duplicate-field diagnostic")
	}
	// First occurrence wins (no written declaration dropped silently).
	if got := file.Decls[0].(*AgentDecl).Instructions.Value; got != "first" {
		t.Fatalf("first occurrence must win, got %q", got)
	}
}

func TestInstructionsFormatterRoundTrip(t *testing.T) {
	src := "agent A {\n    model mock/gpt-4\n    instructions \"\"\"\n    line one\n\n        indented\n    line two\n    \"\"\"\n}\n"
	once, diags := Format("a.agent", src)
	if len(diags) != 0 {
		t.Fatalf("format diags: %s", diags.Error())
	}
	// Idempotent: a second format is a no-op.
	twice, diags2 := Format("a.agent", once)
	if len(diags2) != 0 {
		t.Fatalf("reformat diags: %s", diags2.Error())
	}
	if once != twice {
		t.Fatalf("format not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	// Semantics preserved: the reparsed value equals the original value.
	f1, _ := Parse("a.agent", src)
	f2, _ := Parse("a.agent", once)
	v1 := f1.Decls[0].(*AgentDecl).Instructions.Value
	v2 := f2.Decls[0].(*AgentDecl).Instructions.Value
	if v1 != v2 {
		t.Fatalf("round-trip changed the value:\n got %q\nwant %q", v2, v1)
	}
}
