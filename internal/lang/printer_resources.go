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
	if m := d.MCP; m != nil {
		b.WriteString("    mcp {\n")
		printStringLitField(b, "        ", "transport", m.Transport)
		printStringLitField(b, "        ", "command", m.Command)
		if len(m.Args) > 0 {
			b.WriteString("        args {")
			for _, a := range m.Args {
				fmt.Fprintf(b, " %s", strconv.Quote(a.Value))
			}
			b.WriteString(" }\n")
		}
		printStringLitField(b, "        ", "url", m.URL)
		printHeadersBlock(b, "        ", m.Headers)
		b.WriteString("    }\n")
	}
	if h := d.HTTP; h != nil {
		b.WriteString("    http {\n")
		printStringLitField(b, "        ", "baseUrl", h.BaseURL)
		printHeadersBlock(b, "        ", h.Headers)
		b.WriteString("    }\n")
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

// printStringLitField prints a quoted string field only when the literal is present (unlike
// printStringField, which always emits). Used for optional transport fields.
func printStringLitField(b *strings.Builder, indent, name string, s *StringLit) {
	if s == nil {
		return
	}
	printStringField(b, indent, name, s.Value)
}

// printHeadersBlock renders a `headers { "<key>" "<value>" … }` block in author order.
func printHeadersBlock(b *strings.Builder, indent string, headers []*HeaderPair) {
	if len(headers) == 0 {
		return
	}
	fmt.Fprintf(b, "%sheaders {\n", indent)
	for _, h := range headers {
		if h == nil || h.Key == nil {
			continue
		}
		fmt.Fprintf(b, "%s    %s %s\n", indent, strconv.Quote(h.Key.Value), strconv.Quote(stringLitOrEmpty(h.Value)))
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

func stringLitOrEmpty(s *StringLit) string {
	if s == nil {
		return ""
	}
	return s.Value
}

func printPolicy(b *strings.Builder, d *PolicyDecl) {
	fmt.Fprintf(b, "policy %s {\n", identName(d.Name))
	if d.Preset != nil {
		fmt.Fprintf(b, "    preset %s\n", identName(d.Preset))
	}
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

// printEnvironment renders `environment <Name> { overrides { agents { … } policies { … } } }`
// (issue #440) with indent-parameterized sub-block helpers so the nested structure round-trips.
func printEnvironment(b *strings.Builder, d *EnvironmentDecl) {
	fmt.Fprintf(b, "environment %s {\n", identName(d.Name))
	if ov := d.Overrides; ov != nil {
		b.WriteString("    overrides {\n")
		if len(ov.Agents) > 0 {
			b.WriteString("        agents {\n")
			for _, a := range ov.Agents {
				fmt.Fprintf(b, "            %s {\n", identName(a.Name))
				if a.Model != nil {
					fmt.Fprintf(b, "                model %s\n", a.Model.Raw)
				}
				if a.Constraints != nil {
					printConstraintsAt(b, "                ", a.Constraints)
				}
				b.WriteString("            }\n")
			}
			b.WriteString("        }\n")
		}
		if len(ov.Policies) > 0 {
			b.WriteString("        policies {\n")
			for _, pol := range ov.Policies {
				fmt.Fprintf(b, "            %s {\n", identName(pol.Name))
				if pol.Execution != nil {
					printExecutionAt(b, "                ", pol.Execution)
				}
				if pol.Approvals != nil {
					printApprovalsAt(b, "                ", pol.Approvals)
				}
				b.WriteString("            }\n")
			}
			b.WriteString("        }\n")
		}
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
}

// printConstraintsAt renders a `constraints { … }` block at the given indent (fields at indent+4).
func printConstraintsAt(b *strings.Builder, indent string, c *Constraints) {
	inner := indent + "    "
	fmt.Fprintf(b, "%sconstraints {\n", indent)
	if c.MaxIterations != nil {
		fmt.Fprintf(b, "%smaxIterations %d\n", inner, *c.MaxIterations)
	}
	if c.TimeoutSeconds != nil {
		fmt.Fprintf(b, "%stimeoutSeconds %d\n", inner, *c.TimeoutSeconds)
	}
	if c.Temperature != nil {
		fmt.Fprintf(b, "%stemperature %s\n", inner, strconv.FormatFloat(*c.Temperature, 'g', -1, 64))
	}
	if c.RequireStructuredOutput != nil {
		fmt.Fprintf(b, "%srequireStructuredOutput %s\n", inner, strconv.FormatBool(*c.RequireStructuredOutput))
	}
	fmt.Fprintf(b, "%s}\n", indent)
}

// printExecutionAt renders an `execution { … }` block at the given indent.
func printExecutionAt(b *strings.Builder, indent string, e *PolicyExecutionBlock) {
	inner := indent + "    "
	fmt.Fprintf(b, "%sexecution {\n", indent)
	if e.MaxTotalCostUsd != nil {
		fmt.Fprintf(b, "%smaxTotalCostUsd %s\n", inner, strconv.FormatFloat(*e.MaxTotalCostUsd, 'f', -1, 64))
	}
	if e.MaxWallClockSeconds != nil {
		fmt.Fprintf(b, "%smaxWallClockSeconds %d\n", inner, *e.MaxWallClockSeconds)
	}
	printBoolField(b, inner, "requireStructuredOutput", e.RequireStructuredOutput)
	fmt.Fprintf(b, "%s}\n", indent)
}

// printApprovalsAt renders an `approvals { … }` block at the given indent.
func printApprovalsAt(b *strings.Builder, indent string, a *PolicyApprovalsBlock) {
	inner := indent + "    "
	fmt.Fprintf(b, "%sapprovals {\n", indent)
	if len(a.RequiredFor) > 0 {
		fmt.Fprintf(b, "%srequiredFor {\n", inner)
		for _, g := range a.RequiredFor {
			fmt.Fprintf(b, "%s    %s\n", inner, grantPath(g))
		}
		fmt.Fprintf(b, "%s}\n", inner)
	}
	printBoolField(b, inner, "requireAllTools", a.RequireAllTools)
	printBoolField(b, inner, "permissive", a.Permissive)
	fmt.Fprintf(b, "%s}\n", indent)
}
