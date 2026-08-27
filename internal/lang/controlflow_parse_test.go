package lang

import "testing"

func TestParse_ControlFlowSurface(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: PR) -> Report {
    if input.n >= 10 && !input.done {
        big = github.merge_pr(input.repo)
    } else if input.n == 0 {
        small = github.get_pr()
    } else {
        github.noop()
    }

    for repo in input.repos {
        github.build(repo, tag: "release")
    }

    parallel for item in input.items {
        github.scan(item)
    }

    return big
}
`
	f, diags := Parse("test.agent", src)
	if diags.HasErrors() {
		t.Fatalf("unexpected parse errors: %v", diags)
	}
	wd, ok := f.Decls[0].(*WorkflowDecl)
	if !ok {
		t.Fatalf("expected a workflow decl")
	}
	// Body: if, for, parallel-for, return.
	if len(wd.Body) != 4 {
		t.Fatalf("expected 4 top-level statements, got %d", len(wd.Body))
	}
	ifs, ok := wd.Body[0].(*IfStmt)
	if !ok {
		t.Fatalf("first statement should be an if, got %T", wd.Body[0])
	}
	// Condition is `input.n >= 10 && !input.done` — a BinaryExpr with && at the root.
	be, ok := ifs.Cond.(*BinaryExpr)
	if !ok || be.Op != KindAndAnd {
		t.Fatalf("condition root should be &&, got %#v", ifs.Cond)
	}
	// else if -> Else holds a single nested IfStmt.
	if len(ifs.Else) != 1 {
		t.Fatalf("expected else-if to nest one statement, got %d", len(ifs.Else))
	}
	if _, ok := ifs.Else[0].(*IfStmt); !ok {
		t.Fatalf("else-if should nest an IfStmt, got %T", ifs.Else[0])
	}
	forStmt, ok := wd.Body[1].(*ForStmt)
	if !ok || forStmt.Parallel {
		t.Fatalf("second statement should be a sequential for, got %#v", wd.Body[1])
	}
	if forStmt.Var == nil || forStmt.Var.Name != "repo" {
		t.Fatalf("loop var should be repo")
	}
	parFor, ok := wd.Body[2].(*ForStmt)
	if !ok || !parFor.Parallel {
		t.Fatalf("third statement should be a parallel for, got %#v", wd.Body[2])
	}
	if _, ok := wd.Body[3].(*ReturnStmt); !ok {
		t.Fatalf("fourth statement should be a return")
	}
}

func TestParse_ComparisonsDoNotChain(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: X) {
    if input.a < input.b < input.c {
        github.noop()
    }
}
`
	_, diags := Parse("test.agent", src)
	if !diags.HasErrors() {
		t.Fatalf("expected a non-chaining comparison error")
	}
}

func TestParse_StringAndNumberLiterals(t *testing.T) {
	t.Parallel()
	src := `
workflow W(input: X) {
    a = github.tag(name: "v1.0", count: 42, ratio: 1.5, on: true)
}
`
	f, diags := Parse("test.agent", src)
	if diags.HasErrors() {
		t.Fatalf("unexpected parse errors: %v", diags)
	}
	wd := f.Decls[0].(*WorkflowDecl)
	assign := wd.Body[0].(*AssignStmt)
	call := assign.Value.(*CallExpr)
	if len(call.Args) != 4 {
		t.Fatalf("expected 4 args, got %d", len(call.Args))
	}
	if lit, ok := call.Args[0].Value.(*LitExpr); !ok || lit.Value != "v1.0" {
		t.Fatalf("first arg should be string literal v1.0, got %#v", call.Args[0].Value)
	}
	if lit, ok := call.Args[1].Value.(*LitExpr); !ok || lit.Value != int64(42) {
		t.Fatalf("second arg should be int 42, got %#v", call.Args[1].Value)
	}
	if lit, ok := call.Args[2].Value.(*LitExpr); !ok || lit.Value != 1.5 {
		t.Fatalf("third arg should be float 1.5, got %#v", call.Args[2].Value)
	}
	if lit, ok := call.Args[3].Value.(*LitExpr); !ok || lit.Value != true {
		t.Fatalf("fourth arg should be bool true, got %#v", call.Args[3].Value)
	}
}
