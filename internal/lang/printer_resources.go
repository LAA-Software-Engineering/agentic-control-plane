package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// Canonical printing for inline tool/policy declarations (ADR 005, issue #333). Fields are
// newline-separated, matching the rest of the .agent surface.

func printTool(b *strings.Builder, d *ToolDecl) {
	fmt.Fprintf(b, "tool %s {\n", identName(d.Name))
	if d.Type != nil {
		fmt.Fprintf(b, "    type %s\n", d.Type.Name)
	}
	if s := d.Safety; s != nil {
		b.WriteString("    safety {\n")
		printBoolField(b, "        ", "trusted", s.Trusted)
		printBoolField(b, "        ", "sideEffects", s.SideEffects)
		printBoolField(b, "        ", "requiresApproval", s.RequiresApproval)
		b.WriteString("    }\n")
	}
	if d.Operations != nil {
		if len(d.Operations.Ops) == 0 {
			b.WriteString("    operations {}\n") // an explicit empty block: a closed, deny-all manifest
		} else {
			b.WriteString("    operations {\n")
			for _, op := range d.Operations.Ops {
				fmt.Fprintf(b, "        %s {", identName(op.Name))
				if len(op.Effects) > 0 {
					fmt.Fprintf(b, " effects { %s }", joinEffects(op.Effects))
				}
				b.WriteString(" }\n")
			}
			b.WriteString("    }\n")
		}
	}
	b.WriteString("}\n")
}

func printPolicy(b *strings.Builder, d *PolicyDecl) {
	fmt.Fprintf(b, "policy %s {\n", identName(d.Name))
	if e := d.Execution; e != nil {
		b.WriteString("    execution {\n")
		if e.MaxTotalCostUsd != nil {
			fmt.Fprintf(b, "        maxTotalCostUsd %s\n", strconv.FormatFloat(*e.MaxTotalCostUsd, 'f', -1, 64))
		}
		if e.MaxWallClockSeconds != nil {
			fmt.Fprintf(b, "        maxWallClockSeconds %d\n", *e.MaxWallClockSeconds)
		}
		printBoolField(b, "        ", "requireStructuredOutput", e.RequireStructuredOutput)
		b.WriteString("    }\n")
	}
	if a := d.Approvals; a != nil {
		b.WriteString("    approvals {\n")
		if len(a.RequiredFor) > 0 {
			b.WriteString("        requiredFor {\n")
			for _, g := range a.RequiredFor {
				fmt.Fprintf(b, "            %s\n", grantPath(g))
			}
			b.WriteString("        }\n")
		}
		printBoolField(b, "        ", "requireAllTools", a.RequireAllTools)
		printBoolField(b, "        ", "permissive", a.Permissive)
		b.WriteString("    }\n")
	}
	if e := d.Effects; e != nil {
		b.WriteString("    effects {\n")
		if len(e.Permit) > 0 {
			fmt.Fprintf(b, "        permit { %s }\n", joinEffects(e.Permit))
		}
		if len(e.PermitWithApproval) > 0 {
			fmt.Fprintf(b, "        permitWithApproval { %s }\n", joinEffects(e.PermitWithApproval))
		}
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
}

func printBoolField(b *strings.Builder, indent, name string, v *bool) {
	if v != nil {
		fmt.Fprintf(b, "%s%s %t\n", indent, name, *v)
	}
}

func joinEffects(effs []*EffectRef) string {
	parts := make([]string, 0, len(effs))
	for _, e := range effs {
		if e != nil {
			parts = append(parts, e.Name)
		}
	}
	return strings.Join(parts, ", ")
}

// grantPath reprints a grant's full dotted path (tool.<name>.<operation>).
func grantPath(g *Grant) string {
	if g == nil {
		return ""
	}
	segs := make([]string, 0, len(g.Segments))
	for _, s := range g.Segments {
		segs = append(segs, s.Name)
	}
	return strings.Join(segs, ".")
}
