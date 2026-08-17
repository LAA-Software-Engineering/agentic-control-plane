package plan

import (
	"fmt"
	"strings"
)

// FormatPlan renders a short human-readable summary (design doc §10.2).
func FormatPlan(p *Plan) string {
	if p == nil {
		return ""
	}
	var nCreate, nUpdate, nDelete int
	for _, op := range p.Operations {
		switch op.Action {
		case ActionCreate:
			nCreate++
		case ActionUpdate:
			nUpdate++
		case ActionDelete:
			nDelete++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Plan: %d to add, %d to change, %d to delete\n", nCreate, nUpdate, nDelete)
	for _, op := range p.Operations {
		switch op.Action {
		case ActionCreate:
			fmt.Fprintf(&b, "+ create %s\n", op.Target.String())
		case ActionUpdate:
			fmt.Fprintf(&b, "~ update %s\n", op.Target.String())
			for _, d := range op.Diff {
				fmt.Fprintf(&b, "    %s: %s -> %s\n", d.Path, d.Old, d.New)
			}
		case ActionDelete:
			fmt.Fprintf(&b, "- delete %s\n", op.Target.String())
		}
	}
	if len(p.Risk.Items) > 0 {
		b.WriteString("\nRisk delta:\n")
		for _, it := range p.Risk.Items {
			fmt.Fprintf(&b, "- %s\n", FormatRiskItem(it))
		}
	} else if len(p.Risk.Messages) > 0 {
		b.WriteString("\nRisk delta:\n")
		for _, m := range p.Risk.Messages {
			fmt.Fprintf(&b, "- %s\n", m)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// FormatRiskItem renders one labeled risk line: [severity] category: reason.
func FormatRiskItem(it RiskItem) string {
	return fmt.Sprintf("[%s] %s: %s", it.Severity, it.Category, it.Reason)
}
