package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// Print reconstructs canonical .agent source from an AST. Output is normalized —
// 4-space indentation, single spaces around operators, comma-joined effects — so
// it does not depend on the incidental formatting of the input. Print is the
// engine behind `terfyn fmt`; parse -> Print -> parse -> Print is stable
// (idempotent) for any file that parses without error.
func Print(f *File) string {
	var b strings.Builder
	if f == nil {
		return ""
	}
	for i, d := range f.Decls {
		if i > 0 {
			b.WriteString("\n")
		}
		switch n := d.(type) {
		case *AgentDecl:
			printAgent(&b, n)
		case *WorkflowDecl:
			printWorkflow(&b, n)
		case *ToolDecl:
			printTool(&b, n)
		case *PolicyDecl:
			printPolicy(&b, n)
		case *EnvironmentDecl:
			printEnvironment(&b, n)
		case *ProviderDecl:
			printProvider(&b, n)
		case *DefaultsDecl:
			printDefaults(&b, n)
		case *LimitsDecl:
			printLimitsDecl(&b, n)
		default:
			fmt.Fprintf(&b, "/* unknown decl %T */\n", d)
		}
	}
	return b.String()
}

// Format parses src and returns canonical .agent source plus any diagnostics.
// When parsing reports errors the returned source is best-effort (formatted from
// the partial AST) and callers should surface the diagnostics rather than write
// the output.
func Format(file, src string) (string, Diagnostics) {
	f, diags := Parse(file, src)
	return Print(f), diags
}

func printAgent(b *strings.Builder, a *AgentDecl) {
	fmt.Fprintf(b, "agent %s {\n", identName(a.Name))
	if a.Model != nil {
		fmt.Fprintf(b, "    model %s/%s\n", a.Model.Provider, a.Model.Name)
	}
	if a.Policy != nil {
		fmt.Fprintf(b, "    policy %s\n", a.Policy.Name)
	}
	if a.Description != nil {
		printStringField(b, "    ", "description", a.Description.Value)
	}
	if a.Instructions != nil {
		printInstructions(b, a.Instructions.Value)
	}
	if a.InstructionsFile != nil && a.InstructionsFile.Path != nil {
		// Re-emit the file("...") form verbatim (never the resolved prompt text), so fmt
		// round-trips the reference (#360).
		fmt.Fprintf(b, "    instructions file(%s)\n", strconv.Quote(a.InstructionsFile.Path.Value))
	}
	if a.Constraints != nil {
		printConstraints(b, a.Constraints)
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

// printInstructions renders the agent prompt. A multiline value prints as a
// canonical `"""…"""` block whose body lines carry the field's 4-space indent and
// whose closing delimiter is on its own indented line — the exact shape
// normalizeMultiline strips back to the original value, so parse -> print -> parse
// is stable. A value with no newline — OR one containing a literal `"""`, which
// the raw multiline body cannot represent (it would read as a premature close and
// corrupt the file) — falls back to the escaped single-quoted form, which escapes
// newlines and quotes and always re-parses.
func printInstructions(b *strings.Builder, v string) {
	printStringField(b, "    ", "instructions", v)
}

// printStringField renders a string-valued field (instructions, description) at the
// given indent: a single-line value as a quoted literal; a multiline value (or one
// containing a `"""`, which the raw block cannot hold) as a canonical `"""…"""` block
// whose body lines carry the field indent — the exact shape normalizeMultiline strips
// back, so parse -> print -> parse is stable.
func printStringField(b *strings.Builder, indent, name, v string) {
	if !strings.Contains(v, "\n") || strings.Contains(v, `"""`) {
		fmt.Fprintf(b, "%s%s %s\n", indent, name, strconv.Quote(v))
		return
	}
	fmt.Fprintf(b, "%s%s \"\"\"\n", indent, name)
	for _, ln := range strings.Split(v, "\n") {
		if ln == "" {
			b.WriteString("\n")
		} else {
			b.WriteString(indent + ln + "\n")
		}
	}
	fmt.Fprintf(b, "%s\"\"\"\n", indent)
}

// printConstraints renders the `constraints { }` block, one field per line in a
// stable order, omitting fields the author did not set.
func printConstraints(b *strings.Builder, c *Constraints) {
	b.WriteString("    constraints {\n")
	if c.MaxIterations != nil {
		fmt.Fprintf(b, "        maxIterations %d\n", *c.MaxIterations)
	}
	if c.MaxTokens != nil {
		fmt.Fprintf(b, "        maxTokens %d\n", *c.MaxTokens)
	}
	if c.TimeoutSeconds != nil {
		fmt.Fprintf(b, "        timeoutSeconds %d\n", *c.TimeoutSeconds)
	}
	if c.Temperature != nil {
		fmt.Fprintf(b, "        temperature %s\n", strconv.FormatFloat(*c.Temperature, 'g', -1, 64))
	}
	if c.RequireStructuredOutput != nil {
		fmt.Fprintf(b, "        requireStructuredOutput %s\n", strconv.FormatBool(*c.RequireStructuredOutput))
	}
	b.WriteString("    }\n")
}

func printWorkflow(b *strings.Builder, w *WorkflowDecl) {
	params := make([]string, len(w.Params))
	for i, p := range w.Params {
		params[i] = fmt.Sprintf("%s: %s", identName(p.Name), typeName(p.Type))
	}
	fmt.Fprintf(b, "workflow %s(%s)", identName(w.Name), strings.Join(params, ", "))
	if w.Result != nil {
		fmt.Fprintf(b, " -> %s", w.Result.Name)
	}
	var clauses []string
	if w.Policy != nil {
		clauses = append(clauses, fmt.Sprintf("policy %s", w.Policy.Name))
	}
	if w.Effects != nil {
		names := make([]string, len(w.Effects))
		for i, e := range w.Effects {
			names[i] = e.Name
		}
		clauses = append(clauses, fmt.Sprintf("effects { %s }", strings.Join(names, ", ")))
	}
	if w.Description != nil {
		// A description (possibly multiline) does not fit the inline header, so the
		// header clauses go on their own indented lines before the opening brace.
		b.WriteString("\n")
		printStringField(b, "    ", "description", w.Description.Value)
		for _, c := range clauses {
			fmt.Fprintf(b, "    %s\n", c)
		}
		b.WriteString("{\n")
	} else {
		for _, c := range clauses {
			fmt.Fprintf(b, " %s", c)
		}
		b.WriteString(" {\n")
	}
	for _, s := range w.Body {
		printStmt(b, s, 1)
	}
	b.WriteString("}\n")
}

func printStmt(b *strings.Builder, s Stmt, depth int) {
	indent := strings.Repeat("    ", depth)
	switch n := s.(type) {
	case *AssignStmt:
		fmt.Fprintf(b, "%s%s = %s\n", indent, identName(n.Target), printExpr(n.Value))
	case *ExprStmt:
		fmt.Fprintf(b, "%s%s\n", indent, printExpr(n.X))
	case *ReturnStmt:
		fmt.Fprintf(b, "%sreturn %s\n", indent, printExpr(n.Value))
	case *ParallelStmt:
		fmt.Fprintf(b, "%sparallel {\n", indent)
		for _, a := range n.Body {
			printStmt(b, a, depth+1)
		}
		fmt.Fprintf(b, "%s}\n", indent)
	case *IfStmt:
		printIf(b, n, depth)
	case *ForStmt:
		kw := "for"
		if n.Parallel {
			kw = "parallel for"
		}
		fmt.Fprintf(b, "%s%s %s in %s {\n", indent, kw, identName(n.Var), printExpr(n.In))
		for _, st := range n.Body {
			printStmt(b, st, depth+1)
		}
		fmt.Fprintf(b, "%s}\n", indent)
	case *WhileStmt:
		fmt.Fprintf(b, "%swhile %s limit %d {\n", indent, printExpr(n.Cond), n.Limit)
		for _, st := range n.Body {
			printStmt(b, st, depth+1)
		}
		fmt.Fprintf(b, "%s}\n", indent)
	case *RetryStmt:
		fmt.Fprintf(b, "%sretry until %s limit %d {\n", indent, printExpr(n.Cond), n.Limit)
		for _, st := range n.Body {
			printStmt(b, st, depth+1)
		}
		fmt.Fprintf(b, "%s}\n", indent)
	case *ApprovalStmt:
		inner := indent + "    "
		fmt.Fprintf(b, "%sapproval %s {\n", indent, identName(n.Bind))
		if n.Description != nil {
			printStringField(b, inner, "description", n.Description.Value)
		}
		if len(n.RedactKeys) > 0 {
			fmt.Fprintf(b, "%sredactKeys {", inner)
			for _, k := range n.RedactKeys {
				fmt.Fprintf(b, " %s", strconv.Quote(k.Value))
			}
			b.WriteString(" }\n")
		}
		if len(n.With) > 0 {
			fmt.Fprintf(b, "%swith {\n", inner)
			arg := inner + "    "
			for _, a := range n.With {
				if a.Name != nil {
					fmt.Fprintf(b, "%s%s: %s\n", arg, identName(a.Name), printExpr(a.Value))
				} else {
					fmt.Fprintf(b, "%s%s\n", arg, printExpr(a.Value))
				}
			}
			fmt.Fprintf(b, "%s}\n", inner)
		}
		fmt.Fprintf(b, "%s}\n", indent)
	default:
		fmt.Fprintf(b, "%s/* unknown stmt %T */\n", indent, s)
	}
}

// printIf renders a conditional, collapsing `else { if … }` back into
// `else if …` when the else arm is exactly one nested IfStmt.
func printIf(b *strings.Builder, n *IfStmt, depth int) {
	indent := strings.Repeat("    ", depth)
	fmt.Fprintf(b, "%sif %s {\n", indent, printExpr(n.Cond))
	for _, st := range n.Then {
		printStmt(b, st, depth+1)
	}
	if len(n.Else) == 1 {
		if elif, ok := n.Else[0].(*IfStmt); ok {
			fmt.Fprintf(b, "%s} else ", indent)
			// Render the nested if without its leading indent, on the same line.
			var nested strings.Builder
			printIf(&nested, elif, depth)
			b.WriteString(strings.TrimLeft(nested.String(), " "))
			return
		}
	}
	if len(n.Else) > 0 {
		fmt.Fprintf(b, "%s} else {\n", indent)
		for _, st := range n.Else {
			printStmt(b, st, depth+1)
		}
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

func printExpr(e Expr) string {
	switch n := e.(type) {
	case *RefExpr:
		return dottedName(n.Parts)
	case *LitExpr:
		return printLit(n)
	case *UnaryExpr:
		// `!` binds tighter than every binary operator, so any binary operand is
		// parenthesized; a unary/leaf operand is not.
		return "!" + parenIfBinary(n.X)
	case *BinaryExpr:
		return fmt.Sprintf("%s %s %s", printBinOperand(n.X, n.Op, false), opSymbol(n.Op), printBinOperand(n.Y, n.Op, true))
	case *CallExpr:
		args := make([]string, len(n.Args))
		for i, a := range n.Args {
			if a.Name != nil {
				args[i] = fmt.Sprintf("%s: %s", a.Name.Name, printExpr(a.Value))
			} else {
				args[i] = printExpr(a.Value)
			}
		}
		return fmt.Sprintf("%s(%s)", dottedName(n.Callee.Parts), strings.Join(args, ", "))
	case *ObjectExpr:
		if len(n.Fields) == 0 {
			return "{}"
		}
		fields := make([]string, len(n.Fields))
		for i, f := range n.Fields {
			fields[i] = fmt.Sprintf("%s: %s", identName(f.Key), printExpr(f.Value))
		}
		return "{ " + strings.Join(fields, ", ") + " }"
	case nil:
		return "/*nil*/"
	default:
		return fmt.Sprintf("/* unknown expr %T */", e)
	}
}

// binPrec is the binding tightness used to decide parenthesization: `||` loosest,
// then `&&`, then comparisons (tightest of the binaries). Higher binds tighter.
func binPrec(k Kind) int {
	switch k {
	case KindOrOr:
		return 1
	case KindAndAnd:
		return 2
	default: // comparisons (==, !=, <, <=, >, >=) — non-chaining
		return 3
	}
}

// printBinOperand renders one operand of a binary expression, adding parentheses
// only when omitting them would regroup the tree on re-parse. Operators are
// left-associative, so an operand of equal precedence needs parentheses only on
// the right. A non-binary operand (a unary, call, ref, or literal) never needs
// them.
func printBinOperand(e Expr, parentOp Kind, isRight bool) string {
	be, ok := e.(*BinaryExpr)
	if !ok {
		return printExpr(e)
	}
	cp, pp := binPrec(be.Op), binPrec(parentOp)
	if cp < pp || (cp == pp && isRight) {
		return "(" + printExpr(e) + ")"
	}
	return printExpr(e)
}

// parenIfBinary parenthesizes a binary operand (used by unary `!`, which binds
// tighter than any binary).
func parenIfBinary(e Expr) string {
	if _, ok := e.(*BinaryExpr); ok {
		return "(" + printExpr(e) + ")"
	}
	return printExpr(e)
}

func printLit(n *LitExpr) string {
	switch v := n.Value.(type) {
	case string:
		return strconv.Quote(v)
	case bool:
		return strconv.FormatBool(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", n.Value)
	}
}

func opSymbol(k Kind) string {
	switch k {
	case KindEqEq:
		return "=="
	case KindBangEq:
		return "!="
	case KindLt:
		return "<"
	case KindLte:
		return "<="
	case KindGt:
		return ">"
	case KindGte:
		return ">="
	case KindAndAnd:
		return "&&"
	case KindOrOr:
		return "||"
	default:
		return "?"
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
