package lang

import (
	"strings"
	"testing"
)

// TestPrint_ControlFlowIdempotent proves the printer is idempotent over the
// #199 surface (conditionals, loops, dynamic fan-out, and the expression
// language): parse -> Print -> parse -> Print is stable, and the printed output
// re-parses without error.
func TestPrint_ControlFlowIdempotent(t *testing.T) {
	t.Parallel()
	src := `
agent Reviewer {
    model openai/gpt-5
    grants {
        tool.github.read_pr
    }
    input PullRequest
    output Review
}

workflow Ship(input: Batch) -> Report effects { github.read, github.write } {
    pr = github.get_pr(input.repo)
    if input.n >= 10 && !input.done {
        big = github.merge_pr(input.repo, tag: "release", count: 3)
    } else if input.n == 0 {
        small = github.get_pr()
    } else {
        github.noop()
    }
    for repo in input.repos {
        github.build(repo)
    }
    parallel for item in input.items {
        github.scan(item)
    }
    parallel {
        sec = Reviewer(pr)
    }
    return sec
}
`
	f, diags := Parse("t.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse: %v", diags)
	}
	once := Print(f)

	f2, diags2 := Parse("t.agent", once)
	if diags2.HasErrors() {
		t.Fatalf("printed output does not re-parse cleanly:\n%s\ndiags: %v", once, diags2)
	}
	twice := Print(f2)
	if once != twice {
		t.Fatalf("Print is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}

	// Spot-check that meaningful constructs survived the round trip.
	for _, want := range []string{
		"if input.n >= 10 && !input.done {",
		"} else if input.n == 0 {",
		"for repo in input.repos {",
		"parallel for item in input.items {",
		`tag: "release"`,
		"count: 3",
	} {
		if !strings.Contains(once, want) {
			t.Fatalf("printed output missing %q:\n%s", want, once)
		}
	}
}

// TestPrint_ParenthesizesNestedBinary proves logical grouping is preserved so
// the re-parsed tree is identical.
func TestPrint_ParenthesizesNestedBinary(t *testing.T) {
	t.Parallel()
	// Explicit parens force `||` inside `&&`; the printer must keep them, since
	// dropping them would regroup the tree (&& binds tighter than ||).
	src := `
workflow W(input: X) {
    if input.a && (input.b || input.c) {
        github.noop()
    }
}
`
	f, diags := Parse("t.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse: %v", diags)
	}
	out := Print(f)
	if !strings.Contains(out, "input.a && (input.b || input.c)") {
		t.Fatalf("expected the || group to stay parenthesized under &&, got:\n%s", out)
	}
	// A naturally-tighter group needs no added parens.
	if strings.Contains(out, "(input.a)") {
		t.Fatalf("unexpected spurious parentheses:\n%s", out)
	}
	// And it must be idempotent.
	f2, _ := Parse("t.agent", out)
	if Print(f2) != out {
		t.Fatalf("not idempotent for nested binary")
	}
}

// TestPrint_ToolTransportAndPresetRoundTrip proves the .agent tool mcp/http transport blocks and the
// policy preset field survive `terfyn fmt` (parse -> print -> parse -> print is idempotent and the
// constructs are retained) — issue #440, plus the #436 preset round-trip gap.
func TestPrint_ToolTransportAndPresetRoundTrip(t *testing.T) {
	t.Parallel()
	src := `tool github {
    type mcp
    mcp {
        transport "stdio"
        command "npx"
        args { "-y" "@server" }
        headers { "Authorization" "env:GITHUB_TOKEN" }
    }
}

tool webhook {
    type http
    http {
        baseUrl "https://api.example.com"
        headers { "Authorization" "env:API_TOKEN" }
    }
}

policy p {
    preset shell_safe
}
`
	f, diags := Parse("t.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse: %v", diags)
	}
	once := Print(f)
	f2, d2 := Parse("t.agent", once)
	if d2.HasErrors() {
		t.Fatalf("printed output does not re-parse:\n%s\ndiags: %v", once, d2)
	}
	if twice := Print(f2); once != twice {
		t.Fatalf("Print is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	for _, want := range []string{
		"mcp {", `transport "stdio"`, `command "npx"`, `args { "-y" "@server" }`,
		`"Authorization" "env:GITHUB_TOKEN"`, "http {", `baseUrl "https://api.example.com"`,
		"preset shell_safe",
	} {
		if !strings.Contains(once, want) {
			t.Fatalf("printed output missing %q:\n%s", want, once)
		}
	}
}

// TestPrint_EnvironmentRoundTrip proves the .agent environment overlay block survives `terfyn fmt`
// (parse -> print -> parse -> print is idempotent and the nested constructs are retained) — #440.
func TestPrint_EnvironmentRoundTrip(t *testing.T) {
	t.Parallel()
	src := `environment prod {
    overrides {
        agents {
            reviewer {
                model anthropic/claude-sonnet-5
                constraints {
                    timeoutSeconds 300
                }
            }
        }
        policies {
            guarded-writes {
                execution {
                    maxTotalCostUsd 10
                }
                approvals {
                    requiredFor {
                        tool.workspace.run_tests
                    }
                }
            }
        }
    }
}
`
	f, diags := Parse("t.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse: %v", diags)
	}
	once := Print(f)
	f2, d2 := Parse("t.agent", once)
	if d2.HasErrors() {
		t.Fatalf("printed output does not re-parse:\n%s\ndiags: %v", once, d2)
	}
	if twice := Print(f2); once != twice {
		t.Fatalf("Print is not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
	for _, want := range []string{
		"environment prod {", "overrides {", "agents {", "reviewer {",
		"model anthropic/claude-sonnet-5", "timeoutSeconds 300",
		"policies {", "guarded-writes {", "maxTotalCostUsd 10", "tool.workspace.run_tests",
	} {
		if !strings.Contains(once, want) {
			t.Fatalf("printed output missing %q:\n%s", want, once)
		}
	}
}
