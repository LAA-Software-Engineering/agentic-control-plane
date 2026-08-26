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
	Body     string
}

// planTableSections is the single table render path for plan extras.
// Effect bound, capability/effect deltas, and authority (issue #191) append here
// rather than through a second formatter.
func planTableSections(p *Plan) []planTableSection {
	if p == nil {
		return nil
	}
	var secs []planTableSection
	for _, b := range p.EffectBound {
		title := "Effect bound"
		if b.RootKind != "" && b.RootName != "" {
			title = fmt.Sprintf("Effect bound (%s/%s)", displayRootKind(b.RootKind), b.RootName)
		}
		if len(b.Items) > 0 {
			secs = append(secs, planTableSection{Title: title, Items: b.Items})
		}
	}

	var cap, eff, other []RiskItem
	for _, it := range p.Risk.Items {
		switch it.Category {
		case RiskCategoryCapabilityDelta:
			cap = append(cap, it)
		case RiskCategoryEffectDelta:
			eff = append(eff, it)
		default:
			other = append(other, it)
		}
	}
	if len(cap) > 0 {
		secs = append(secs, planTableSection{Title: "Capability delta", Body: formatCapabilityDeltaBody(cap)})
	}
	if len(eff) > 0 {
		secs = append(secs, planTableSection{Title: "Effect delta", Body: formatEffectDeltaBody(eff)})
	}
	if shouldShowAuthority(p, cap, eff) {
		secs = append(secs, planTableSection{Title: "Authority", Body: formatAuthorityBody(p.Authority)})
	}

	if len(other) > 0 {
		secs = append(secs, planTableSection{Title: "Risk delta", Items: other})
	} else if len(p.Risk.Items) == 0 && len(p.Risk.Messages) > 0 {
		secs = append(secs, planTableSection{Title: "Risk delta", Messages: p.Risk.Messages})
	}
	return secs
}

func formatPlanSection(sec planTableSection) string {
	title := strings.TrimSpace(sec.Title)
	if title == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", title)
	if sec.Body != "" {
		b.WriteString(sec.Body)
		if !strings.HasSuffix(sec.Body, "\n") {
			b.WriteByte('\n')
		}
		return strings.TrimSuffix(b.String(), "\n")
	}
	if len(sec.Items) > 0 {
		writeGroupedRiskItems(&b, sec.Items)
		return strings.TrimSuffix(b.String(), "\n")
	}
	for _, m := range sec.Messages {
		fmt.Fprintf(&b, "- %s\n", m)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func displayRootKind(kind string) string {
	switch kind {
	case "workflow":
		return "Workflow"
	case "agent":
		return "Agent"
	default:
		if kind == "" {
			return ""
		}
		return strings.ToUpper(kind[:1]) + kind[1:]
	}
}

func formatCapabilityDeltaBody(items []RiskItem) string {
	byAgent := map[string][]string{}
	var agents []string
	seenAgent := map[string]struct{}{}
	for _, it := range items {
		name := it.Target.Name
		if name == "" {
			name = it.Ident
		}
		if _, ok := seenAgent[name]; !ok {
			seenAgent[name] = struct{}{}
			agents = append(agents, name)
		}
		uses := it.Ident
		if uses == "" {
			uses = it.Reason
		}
		byAgent[name] = append(byAgent[name], uses)
	}
	var b strings.Builder
	for _, name := range agents {
		fmt.Fprintf(&b, "Agent/%s\n", name)
		for _, uses := range byAgent[name] {
			fmt.Fprintf(&b, "+ %s\n", uses)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func formatEffectDeltaBody(items []RiskItem) string {
	var b strings.Builder
	seen := map[string]struct{}{}
	for _, it := range items {
		ident := it.Ident
		if ident == "" {
			ident = it.Reason
		}
		if _, ok := seen[ident]; ok {
			continue
		}
		seen[ident] = struct{}{}
		fmt.Fprintf(&b, "+ %s\n", ident)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func formatAuthorityBody(a AuthorityDelta) string {
	return fmt.Sprintf("  static      -> %s\n  autonomous  -> %s",
		formatAuthorityStatus(a.Static), formatAuthorityStatus(a.Autonomous))
}

func formatAuthorityStatus(s AuthorityStatus) string {
	if s == AuthorityWidened {
		return "WIDENED"
	}
	if s == "" {
		return string(AuthorityUnchanged)
	}
	return string(s)
}

// FormatPlanSection renders a titled, severity-grouped list of risk items
// (high, then medium, then low). Effect bound sections reuse this path.
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
// on category, severity, target, and path. Effect bound, capability/effect deltas,
// and authority status are structural fields for #191 CI gates.
type RiskExport struct {
	Risk            []string        `json:"risk" yaml:"risk"`
	RiskItems       []RiskItem      `json:"riskItems" yaml:"riskItems"`
	EffectBound     []BoundSection  `json:"effectBound,omitempty" yaml:"effectBound,omitempty"`
	CapabilityDelta []RiskItem      `json:"capabilityDelta,omitempty" yaml:"capabilityDelta,omitempty"`
	EffectDelta     []RiskItem      `json:"effectDelta,omitempty" yaml:"effectDelta,omitempty"`
	Authority       *AuthorityDelta `json:"authority,omitempty" yaml:"authority,omitempty"`
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
	if len(p.EffectBound) > 0 {
		out.EffectBound = p.EffectBound
	}
	for _, it := range p.Risk.Items {
		switch it.Category {
		case RiskCategoryCapabilityDelta:
			out.CapabilityDelta = append(out.CapabilityDelta, it)
		case RiskCategoryEffectDelta:
			out.EffectDelta = append(out.EffectDelta, it)
		}
	}
	if shouldExportAuthority(p, out) {
		auth := p.Authority
		out.Authority = &auth
	}
	return out
}

func shouldShowAuthority(p *Plan, cap, eff []RiskItem) bool {
	if p == nil {
		return false
	}
	if p.Authority.Static == AuthorityWidened || p.Authority.Autonomous == AuthorityWidened {
		return true
	}
	return len(p.EffectBound) > 0 || len(cap) > 0 || len(eff) > 0
}

func shouldExportAuthority(p *Plan, out RiskExport) bool {
	if p == nil {
		return false
	}
	if p.Authority.Static == AuthorityWidened || p.Authority.Autonomous == AuthorityWidened {
		return true
	}
	return len(p.EffectBound) > 0 || len(out.CapabilityDelta) > 0 || len(out.EffectDelta) > 0
}

// AttachRiskExport writes risk and riskItems onto a JSON/YAML plan map.
func AttachRiskExport(m map[string]any, p *Plan) {
	if m == nil {
		return
	}
	exp := ExportRisk(p)
	m["risk"] = exp.Risk
	m["riskItems"] = exp.RiskItems
	if len(exp.EffectBound) > 0 {
		m["effectBound"] = exp.EffectBound
	}
	if len(exp.CapabilityDelta) > 0 {
		m["capabilityDelta"] = exp.CapabilityDelta
	}
	if len(exp.EffectDelta) > 0 {
		m["effectDelta"] = exp.EffectDelta
	}
	if exp.Authority != nil {
		m["authority"] = exp.Authority
	}
}
