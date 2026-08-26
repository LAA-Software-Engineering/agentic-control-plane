package lang

import (
	"fmt"
	"strings"
)

// render reconstructs canonical .agent source from an AST. It is a test helper
// used for round-trip idempotency checks: parse → render → parse → render must
// be stable. Output is normalized (single spaces, 4-space indent, comma-joined
// effects) so it does not depend on the incidental formatting of the input.
func render(f *File) string {
	var b strings.Builder
	for i, d := range f.Decls {
		if i > 0 {
			b.WriteString("\n")
		}
		switch n := d.(type) {
		case *AgentDecl:
			renderAgent(&b, n)
		case *WorkflowDecl:
			renderWorkflow(&b, n)
		default:
			fmt.Fprintf(&b, "/* unknown decl %T */\n", d)
		}
	}
	return b.String()
}

func renderAgent(b *strings.Builder, a *AgentDecl) {
	fmt.Fprintf(b, "agent %s {\n", identName(a.Name))
	if a.Model != nil {
		fmt.Fprintf(b, "    model %s/%s\n", a.Model.Provider, a.Model.Name)
	}
	if a.Policy != nil {
		fmt.Fprintf(b, "    policy %s\n", a.Policy.Name)
	}
	if len(a.Grants) > 0 {
		b.WriteString("    grants {\n")
		for _, g := range a.Grants {
			fmt.Fprintf(b, "        %s\n", dottedName(g.Segments))
		}
		b.WriteString("    }\n")
	}
	if a.Input != nil {
		fmt.Fprintf(b, "    input %s\n", a.Input.Name)
	}
	if a.Output != nil {
		fmt.Fprintf(b, "    output %s\n", a.Output.Name)
	}
	b.WriteString("}\n")
}

func renderWorkflow(b *strings.Builder, w *WorkflowDecl) {
	params := make([]string, len(w.Params))
	for i, p := range w.Params {
		params[i] = fmt.Sprintf("%s: %s", identName(p.Name), typeName(p.Type))
	}
	fmt.Fprintf(b, "workflow %s(%s)", identName(w.Name), strings.Join(params, ", "))
	if w.Result != nil {
		fmt.Fprintf(b, " -> %s", w.Result.Name)
	}
	if w.Effects != nil {
		names := make([]string, len(w.Effects))
		for i, e := range w.Effects {
			names[i] = e.Name
		}
		fmt.Fprintf(b, " effects { %s }", strings.Join(names, ", "))
	}
	b.WriteString(" {\n")
	for _, s := range w.Body {
		renderStmt(b, s, 1)
	}
	b.WriteString("}\n")
}

func renderStmt(b *strings.Builder, s Stmt, depth int) {
	indent := strings.Repeat("    ", depth)
	switch n := s.(type) {
	case *AssignStmt:
		fmt.Fprintf(b, "%s%s = %s\n", indent, identName(n.Target), renderExpr(n.Value))
	case *ExprStmt:
		fmt.Fprintf(b, "%s%s\n", indent, renderExpr(n.X))
	case *ReturnStmt:
		fmt.Fprintf(b, "%sreturn %s\n", indent, renderExpr(n.Value))
	case *ParallelStmt:
		fmt.Fprintf(b, "%sparallel {\n", indent)
		for _, a := range n.Body {
			renderStmt(b, a, depth+1)
		}
		fmt.Fprintf(b, "%s}\n", indent)
	default:
		fmt.Fprintf(b, "%s/* unknown stmt %T */\n", indent, s)
	}
}

func renderExpr(e Expr) string {
	switch n := e.(type) {
	case *RefExpr:
		return dottedName(n.Parts)
	case *CallExpr:
		args := make([]string, len(n.Args))
		for i, a := range n.Args {
			if a.Name != nil {
				args[i] = fmt.Sprintf("%s: %s", a.Name.Name, renderExpr(a.Value))
			} else {
				args[i] = renderExpr(a.Value)
			}
		}
		return fmt.Sprintf("%s(%s)", dottedName(n.Callee.Parts), strings.Join(args, ", "))
	case nil:
		return "/*nil*/"
	default:
		return fmt.Sprintf("/* unknown expr %T */", e)
	}
}

func identName(i *Ident) string {
	if i == nil {
		return "_"
	}
	return i.Name
}

func typeName(t *TypeRef) string {
	if t == nil {
		return "_"
	}
	return t.Name
}
