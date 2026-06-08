package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// SourcePresetBase marks a decision originating from a built-in policy preset (issue #118).
const SourcePresetBase DecisionSource = "preset_base"

// CompiledToolDecision is the static per-tool outcome after policy compilation.
type CompiledToolDecision struct {
	Decision Decision       `json:"decision"`
	Source   DecisionSource `json:"source"`
}

// ResidualPolicy captures dynamic predicates evaluated at run time after static lookup (issue #118).
type ResidualPolicy struct {
	ForbidUnknownTools bool                  `json:"forbidUnknownTools,omitempty"`
	Permissive         bool                  `json:"permissive,omitempty"`
	ShellSafe          bool                  `json:"shellSafe,omitempty"`
	RequireAllTools    bool                  `json:"requireAllTools,omitempty"`
	RequiredForExact   []string              `json:"requiredForExact,omitempty"`
	Execution          *spec.PolicyExecution `json:"execution,omitempty"`
	Hitl               *spec.HitlPolicy      `json:"hitl,omitempty"`
}

// CompiledPolicy is an immutable, content-addressed policy snapshot for a single Policy resource.
type CompiledPolicy struct {
	PolicyName string `json:"policyName"`
	Digest     string `json:"digest"`
	// Tools holds runtime static decisions (preset base and safety metadata only).
	Tools map[string]CompiledToolDecision `json:"tools"`
	// PlanTools holds conservative per-tool decisions for plan/review output (includes explicit requiredFor prefixes).
	PlanTools map[string]CompiledToolDecision `json:"planTools"`
	Residual  ResidualPolicy                  `json:"residual"`
}

// CompileReferenced builds compiled snapshots for every policy name referenced in graph.
func CompileReferenced(graph *spec.ProjectGraph) (map[string]*CompiledPolicy, error) {
	if graph == nil {
		return nil, fmt.Errorf("policy: nil project graph")
	}
	names := collectReferencedPolicyNames(graph)
	if len(names) == 0 {
		return map[string]*CompiledPolicy{}, nil
	}
	out := make(map[string]*CompiledPolicy, len(names))
	for _, name := range names {
		cp, err := Compile(graph, name)
		if err != nil {
			return nil, err
		}
		out[name] = cp
	}
	return out, nil
}

// Compile flattens preset base, explicit rules, and safety metadata into a per-tool decision map.
// Precedence: explicit rule > preset base > safety metadata > fail-closed default (issue #118).
func Compile(graph *spec.ProjectGraph, policyName string) (*CompiledPolicy, error) {
	policyName = strings.TrimSpace(policyName)
	if policyName == "" {
		return nil, fmt.Errorf("policy: empty policy name")
	}
	raw := resolvePolicy(graph, policyName)
	if raw == nil {
		return nil, fmt.Errorf("policy: unknown policy %q", policyName)
	}
	merged := *raw
	if strings.TrimSpace(merged.ResolvedPreset) == "" {
		resolved, err := spec.ResolvePolicySpec(&merged)
		if err != nil {
			return nil, fmt.Errorf("policy: resolve preset for %q: %w", policyName, err)
		}
		if resolved != nil {
			merged = *resolved
		}
	}

	tools := compileRuntimeToolDecisions(graph, &merged)
	planTools := compilePlanToolDecisions(graph, &merged)
	residual := buildResidual(&merged)

	cp := &CompiledPolicy{
		PolicyName: policyName,
		Tools:      tools,
		PlanTools:  planTools,
		Residual:   residual,
	}
	cp.Digest = digestCompiledPolicy(cp)
	return cp, nil
}

func compileRuntimeToolDecisions(graph *spec.ProjectGraph, pol *spec.PolicySpec) map[string]CompiledToolDecision {
	toolNames := sortedToolNames(graph)
	out := make(map[string]CompiledToolDecision, len(toolNames))
	for _, name := range toolNames {
		out[name] = compileRuntimeTool(graph, pol, name)
	}
	return out
}

func compilePlanToolDecisions(graph *spec.ProjectGraph, pol *spec.PolicySpec) map[string]CompiledToolDecision {
	toolNames := sortedToolNames(graph)
	out := make(map[string]CompiledToolDecision, len(toolNames))
	for _, name := range toolNames {
		td := EffectiveToolDecision(graph, pol, name)
		out[name] = CompiledToolDecision{Decision: td.Decision, Source: td.Source}
	}
	return out
}

func compileRuntimeTool(graph *spec.ProjectGraph, pol *spec.PolicySpec, toolName string) CompiledToolDecision {
	toolName = strings.TrimSpace(toolName)
	safety := resolvedSafetyForTool(graph, toolName)
	preset := spec.ResolvedPresetName(pol)
	if pol != nil && pol.Approvals != nil && spec.ApprovalPermissive(pol.Approvals) {
		return CompiledToolDecision{Decision: DecisionAllow, Source: presetSource(pol)}
	}
	if pol != nil && pol.Approvals != nil && spec.ApprovalRequireAllTools(pol.Approvals) {
		return CompiledToolDecision{Decision: DecisionRequireApproval, Source: presetSource(pol)}
	}
	if preset == spec.PresetShellSafe {
		if safety.RequiresApproval || safety.SideEffects {
			return CompiledToolDecision{Decision: DecisionRequireApproval, Source: SourcePresetBase}
		}
	}
	src := SourceSafetyMetadata
	if graph == nil || graph.Tools == nil {
		src = SourceFailClosedDefault
	} else if tr, ok := graph.Tools[toolName]; !ok || tr == nil || tr.Spec.Safety == nil {
		src = SourceFailClosedDefault
	}
	return CompiledToolDecision{
		Decision: Derive(safety),
		Source:   src,
	}
}

func presetSource(pol *spec.PolicySpec) DecisionSource {
	if spec.ResolvedPresetName(pol) != "" {
		return SourcePresetBase
	}
	return SourceExplicitPolicyRule
}

func buildResidual(pol *spec.PolicySpec) ResidualPolicy {
	if pol == nil {
		return ResidualPolicy{}
	}
	var rf []string
	if pol.Approvals != nil && len(pol.Approvals.RequiredFor) > 0 {
		rf = append([]string(nil), pol.Approvals.RequiredFor...)
		sort.Strings(rf)
	}
	var exec *spec.PolicyExecution
	if pol.Execution != nil {
		cp := *pol.Execution
		exec = &cp
	}
	var hitl *spec.HitlPolicy
	if pol.Hitl != nil {
		cp := *pol.Hitl
		hitl = &cp
	}
	return ResidualPolicy{
		ForbidUnknownTools: pol.Tools != nil && pol.Tools.ForbidUnknownTools,
		Permissive:         pol.Approvals != nil && spec.ApprovalPermissive(pol.Approvals),
		ShellSafe:          spec.ResolvedPresetName(pol) == spec.PresetShellSafe,
		RequireAllTools:    pol.Approvals != nil && spec.ApprovalRequireAllTools(pol.Approvals),
		RequiredForExact:   rf,
		Execution:          exec,
		Hitl:               hitl,
	}
}

func sortedToolNames(graph *spec.ProjectGraph) []string {
	if graph == nil || graph.Tools == nil {
		return nil
	}
	names := make([]string, 0, len(graph.Tools))
	for name := range graph.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collectReferencedPolicyNames(graph *spec.ProjectGraph) []string {
	seen := make(map[string]struct{})
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		seen[name] = struct{}{}
	}
	if graph.Spec.Defaults != nil {
		add(graph.Spec.Defaults.Policy)
	}
	for _, ar := range graph.Agents {
		if ar != nil {
			add(ar.Spec.Policy)
		}
	}
	for _, wr := range graph.Workflows {
		if wr != nil {
			add(wr.Spec.Policy)
		}
	}
	for name := range graph.Policies {
		add(name)
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type compiledDigestPayload struct {
	PolicyName string                          `json:"policyName"`
	Tools      map[string]CompiledToolDecision `json:"tools"`
	PlanTools  map[string]CompiledToolDecision `json:"planTools"`
	Residual   ResidualPolicy                  `json:"residual"`
}

func digestCompiledPolicy(cp *CompiledPolicy) string {
	if cp == nil {
		return ""
	}
	payload := compiledDigestPayload{
		PolicyName: cp.PolicyName,
		Tools:      cp.Tools,
		PlanTools:  cp.PlanTools,
		Residual:   cp.Residual,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// EffectivePolicyEntries returns sorted per-tool rows for plan output (issue #118).
func (cp *CompiledPolicy) EffectivePolicyEntries() []EffectivePolicyEntry {
	if cp == nil {
		return nil
	}
	src := cp.PlanTools
	if len(src) == 0 {
		src = cp.Tools
	}
	if len(src) == 0 {
		return nil
	}
	names := make([]string, 0, len(src))
	for name := range src {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]EffectivePolicyEntry, len(names))
	for i, name := range names {
		td := src[name]
		out[i] = EffectivePolicyEntry{
			Tool:     name,
			Decision: td.Decision,
			Source:   td.Source,
		}
	}
	return out
}

// EffectivePolicyEntry is one compiled per-tool decision for plan/review output.
type EffectivePolicyEntry struct {
	Tool     string         `json:"tool"`
	Decision Decision       `json:"decision"`
	Source   DecisionSource `json:"source"`
}

// SnapshotSetDigest returns a stable digest over all compiled policies keyed by name.
func SnapshotSetDigest(policies map[string]*CompiledPolicy) (string, error) {
	if len(policies) == 0 {
		raw, err := json.Marshal(map[string]string{})
		if err != nil {
			return "", fmt.Errorf("policy: marshal empty snapshot digest: %w", err)
		}
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:]), nil
	}
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make(map[string]string, len(names))
	for _, name := range names {
		cp := policies[name]
		if cp == nil {
			return "", fmt.Errorf("policy: nil compiled policy %q", name)
		}
		rows[name] = cp.Digest
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("policy: marshal snapshot set digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// DefaultPolicyName returns the project default policy name, or "default" when unset.
func DefaultPolicyName(graph *spec.ProjectGraph) string {
	if graph != nil && graph.Spec.Defaults != nil {
		if p := strings.TrimSpace(graph.Spec.Defaults.Policy); p != "" {
			return p
		}
	}
	return "default"
}
