package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"
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
		MaxTotalCostUsd float64 `json:"maxTotalCostUsd"`
	} `json:"execution"`
	Approvals *struct {
		RequiredFor []string `json:"requiredFor"`
	} `json:"approvals"`
}

type agentSpecRisk struct {
	Model string `json:"model"`
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

func summarizeRisks(
	g *spec.ProjectGraph,
	appliedByID map[string]state.AppliedResource,
	desiredByID map[string]desiredRow,
	ops []Operation,
) RiskSummary {
	seen := map[string]struct{}{}
	var msgs []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		msgs = append(msgs, s)
	}

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
			summarizePolicyRisk(add, op, oldJSON, des.json, hadPrev)
		case spec.KindAgent:
			summarizeAgentRisk(add, op, oldJSON, des.json, hadPrev)
		case spec.KindTool:
			summarizeToolRisk(add, g, op, oldJSON, des.json, hadPrev)
		}
	}

	sort.Strings(msgs)
	return RiskSummary{Messages: msgs}
}

func mergePolicyLintRisk(g *spec.ProjectGraph, risk RiskSummary) RiskSummary {
	findings := policy.Lint(g)
	if len(findings) == 0 {
		return risk
	}
	seen := make(map[string]struct{}, len(risk.Messages))
	for _, m := range risk.Messages {
		seen[m] = struct{}{}
	}
	for _, f := range findings {
		msg := policy.FormatLintMessage(f)
		if _, ok := seen[msg]; ok {
			continue
		}
		seen[msg] = struct{}{}
		risk.Messages = append(risk.Messages, msg)
	}
	sort.Strings(risk.Messages)
	risk.Lint = findings
	return risk
}

func summarizePolicyRisk(add func(string), op Operation, oldJSON, newJSON string, hadPrev bool) {
	newPol, ok := parsePolicySpec(newJSON)
	if !ok {
		return
	}
	newCost := policyMaxCost(newPol)
	newApprovals := policyApprovals(newPol)

	if op.Action == ActionCreate || !hadPrev {
		if newCost > 0 {
			add(fmt.Sprintf("New policy defines a cost ceiling (Policy/%s).", op.Target.Name))
		}
		if len(newApprovals) > 0 {
			add(fmt.Sprintf("New policy defines approval requirements (Policy/%s).", op.Target.Name))
		}
		return
	}

	oldPol, ok := parsePolicySpec(oldJSON)
	if !ok {
		return
	}
	oldCost := policyMaxCost(oldPol)
	oldApprovals := policyApprovals(oldPol)

	if newCost > oldCost+1e-9 {
		add(fmt.Sprintf("Cost ceiling increased (Policy/%s).", op.Target.Name))
	}
	for _, a := range oldApprovals {
		if !containsString(newApprovals, a) {
			add(fmt.Sprintf("Approval requirements removed for actions (Policy/%s).", op.Target.Name))
			break
		}
	}
}

func summarizeAgentRisk(add func(string), op Operation, oldJSON, newJSON string, hadPrev bool) {
	newAg, ok := parseAgentSpec(newJSON)
	if !ok {
		return
	}
	newModel := strings.TrimSpace(newAg.Model)

	if op.Action == ActionCreate || !hadPrev {
		if newModel != "" {
			add(fmt.Sprintf("New agent binds a model (Agent/%s).", op.Target.Name))
		}
		return
	}

	oldAg, ok := parseAgentSpec(oldJSON)
	if !ok {
		return
	}
	oldModel := strings.TrimSpace(oldAg.Model)
	if newModel != oldModel && (newModel != "" || oldModel != "") {
		add(fmt.Sprintf("Agent model changed (Agent/%s).", op.Target.Name))
	}
}

func summarizeToolRisk(add func(string), g *spec.ProjectGraph, op Operation, oldJSON, newJSON string, hadPrev bool) {
	newTool, ok := parseToolSpec(newJSON)
	if !ok {
		return
	}
	newAllows := toolAllows(newTool)
	newDecision := toolPlanDecisionFromGraph(g, op.Target.Name)

	if op.Action == ActionCreate || !hadPrev {
		for _, a := range newAllows {
			if ActionSuggestsWriteSideEffects(a) {
				add(fmt.Sprintf("New tool may grant write-like permissions (Tool/%s); see ActionSuggestsWriteSideEffects.", op.Target.Name))
				break
			}
		}
		addToolSafetyRisk(add, op.Target.Name, newDecision, nil)
		return
	}

	oldTool, ok := parseToolSpec(oldJSON)
	if !ok {
		return
	}
	oldAllows := toolAllows(oldTool)
	oldSet := make(map[string]struct{}, len(oldAllows))
	for _, a := range oldAllows {
		oldSet[strings.TrimSpace(a)] = struct{}{}
	}
	for _, a := range newAllows {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := oldSet[a]; ok {
			continue
		}
		if ActionSuggestsWriteSideEffects(a) {
			add(fmt.Sprintf("New write-like tool permissions added (Tool/%s); see ActionSuggestsWriteSideEffects.", op.Target.Name))
			break
		}
	}
	oldDecision := toolDecisionFromParsed(g, op.Target.Name, oldTool)
	addToolSafetyRisk(add, op.Target.Name, newDecision, &oldDecision)
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

func addToolSafetyRisk(add func(string), toolName string, cur policy.ToolDecision, prev *policy.ToolDecision) {
	if cur.Decision != policy.DecisionRequireApproval {
		return
	}
	if prev != nil && prev.Decision == policy.DecisionRequireApproval {
		return
	}
	// Plan uses prefix match on tool.<name>. for explicit requiredFor (conservative); runtime matches exact uses.
	add(fmt.Sprintf(
		"Tool/%s will require approval at run (decision=%s, source=%s).",
		toolName, cur.Decision, cur.Source,
	))
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

func policyApprovals(p *policySpecRisk) []string {
	if p == nil || p.Approvals == nil {
		return nil
	}
	return p.Approvals.RequiredFor
}

func toolAllows(t *toolSpecRisk) []string {
	if t == nil || t.Permissions == nil {
		return nil
	}
	return t.Permissions.Allow
}

func containsString(slice []string, want string) bool {
	for _, s := range slice {
		if s == want {
			return true
		}
	}
	return false
}
