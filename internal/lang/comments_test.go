package lang

import (
	"strings"
	"testing"
)

// The issue #509 repro: every comment survives fmt, leading comments stay glued to what they
// document, and trailing inline comments stay on their line.
func TestFormat_preservesComments(t *testing.T) {
	src := `// A greeter agent. This comment documents intent.
agent greeter {
    model mock/default          // inline: the built-in mock model
    instructions """
    Say hello.
    """
}

// The default policy — conservative starting point.
policy default {
    preset shell_safe
}

// Entry workflow.
workflow hello(input: string) -> string
    policy default
{
    return greeter(input)   // dispatch to the agent
}
`
	out, diags := Format("main.agent", src)
	if len(diags) > 0 {
		t.Fatalf("diags: %s", diags.Error())
	}
	// No comment is dropped.
	if got, want := strings.Count(out, "//"), strings.Count(src, "//"); got != want {
		t.Fatalf("comment count: got %d want %d\n%s", got, want, out)
	}
	wantContains := []string{
		"// A greeter agent. This comment documents intent.\nagent greeter {",
		"model mock/default // inline: the built-in mock model",
		"// The default policy — conservative starting point.\npolicy default {",
		"// Entry workflow.\nworkflow hello",
		"return greeter(input) // dispatch to the agent",
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Fatalf("output missing %q\n--- got ---\n%s", w, out)
		}
	}
}

// Formatting is idempotent even with comments: parse -> print -> parse -> print is stable.
func TestFormat_commentsIdempotent(t *testing.T) {
	src := `// header
tool workspace {
    type native
    operations {
        // read is safe
        read_file { effects { workspace.read } } // trailing
        write_file { effects { workspace.write } }
    }
}

// policy doc
policy p {
    execution {
        maxTotalCostUsd 5
    }
    effects {
        // read only
        permit { workspace.read }
    }
}
`
	once, d1 := Format("t.agent", src)
	if len(d1) > 0 {
		t.Fatalf("diags: %s", d1.Error())
	}
	twice, d2 := Format("t.agent", once)
	if len(d2) > 0 {
		t.Fatalf("reparse diags: %s", d2.Error())
	}
	if once != twice {
		t.Fatalf("not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	if strings.Count(once, "//") != strings.Count(src, "//") {
		t.Fatalf("comment count changed: %d -> %d", strings.Count(src, "//"), strings.Count(once, "//"))
	}
}

// A blank comment (`//` with no text) and a comment after the last declaration both survive.
func TestFormat_blankAndTrailingFileComments(t *testing.T) {
	src := "// one\n//\n// three\nagent a {\n    model m/x\n}\n\n// footer after last decl\n"
	out, diags := Format("a.agent", src)
	if len(diags) > 0 {
		t.Fatalf("diags: %s", diags.Error())
	}
	if !strings.Contains(out, "//\n") {
		t.Fatalf("blank comment line dropped:\n%s", out)
	}
	if !strings.Contains(out, "// footer after last decl") {
		t.Fatalf("footer comment after last decl dropped:\n%s", out)
	}
}

// The lexer classifies own-line comments as standalone and end-of-line comments as trailing.
func TestLexer_commentClassification(t *testing.T) {
	f, diags := Parse("c.agent", "// standalone\nagent a {\n    model m/x // trailing\n}\n")
	if len(diags) > 0 {
		t.Fatalf("diags: %s", diags.Error())
	}
	if len(f.Comments) != 2 {
		t.Fatalf("got %d comments, want 2", len(f.Comments))
	}
	if !f.Comments[0].Standalone || f.Comments[0].Text != "standalone" {
		t.Fatalf("comment 0 = %+v", f.Comments[0])
	}
	if f.Comments[1].Standalone || f.Comments[1].Text != "trailing" {
		t.Fatalf("comment 1 = %+v", f.Comments[1])
	}
}
