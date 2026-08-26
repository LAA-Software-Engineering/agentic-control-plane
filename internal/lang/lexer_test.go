package lang

import "testing"

// collect drains the lexer into a slice (excluding the terminating EOF).
func collect(src string) ([]Token, Diagnostics) {
	l := NewLexer("t.agent", src)
	var toks []Token
	for {
		tok := l.Next()
		if tok.Kind == KindEOF {
			break
		}
		toks = append(toks, tok)
	}
	return toks, l.Diagnostics()
}

func TestLexerTokens(t *testing.T) {
	src := "agent Reviewer { model openai/gpt-5 } -> return , : = ."
	toks, diags := collect(src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lex diagnostics: %s", diags.Error())
	}
	want := []struct {
		kind Kind
		lit  string
	}{
		{KindAgent, "agent"},
		{KindIdent, "Reviewer"},
		{KindLBrace, "{"},
		{KindIdent, "model"}, // 'model' is contextual, not reserved: an IDENT
		{KindIdent, "openai"},
		{KindSlash, "/"},
		{KindIdent, "gpt-5"}, // hyphen stays inside the identifier
		{KindRBrace, "}"},
		{KindArrow, "->"},
		{KindReturn, "return"},
		{KindComma, ","},
		{KindColon, ":"},
		{KindEquals, "="},
		{KindDot, "."},
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %v", len(toks), len(want), toks)
	}
	for i, w := range want {
		if toks[i].Kind != w.kind || toks[i].Lit != w.lit {
			t.Errorf("token %d = %s, want %s(%q)", i, toks[i], w.kind, w.lit)
		}
	}
}

func TestLexerPositions(t *testing.T) {
	// Line 2, column 5 for the 'model' token after 4 spaces of indent.
	toks, _ := collect("agent A {\n    model x/y\n}")
	var model Token
	for _, tk := range toks {
		if tk.Lit == "model" {
			model = tk
			break
		}
	}
	if model.Pos.Line != 2 || model.Pos.Column != 5 {
		t.Errorf("model at %d:%d, want 2:5", model.Pos.Line, model.Pos.Column)
	}
	if model.Pos.File != "t.agent" {
		t.Errorf("file = %q, want t.agent", model.Pos.File)
	}
}

func TestLexerSkipsComments(t *testing.T) {
	toks, diags := collect("agent // this is a comment\nA")
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
	if len(toks) != 2 || toks[0].Kind != KindAgent || toks[1].Lit != "A" {
		t.Fatalf("comment not skipped: %v", toks)
	}
	// The identifier after the comment is on line 2.
	if toks[1].Pos.Line != 2 {
		t.Errorf("ident line = %d, want 2", toks[1].Pos.Line)
	}
}

func TestLexerStrayRune(t *testing.T) {
	toks, diags := collect("agent @ A")
	if len(diags) != 1 {
		t.Fatalf("want 1 diagnostic, got %d: %s", len(diags), diags.Error())
	}
	if diags[0].Pos.Column != 7 {
		t.Errorf("stray rune at column %d, want 7", diags[0].Pos.Column)
	}
	// Lexing still recovers: the KindError token is emitted and scanning
	// continues to the trailing identifier.
	if got := toks[len(toks)-1]; got.Kind != KindIdent || got.Lit != "A" {
		t.Errorf("last token = %s, want identifier A", got)
	}
}
