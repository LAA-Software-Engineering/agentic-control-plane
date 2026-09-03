package lang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseValidCorpus parses every program under testdata/valid and asserts it
// produces no diagnostics, that re-parsing rendered output is idempotent
// (round-trip), and that every AST node carries a position.
func TestParseValidCorpus(t *testing.T) {
	dir := filepath.Join("testdata", "valid")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus dir: %v", err)
	}
	var seen int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".agent") {
			continue
		}
		seen++
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			file, diags := Parse(name, string(src))
			if len(diags) != 0 {
				t.Fatalf("valid program produced %d diagnostic(s):\n%s", len(diags), diags.Error())
			}
			if len(file.Decls) == 0 {
				t.Fatalf("expected at least one declaration")
			}

			// Every node carries a position (acceptance criterion).
			forEachNode(file, func(n Node) {
				if p := n.Position(); p.Line <= 0 || p.Column <= 0 {
					t.Errorf("node %T has no position: %+v", n, p)
				}
				if p := n.Position(); p.File != name {
					t.Errorf("node %T has file %q, want %q", n, p.File, name)
				}
			})

			// Round-trip: rendering and re-parsing is idempotent.
			once := render(file)
			file2, diags2 := Parse(name, once)
			if len(diags2) != 0 {
				t.Fatalf("re-parsing rendered output produced diagnostics:\n%s\nrendered:\n%s", diags2.Error(), once)
			}
			twice := render(file2)
			if once != twice {
				t.Errorf("round-trip not idempotent:\n--- first ---\n%s\n--- second ---\n%s", once, twice)
			}
		})
	}
	if seen == 0 {
		t.Fatal("no .agent fixtures found in corpus")
	}
}

// TestParseADR002Structure asserts the AST shape of the normative program so a
// regression in field wiring is caught, not just a diagnostic count.
func TestParseADR002Structure(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "valid", "adr002.agent"))
	if err != nil {
		t.Fatal(err)
	}
	file, diags := Parse("adr002.agent", string(src))
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics:\n%s", diags.Error())
	}
	if len(file.Decls) != 2 {
		t.Fatalf("want 2 decls, got %d", len(file.Decls))
	}

	agent, ok := file.Decls[0].(*AgentDecl)
	if !ok {
		t.Fatalf("decl 0: want *AgentDecl, got %T", file.Decls[0])
	}
	if agent.Name.Name != "Reviewer" {
		t.Errorf("agent name = %q, want Reviewer", agent.Name.Name)
	}
	if agent.Model == nil || agent.Model.Provider != "openai" || agent.Model.Name != "gpt-5" {
		t.Errorf("model = %+v, want openai/gpt-5", agent.Model)
	}
	if agent.Policy == nil || agent.Policy.Name != "guarded-writes" {
		t.Errorf("policy = %+v, want guarded-writes", agent.Policy)
	}
	if len(agent.Grants) != 2 {
		t.Fatalf("want 2 grants, got %d", len(agent.Grants))
	}
	if g := agent.Grants[0]; g.ToolName() != "github" || g.OperationName() != "read_pr" {
		t.Errorf("grant 0 = %s.%s, want github.read_pr", g.ToolName(), g.OperationName())
	}
	if agent.Input == nil || agent.Input.Name != "ReviewRequest" {
		t.Errorf("input = %+v, want ReviewRequest", agent.Input)
	}
	if agent.Output == nil || agent.Output.Name != "Review" {
		t.Errorf("output = %+v, want Review", agent.Output)
	}

	wf, ok := file.Decls[1].(*WorkflowDecl)
	if !ok {
		t.Fatalf("decl 1: want *WorkflowDecl, got %T", file.Decls[1])
	}
	if wf.Name.Name != "PRReview" {
		t.Errorf("workflow name = %q, want PRReview", wf.Name.Name)
	}
	if len(wf.Params) != 1 || wf.Params[0].Name.Name != "input" || wf.Params[0].Type.Name != "PullRequest" {
		t.Errorf("params = %+v, want (input: PullRequest)", wf.Params)
	}
	if wf.Result == nil || wf.Result.Name != "Review" {
		t.Errorf("result = %+v, want Review", wf.Result)
	}
	wantEffects := []string{"github.read", "github.write", "external.visible"}
	if len(wf.Effects) != len(wantEffects) {
		t.Fatalf("want %d effects, got %d", len(wantEffects), len(wf.Effects))
	}
	for i, w := range wantEffects {
		if wf.Effects[i].Name != w {
			t.Errorf("effect %d = %q, want %q", i, wf.Effects[i].Name, w)
		}
	}

	// Body: assign, parallel (3 branches), assign, expr-stmt call, return.
	if len(wf.Body) != 5 {
		t.Fatalf("want 5 body statements, got %d", len(wf.Body))
	}
	if _, ok := wf.Body[0].(*AssignStmt); !ok {
		t.Errorf("stmt 0: want *AssignStmt, got %T", wf.Body[0])
	}
	par, ok := wf.Body[1].(*ParallelStmt)
	if !ok {
		t.Fatalf("stmt 1: want *ParallelStmt, got %T", wf.Body[1])
	}
	if len(par.Body) != 3 {
		t.Errorf("parallel: want 3 branches, got %d", len(par.Body))
	}
	if _, ok := wf.Body[3].(*ExprStmt); !ok {
		t.Errorf("stmt 3: want *ExprStmt, got %T", wf.Body[3])
	}
	if _, ok := wf.Body[4].(*ReturnStmt); !ok {
		t.Errorf("stmt 4: want *ReturnStmt, got %T", wf.Body[4])
	}
}

// TestParseGrantOperations asserts a grant splits the same way as
// tools.ParseUses: tool name is the first segment after "tool", and the
// operation is the remainder (possibly dotted). A frozen three-segment grammar
// would reject the multi-segment operations this repository already ships.
func TestParseGrantOperations(t *testing.T) {
	src := "agent A {\n" +
		"    grants {\n" +
		"        tool.github.pull_request.post_comment\n" +
		"        tool.api.post.users\n" +
		"        tool.helper.echo\n" +
		"    }\n" +
		"}\n"
	file, diags := Parse("g.agent", src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics:\n%s", diags.Error())
	}
	grants := file.Decls[0].(*AgentDecl).Grants
	want := []struct{ tool, op string }{
		{"github", "pull_request.post_comment"},
		{"api", "post.users"},
		{"helper", "echo"},
	}
	if len(grants) != len(want) {
		t.Fatalf("want %d grants, got %d", len(want), len(grants))
	}
	for i, w := range want {
		if grants[i].ToolName() != w.tool || grants[i].OperationName() != w.op {
			t.Errorf("grant %d = %s / %s, want %s / %s", i, grants[i].ToolName(), grants[i].OperationName(), w.tool, w.op)
		}
	}
}

// TestParseErrors is a table of malformed inputs asserting the parser recovers
// and reports diagnostics at the expected positions and with the expected
// messages (acceptance criterion: multiple positioned diagnostics).
func TestParseErrors(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag []wantDiag // each must be matched (by line/col + message substring)
		minCount int        // minimum total diagnostics expected
	}{
		{
			name:     "unknown agent field",
			src:      "agent A {\n    modle openai/gpt-5\n}\n",
			wantDiag: []wantDiag{{line: 2, col: 5, msg: "unknown agent field"}},
			minCount: 1,
		},
		{
			name:     "grant missing tool prefix",
			src:      "agent A {\n    grants {\n        github.read_pr\n    }\n}\n",
			wantDiag: []wantDiag{{line: 3, col: 9, msg: "is not a grant"}},
			minCount: 1,
		},
		{
			name:     "grant missing operation",
			src:      "agent A {\n    grants {\n        tool.github\n    }\n}\n",
			wantDiag: []wantDiag{{line: 3, col: 9, msg: "grant must be tool.<name>.<operation>"}},
			minCount: 1,
		},
		{
			name:     "duplicate agent field",
			src:      "agent A {\n    input Foo\n    input Bar\n}\n",
			wantDiag: []wantDiag{{line: 3, col: 5, msg: "duplicate agent field \"input\""}},
			minCount: 1,
		},
		{
			name:     "effect with tool prefix",
			src:      "workflow W(x: T) effects { tool.github.read } {\n    return x\n}\n",
			wantDiag: []wantDiag{{line: 1, col: 28, msg: "looks like a grant"}},
			minCount: 1,
		},
		{
			name:     "model missing slash",
			src:      "agent A {\n    model gpt5\n}\n",
			wantDiag: []wantDiag{{line: 3, col: 1, msg: "expected '/' in model reference"}},
			minCount: 1,
		},
		{
			name:     "stray character is lexer error",
			src:      "agent A {\n    model openai/gpt-5\n}\n@\n",
			wantDiag: []wantDiag{{line: 4, col: 1, msg: "unexpected character"}},
			minCount: 1,
		},
		{
			name: "multiple errors recover across declarations",
			src: "agent A {\n    modle x\n}\n" +
				"workflow W(x: T) {\n    y = = foo(x)\n    return y\n}\n",
			wantDiag: []wantDiag{
				{line: 2, col: 5, msg: "unknown agent field"},
			},
			minCount: 2, // at least the bad field and the bad statement
		},
		{
			name:     "top-level junk before declaration",
			src:      "banana\nagent A {\n    input X\n}\n",
			wantDiag: []wantDiag{{line: 1, col: 1, msg: "expected 'agent', 'workflow', 'tool', 'policy', 'environment', 'provider', or 'defaults'"}},
			minCount: 1,
		},
		{
			name:     "parallel branch not an assignment",
			src:      "workflow W(x: T) {\n    parallel {\n        foo(x)\n    }\n}\n",
			wantDiag: []wantDiag{{line: 3, col: 9, msg: "parallel branches must be assignments"}},
			minCount: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := Parse(tc.name, tc.src)
			if len(diags) < tc.minCount {
				t.Fatalf("want at least %d diagnostics, got %d:\n%s", tc.minCount, len(diags), diags.Error())
			}
			for _, want := range tc.wantDiag {
				if !want.matched(diags) {
					t.Errorf("no diagnostic matched %d:%d %q\ngot:\n%s", want.line, want.col, want.msg, diags.Error())
				}
			}
		})
	}
}

type wantDiag struct {
	line, col int
	msg       string
}

func (w wantDiag) matched(diags Diagnostics) bool {
	for _, d := range diags {
		if d.Pos.Line == w.line && d.Pos.Column == w.col && strings.Contains(d.Msg, w.msg) {
			return true
		}
	}
	return false
}

// forEachNode visits every node in the tree in a stable order.
func forEachNode(n Node, fn func(Node)) {
	if n == nil {
		return
	}
	// Guard typed-nil pointers: a nil *Ident/*TypeRef stored in the Node
	// interface is not == nil, so check each concrete type before use.
	switch x := n.(type) {
	case *File:
		if x == nil {
			return
		}
		fn(x)
		for _, d := range x.Decls {
			forEachNode(d, fn)
		}
	case *AgentDecl:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.Name, fn)
		if x.Model != nil {
			fn(x.Model)
		}
		forEachNode(x.Policy, fn)
		for _, g := range x.Grants {
			forEachNode(g, fn)
		}
		forEachNode(x.Input, fn)
		forEachNode(x.Output, fn)
	case *Grant:
		if x == nil {
			return
		}
		fn(x)
		for _, s := range x.Segments {
			forEachNode(s, fn)
		}
	case *WorkflowDecl:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.Name, fn)
		for _, p := range x.Params {
			forEachNode(p, fn)
		}
		forEachNode(x.Result, fn)
		for _, e := range x.Effects {
			fn(e)
		}
		for _, s := range x.Body {
			forEachNode(s, fn)
		}
	case *Param:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.Name, fn)
		forEachNode(x.Type, fn)
	case *AssignStmt:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.Target, fn)
		forEachNode(x.Value, fn)
	case *ExprStmt:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.X, fn)
	case *ParallelStmt:
		if x == nil {
			return
		}
		fn(x)
		for _, a := range x.Body {
			forEachNode(a, fn)
		}
	case *ReturnStmt:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.Value, fn)
	case *CallExpr:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.Callee, fn)
		for _, a := range x.Args {
			forEachNode(a, fn)
		}
	case *RefExpr:
		if x == nil {
			return
		}
		fn(x)
		for _, p := range x.Parts {
			forEachNode(p, fn)
		}
	case *Arg:
		if x == nil {
			return
		}
		fn(x)
		forEachNode(x.Name, fn)
		forEachNode(x.Value, fn)
	case *TypeRef:
		if x == nil {
			return
		}
		fn(x)
	case *Ident:
		if x == nil {
			return
		}
		fn(x)
	default:
		fn(n)
	}
}
