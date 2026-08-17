package plan

import (
	"fmt"
	"strings"
)

// FormatPlan renders a short human-readable summary (design doc §10.2, issue #166).
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
	for _, sec := range planTableSections(p) {
		b.WriteByte('\n')
		b.WriteString(formatPlanSection(sec))
		b.WriteByte('\n')
	}
	return strings.TrimSuffix(b.String(), "\n")
}

type planTableSection struct {
	Title    string
	Items    []RiskItem
	Messages []string
}

// planTableSections is the single table render path for plan extras.
// #191 should append an "Effect bound" section here rather than a second formatter.
func planTableSections(p *Plan) []planTableSection {
	if p == nil {
		return nil
	}
	if len(p.Risk.Items) > 0 {
		return []planTableSection{{Title: "Risk delta", Items: p.Risk.Items}}
	}
	if len(p.Risk.Messages) > 0 {
		return []planTableSection{{Title: "Risk delta", Messages: p.Risk.Messages}}
	}
	return nil
}

func formatPlanSection(sec planTableSection) string {
	title := strings.TrimSpace(sec.Title)
	if title == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", title)
	if len(sec.Items) > 0 {
		writeGroupedRiskItems(&b, sec.Items)
		return strings.TrimSuffix(b.String(), "\n")
	}
	for _, m := range sec.Messages {
		fmt.Fprintf(&b, "- %s\n", m)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// FormatPlanSection renders a titled, severity-grouped list of risk items
// (high, then medium, then low). #191 should reuse this for an "Effect bound"
// section instead of introducing a parallel table renderer.
func FormatPlanSection(title string, items []RiskItem) string {
	return formatPlanSection(planTableSection{Title: title, Items: items})
}

func writeGroupedRiskItems(b *strings.Builder, items []RiskItem) {
	by := map[RiskSeverity][]RiskItem{}
	for _, it := range items {
		sev := it.Severity
		switch sev {
		case RiskSeverityHigh, RiskSeverityMedium, RiskSeverityLow:
		default:
			sev = RiskSeverityLow
		}
		by[sev] = append(by[sev], it)
	}
	for _, sev := range []RiskSeverity{RiskSeverityHigh, RiskSeverityMedium, RiskSeverityLow} {
		group := by[sev]
		if len(group) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s:\n", sev)
		for _, it := range group {
			fmt.Fprintf(b, "- %s\n", FormatRiskItem(it))
		}
	}
}

// FormatRiskItem renders one labeled risk line: [severity] category: reason.
func FormatRiskItem(it RiskItem) string {
	return fmt.Sprintf("[%s] %s: %s", it.Severity, it.Category, it.Reason)
}

// RiskExport is the JSON/YAML risk payload shared by plan and apply (issue #166).
// Witness hops stay structured objects (not pre-formatted strings) so CI can gate
// on category, severity, target, and path. #191 should add effect-bound items on
// this type (and in planTableSections) rather than a second mapping in the CLI.
type RiskExport struct {
	Risk      []string   `json:"risk" yaml:"risk"`
	RiskItems []RiskItem `json:"riskItems" yaml:"riskItems"`
}

// ExportRisk returns the machine-readable risk view for -o json and -o yaml.
func ExportRisk(p *Plan) RiskExport {
	out := RiskExport{Risk: []string{}, RiskItems: []RiskItem{}}
	if p == nil {
		return out
	}
	if len(p.Risk.Messages) > 0 {
		out.Risk = p.Risk.Messages
	}
	if len(p.Risk.Items) > 0 {
		out.RiskItems = p.Risk.Items
	}
	return out
}

// AttachRiskExport writes risk and riskItems onto a JSON/YAML plan map.
func AttachRiskExport(m map[string]any, p *Plan) {
	if m == nil {
		return
	}
	exp := ExportRisk(p)
	m["risk"] = exp.Risk
	m["riskItems"] = exp.RiskItems
}
