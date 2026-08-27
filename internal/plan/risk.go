package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/policy"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

// ActionSuggestsWriteSideEffects is the MVP heuristic for whether a tool permission "allow"
// action may grant mutating side effects. It is used when diffing Tool specs and when
// planning brand-new tools (no prior state). True when s (ASCII case-folding) contains any of:
//   - "write"  (e.g. issues.write, pull_requests.write)
//   - "delete"
//   - "merge"
//   - ".send"  (e.g. slack.message.send)
//   - ".post"
func ActionSuggestsWriteSideEffects(action string) bool {
	s := strings.ToLower(strings.TrimSpace(action))
	if s == "" {
		return false
	}
	return strings.Contains(s, "write") ||
		strings.Contains(s, "delete") ||
		strings.Contains(s, "merge") ||
		strings.Contains(s, ".send") ||
		strings.Contains(s, ".post")
}

type policySpecRisk struct {
	Execution *struct {
		MaxTotalCostUsd     float64 `json:"maxTotalCostUsd"`
		MaxWallClockSeconds int     `json:"maxWallClockSeconds"`
	} `json:"execution"`
	Approvals *struct {
		RequiredFor []string `json:"requiredFor"`
	} `json:"approvals"`
	Effects *struct {
		Permit             []string `json:"permit"`
		PermitWithApproval []string `json:"permitWithApproval"`
	} `json:"effects"`
}

type agentSpecRisk struct {
	Model string   `json:"model"`
	Tools []string `json:"tools"`
}

type toolSpecRisk struct {
	Permissions *struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
	Safety *struct {
		Trusted          *bool `json:"trusted"`
		SideEffects      *bool `json:"sideEffects"`
		RequiresApproval *bool `json:"requiresApproval"`
	} `json:"safety"`
}

type jsonEnvelope struct {
	Spec json.RawMessage `json:"spec"`
}

type riskSink struct {
	items []RiskItem
	seen  map[string]struct{}
}

func newRiskSink() *riskSink {
	return &riskSink{seen: map[string]struct{}{}}
}

func (s *riskSink) add(it RiskItem) {
	it.Reason = strings.TrimSpace(it.Reason)
	if it.Reason == "" {
		return
	}
	key := string(it.Category) + "\x00" + string(it.Target.Kind) + "/" + it.Target.Name + "\x00" + it.Reason
	if _, ok := s.seen[key]; ok {
		return
	}
	s.seen[key] = struct{}{}
	s.items = append(s.items, it)
}

func summarizeRisks(
	g *spec.ProjectGraph,
	appliedByID map[string]state.AppliedResource,
	desiredByID map[string]desiredRow,
	ops []Operation,
) RiskSummary {
	sink := newRiskSink()

	for _, op := range ops {
		key := resourceMapKey(op.Target.Kind, op.Target.Name)
		des := desiredByID[key]
		prev, hadPrev := appliedByID[key]

		var oldJSON string
		if hadPrev {
			oldJSON = prev.NormalizedSpecJSON
		}

		switch op.Target.Kind {
		case spec.KindPolicy:
			summarizePolicyRisk(sink, op, oldJSON, des.json, hadPrev)
		case spec.KindAgent:
			summarizeAgentRisk(sink, g, op, oldJSON, des.json, hadPrev)
		case spec.KindTool:
			summarizeToolRisk(sink, g, op, oldJSON, des.json, hadPrev)
		}
	}

	return finalizeRiskItems(sink.items)
}

func mergePolicyLintRisk(g *spec.ProjectGraph, risk RiskSummary) RiskSummary {
	findings := policy.Lint(g)
	if len(findings) == 0 {
		return risk
	}
	sink := newRiskSink()
	for _, it := range risk.Items {
		sink.add(it)
	}
	for _, f := range findings {
		reason := strings.TrimSpace(f.Message)
		if reason == "" {
			reason = string(f.Rule)
		}
		if loc := f.Pos.String(); loc != "" {
			reason = loc + ": " + reason
		}
		name := strings.TrimSpace(f.Policy)
		kind := RiskTargetPolicy
		wkind := WitnessKindPolicy
		if name == "" {
			name = strings.TrimSpace(f.Tool)
			kind = RiskTargetTool
			wkind = WitnessKindTool
		}
		sink.add(RiskItem{
			Category: RiskCategoryLint,
			Severity: RiskSeverity(f.Severity),
			Reason:   reason,
			Target:   RiskTarget{Kind: kind, Name: name},
			Witness:  staticResourceWitness(wkind, name),
		})
	}
	out := finalizeRiskItems(sink.items)
	out.Lint = findings
	return out
}

func finalizeRiskItems(items []RiskItem) RiskSummary {
	sort.SliceStable(items, func(i, j int) bool {
		if riskSevRank(items[i].Severity) != riskSevRank(items[j].Severity) {
			return riskSevRank(items[i].Severity) < riskSevRank(items[j].Severity)
		}
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		if items[i].Target.Kind != items[j].Target.Kind {
			return items[i].Target.Kind < items[j].Target.Kind
		}
		if items[i].Target.Name != items[j].Target.Name {
			return items[i].Target.Name < items[j].Target.Name
		}
		return items[i].Reason < items[j].Reason
	})
	msgs := make([]string, 0, len(items))
	for _, it := range items {
		msgs = append(msgs, it.Reason)
	}
	return RiskSummary{Messages: msgs, Items: items}
}

func riskSevRank(s RiskSeverity) int {
	switch s {
	case RiskSeverityHigh:
		return 0
	case RiskSeverityMedium:
		return 1
	case RiskSeverityLow:
		return 2
	default:
		return 3
	}
}

func staticResourceWitness(kind WitnessHopKind, name string) []WitnessHop {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	return []WitnessHop{{
		Kind:         kind,
		Name:         name,
		Reachability: WitnessStatic,
	}}
}

func summarizePolicyRisk(sink *riskSink, op Operation, oldJSON, newJSON string, hadPrev bool) {
	newPol, ok := parsePolicySpec(newJSON)
	if !ok {
		return
	}
	name := op.Target.Name
	target := RiskTarget{Kind: RiskTargetPolicy, Name: name}
	wit := staticResourceWitness(WitnessKindPolicy, name)
	newCost := policyMaxCost(newPol)
	newWall := policyMaxWall(newPol)
	newApprovals := policyApprovals(newPol)

	if op.Action == ActionCreate || !hadPrev {
		if newCost > 0 {
			sink.add(RiskItem{
				Category: RiskCategorySafety,
				Severity: RiskSeverityLow,
				Reason:   fmt.Sprintf("New policy defines a cost ceiling (Policy/%s).", name),
				Target:   target,
				Witness:  wit,
			})
		}
		if newWall > 0 {
			sink.add(RiskItem{
				Category: RiskCategorySafety,
				Severity: RiskSeverityLow,
				Reason:   fmt.Sprintf("New policy defines a wall-clock ceiling (Policy/%s).", name),
				Target:   target,
				Witness:  wit,
			})
		}
		if len(newApprovals) > 0 {
			sink.add(RiskItem{
				Category: RiskCategorySafety,
				Severity: RiskSeverityLow,
				Reason:   fmt.Sprintf("New policy defines approval requirements (Policy/%s).", name),
				Target:   target,
				Witness:  wit,
			})
		}
		return
	}

	oldPol, ok := parsePolicySpec(oldJSON)
	if !ok {
		return
	}
	oldCost := policyMaxCost(oldPol)
	oldWall := policyMaxWall(oldPol)
	oldApprovals := policyApprovals(oldPol)

	if newCost > oldCost+1e-9 {
		sink.add(RiskItem{
			Category: RiskCategoryBudgetRelaxation,
			Severity: RiskSeverityHigh,
			Reason:   fmt.Sprintf("Cost ceiling increased (Policy/%s).", name),
			Target:   target,
			Witness:  wit,
		})
	}
	if newWall > oldWall {
		sink.add(RiskItem{
			Category: RiskCategoryBudgetRelaxation,
			Severity: RiskSeverityHigh,
			Reason:   fmt.Sprintf("Wall-clock ceiling increased (Policy/%s).", name),
			Target:   target,
			Witness:  wit,
		})
	}
	for _, a := range oldApprovals {
		if containsString(newApprovals, a) {
			continue
		}
		sink.add(RiskItem{
			Category: RiskCategoryApprovalRemoval,
			Severity: RiskSeverityHigh,
			Reason:   fmt.Sprintf("Approval requirements removed for %q (Policy/%s).", a, name),
			Target:   target,
			Witness:  wit,
		})
	}
	addEffectPermitWidening(sink, name, target, wit, oldPol, newPol)
}

func summarizeAgentRisk(sink *riskSink, g *spec.ProjectGraph, op Operation, oldJSON, newJSON string, hadPrev bool) {
	newAg, ok := parseAgentSpec(newJSON)
	if !ok {
		return
	}
	name := op.Target.Name
	target := RiskTarget{Kind: RiskTargetAgent, Name: name}
	wit := staticResourceWitness(WitnessKindAgent, name)
	newModel := strings.TrimSpace(newAg.Model)
	newTools := agentTools(newAg)

	if op.Action == ActionCreate || !hadPrev {
		if newModel != "" {
			sink.add(RiskItem{
				Category: RiskCategorySafety,
				Severity: RiskSeverityLow,
				Reason:   fmt.Sprintf("New agent binds a model (Agent/%s).", name),
				Target:   target,
				Witness:  wit,
			})
		}
		for _, toolName := range newTools {
			sink.add(toolSurfaceItem(g, name, toolName, target, wit))
		}
		return
	}

	oldAg, ok := parseAgentSpec(oldJSON)
	if !ok {
		return
	}
	oldModel := strings.TrimSpace(oldAg.Model)
	if newModel != oldModel && (newModel != "" || oldModel != "") {
		sink.add(RiskItem{
			Category: RiskCategoryModelChange,
			Severity: RiskSeverityMedium,
			Reason:   fmt.Sprintf("Agent model changed (Agent/%s).", name),
			Target:   target,
			Witness:  wit,
		})
	}
	oldSet := make(map[string]struct{}, len(oldAg.Tools))
	for _, t := range agentTools(oldAg) {
		oldSet[t] = struct{}{}
	}
	for _, toolName := range newTools {
		if _, ok := oldSet[toolName]; ok {
			continue
		}
		sink.add(toolSurfaceItem(g, name, toolName, target, wit))
	}
}

func toolSurfaceItem(g *spec.ProjectGraph, agentName, toolName string, target RiskTarget, wit []WitnessHop) RiskItem {
	sev := RiskSeverityMedium
	reason := fmt.Sprintf("Agent tools list gained %q (Agent/%s).", toolName, agentName)
	if toolHasWriteLikeAllow(g, toolName) {
		sev = RiskSeverityHigh
		reason = fmt.Sprintf("Agent tools list gained write-like tool %q (Agent/%s).", toolName, agentName)
	}
	return RiskItem{
		Category: RiskCategoryToolSurfaceChange,
		Severity: sev,
		Reason:   reason,
		Target:   target,
		Witness:  wit,
	}
}

func summarizeToolRisk(sink *riskSink, g *spec.ProjectGraph, op Operation, oldJSON, newJSON string, hadPrev bool) {
	newTool, ok := parseToolSpec(newJSON)
	if !ok {
		return
	}
	name := op.Target.Name
	target := RiskTarget{Kind: RiskTargetTool, Name: name}
	wit := staticResourceWitness(WitnessKindTool, name)
	newAllows := toolAllows(newTool)
	newDecision := toolPlanDecisionFromGraph(g, name)

	if op.Action == ActionCreate || !hadPrev {
		addPermissionWidening(sink, name, nil, newAllows, target, wit)
		addToolSafetyRisk(sink, name, newDecision, nil, target, wit)
		return
	}

	oldTool, ok := parseToolSpec(oldJSON)
	if !ok {
		return
	}
	oldAllows := toolAllows(oldTool)
	addPermissionWidening(sink, name, oldAllows, newAllows, target, wit)
	oldDecision := toolDecisionFromParsed(g, name, oldTool)
	addToolSafetyRisk(sink, name, newDecision, &oldDecision, target, wit)
}

func addPermissionWidening(sink *riskSink, toolName string, oldAllows, newAllows []string, target RiskTarget, wit []WitnessHop) {
	oldSet := make(map[string]struct{}, len(oldAllows))
	for _, a := range oldAllows {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		oldSet[a] = struct{}{}
	}
	for _, a := range newAllows {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := oldSet[a]; ok {
			continue
		}
		sev := RiskSeverityMedium
		reason := fmt.Sprintf("New tool permission allow %q added (Tool/%s).", a, toolName)
		if ActionSuggestsWriteSideEffects(a) {
			sev = RiskSeverityHigh
			reason = fmt.Sprintf("New write-like tool permission %q added (Tool/%s); see ActionSuggestsWriteSideEffects.", a, toolName)
		}
		sink.add(RiskItem{
			Category: RiskCategoryPermissionWidening,
			Severity: sev,
			Reason:   reason,
			Target:   target,
			Witness:  wit,
		})
	}
}

func toolPlanDecisionFromGraph(g *spec.ProjectGraph, toolName string) policy.ToolDecision {
	if g != nil {
		for _, pr := range g.Policies {
			if pr == nil {
				continue
			}
			td := policy.EffectiveToolDecision(g, &pr.Spec, toolName)
			if td.Decision == policy.DecisionRequireApproval {
				return td
			}
		}
	}
	return policy.EffectiveToolDecision(g, nil, toolName)
}

func toolDecisionFromParsed(g *spec.ProjectGraph, toolName string, parsed *toolSpecRisk) policy.ToolDecision {
	if g != nil {
		for _, pr := range g.Policies {
			if pr == nil {
				continue
			}
			td := policy.EffectiveToolDecision(g, &pr.Spec, toolName)
			if td.Decision == policy.DecisionRequireApproval {
				return td
			}
		}
	}
	var safety *spec.ToolSafety
	src := policy.SourceFailClosedDefault
	if parsed != nil && parsed.Safety != nil {
		safety = &spec.ToolSafety{
			Trusted:          parsed.Safety.Trusted,
			SideEffects:      parsed.Safety.SideEffects,
			RequiresApproval: parsed.Safety.RequiresApproval,
		}
		src = policy.SourceSafetyMetadata
	}
	resolved := spec.ResolveToolSafety(safety)
	return policy.ToolDecision{
		Decision: policy.Derive(resolved),
		Source:   src,
		Safety:   resolved,
	}
}

func addToolSafetyRisk(sink *riskSink, toolName string, cur policy.ToolDecision, prev *policy.ToolDecision, target RiskTarget, wit []WitnessHop) {
	if cur.Decision != policy.DecisionRequireApproval {
		return
	}
	if prev != nil && prev.Decision == policy.DecisionRequireApproval {
		return
	}
	// Plan uses prefix match on tool.<name>. for explicit requiredFor (conservative); runtime matches exact uses.
	sink.add(RiskItem{
		Category: RiskCategorySafety,
		Severity: RiskSeverityMedium,
		Reason: fmt.Sprintf(
			"Tool/%s will require approval at run (decision=%s, source=%s).",
			toolName, cur.Decision, cur.Source,
		),
		Target:  target,
		Witness: wit,
	})
}

func parsePolicySpec(resourceJSON string) (*policySpecRisk, bool) {
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(resourceJSON), &env); err != nil {
		return nil, false
	}
	var p policySpecRisk
	if err := json.Unmarshal(env.Spec, &p); err != nil {
		return nil, false
	}
	return &p, true
}

func parseAgentSpec(resourceJSON string) (*agentSpecRisk, bool) {
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(resourceJSON), &env); err != nil {
		return nil, false
	}
	var a agentSpecRisk
	if err := json.Unmarshal(env.Spec, &a); err != nil {
		return nil, false
	}
	return &a, true
}

func parseToolSpec(resourceJSON string) (*toolSpecRisk, bool) {
	var env jsonEnvelope
	if err := json.Unmarshal([]byte(resourceJSON), &env); err != nil {
		return nil, false
	}
	var t toolSpecRisk
	if err := json.Unmarshal(env.Spec, &t); err != nil {
		return nil, false
	}
	return &t, true
}

func policyMaxCost(p *policySpecRisk) float64 {
	if p == nil || p.Execution == nil {
		return 0
	}
	return p.Execution.MaxTotalCostUsd
}

func policyMaxWall(p *policySpecRisk) int {
	if p == nil || p.Execution == nil {
		return 0
	}
	return p.Execution.MaxWallClockSeconds
}

func policyApprovals(p *policySpecRisk) []string {
	if p == nil || p.Approvals == nil {
		return nil
	}
	return p.Approvals.RequiredFor
}

func addEffectPermitWidening(sink *riskSink, name string, target RiskTarget, wit []WitnessHop, oldPol, newPol *policySpecRisk) {
	oldUnattended := policyUnattendedEffectPermits(oldPol)
	oldAllowed := policyAnyEffectPermits(oldPol)
	flagged := map[string]struct{}{}
	flag := func(ident string) {
		ident = strings.TrimSpace(ident)
		if ident == "" {
			return
		}
		if _, ok := flagged[ident]; ok {
			return
		}
		flagged[ident] = struct{}{}
		sink.add(RiskItem{
			Category: RiskCategoryEffectPermitWidening,
			Severity: RiskSeverityHigh,
			Reason:   fmt.Sprintf("Effect permit widened with %q (Policy/%s).", ident, name),
			Target:   target,
			Witness:  wit,
		})
	}
	// Unattended permit not already covered by an old unattended permit
	// (includes promoting an ident from permitWithApproval to permit).
	for _, ident := range policyUnattendedEffectPermits(newPol) {
		if effectIdentCovered(oldUnattended, ident) {
			continue
		}
		flag(ident)
	}
	// Newly allowed at all (either list) vs the old union.
	for _, ident := range policyAnyEffectPermits(newPol) {
		if effectIdentCovered(oldAllowed, ident) {
			continue
		}
		flag(ident)
	}
}

func policyAnyEffectPermits(p *policySpecRisk) []string {
	if p == nil || p.Effects == nil {
		return nil
	}
	var out []string
	out = append(out, p.Effects.Permit...)
	out = append(out, p.Effects.PermitWithApproval...)
	return out
}

func policyUnattendedEffectPermits(p *policySpecRisk) []string {
	if p == nil || p.Effects == nil {
		return nil
	}
	var out []string
	for _, ident := range p.Effects.Permit {
		if effectIdentCovered(p.Effects.PermitWithApproval, ident) {
			continue
		}
		out = append(out, ident)
	}
	return out
}

func effectIdentCovered(list []string, ident string) bool {
	ident = strings.TrimSpace(ident)
	if ident == "" {
		return false
	}
	for _, p := range list {
		if spec.EffectCovers(p, ident) {
			return true
		}
	}
	return false
}

func agentTools(a *agentSpecRisk) []string {
	if a == nil {
		return nil
	}
	out := make([]string, 0, len(a.Tools))
	for _, t := range a.Tools {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func toolAllows(t *toolSpecRisk) []string {
	if t == nil || t.Permissions == nil {
		return nil
	}
	return t.Permissions.Allow
}

func toolHasWriteLikeAllow(g *spec.ProjectGraph, toolName string) bool {
	if g == nil {
		return false
	}
	tr := g.Tools[toolName]
	if tr == nil || tr.Spec.Permissions == nil {
		return false
	}
	for _, a := range tr.Spec.Permissions.Allow {
		if ActionSuggestsWriteSideEffects(a) {
			return true
		}
	}
	return false
}

func containsString(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}
