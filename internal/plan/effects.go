package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LAA-Software-Engineering/terfyn/internal/effects"
	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
	"github.com/LAA-Software-Engineering/terfyn/internal/state"
)

type identKey struct {
	unknown bool
	ident   string
	uses    string
}

type identState struct {
	key        identKey
	reach      effects.Reachability
	witness    []effects.Hop
	rootKind   effects.HopKind
	rootName   string
	uses       string
	unknown    bool
	unknownMsg string
}

type effectAuthorityResult struct {
	bound []BoundSection
	auth  AuthorityDelta
	items []RiskItem
}

func attachEffectAuthority(desired *spec.ProjectGraph, applied []state.AppliedResource) effectAuthorityResult {
	desiredBounds := effects.Compute(desired)
	emptyBaseline := len(applied) == 0
	var deployedBounds effects.GraphBounds
	if !emptyBaseline {
		deployedBounds = effects.Compute(graphFromApplied(applied))
	}

	desiredIdents := collectIdents(desiredBounds)
	deployedIdents := collectIdents(deployedBounds)
	desiredCaps := agentCapabilities(desired)
	deployedCaps := agentCapabilities(graphFromApplied(applied))
	if emptyBaseline {
		deployedCaps = map[string][]string{}
	}

	bound := boundSections(desiredBounds)
	items := make([]RiskItem, 0)
	items = append(items, capabilityDeltaItems(desiredCaps, deployedCaps)...)
	items = append(items, effectDeltaItems(desiredIdents, deployedIdents)...)

	auth := compareAuthority(desiredIdents, deployedIdents, desiredCaps, deployedCaps)
	auth.EmptyBaseline = emptyBaseline
	if auth.Autonomous == AuthorityWidened {
		items = append(items, RiskItem{
			Category:     RiskCategoryAuthorityWidening,
			Severity:     RiskSeverityHigh,
			Reason:       "AUTONOMOUS authority WIDENED.",
			Target:       authorityTarget(desiredBounds),
			Reachability: WitnessAutonomous,
		})
	}
	if auth.Static == AuthorityWidened {
		items = append(items, RiskItem{
			Category:     RiskCategoryAuthorityWidening,
			Severity:     RiskSeverityMedium,
			Reason:       "STATIC authority WIDENED.",
			Target:       authorityTarget(desiredBounds),
			Reachability: WitnessStatic,
		})
	}
	return effectAuthorityResult{bound: bound, auth: auth, items: items}
}

func mergeEffectAuthority(risk RiskSummary, extra []RiskItem) RiskSummary {
	if len(extra) == 0 {
		return risk
	}
	sink := newRiskSink()
	for _, it := range risk.Items {
		sink.add(it)
	}
	for _, it := range extra {
		sink.add(it)
	}
	out := finalizeRiskItems(sink.items)
	out.Lint = risk.Lint
	return out
}

func graphFromApplied(rows []state.AppliedResource) *spec.ProjectGraph {
	g := &spec.ProjectGraph{
		Agents:       map[string]*spec.AgentResource{},
		Tools:        map[string]*spec.ToolResource{},
		Workflows:    map[string]*spec.WorkflowResource{},
		Policies:     map[string]*spec.PolicyResource{},
		Environments: map[string]*spec.EnvironmentResource{},
	}
	for _, r := range rows {
		raw := strings.TrimSpace(r.NormalizedSpecJSON)
		if raw == "" {
			continue
		}
		switch r.Kind {
		case spec.KindProject:
			var p spec.ProjectResource
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				continue
			}
			g.Meta = p.Metadata
			g.Spec = p.Spec
		case spec.KindAgent:
			var a spec.AgentResource
			if err := json.Unmarshal([]byte(raw), &a); err != nil {
				continue
			}
			if a.Metadata.Name == "" {
				a.Metadata.Name = r.Name
			}
			g.Agents[a.Metadata.Name] = &a
		case spec.KindTool:
			var t spec.ToolResource
			if err := json.Unmarshal([]byte(raw), &t); err != nil {
				continue
			}
			if t.Metadata.Name == "" {
				t.Metadata.Name = r.Name
			}
			g.Tools[t.Metadata.Name] = &t
		case spec.KindWorkflow:
			var w spec.WorkflowResource
			if err := json.Unmarshal([]byte(raw), &w); err != nil {
				continue
			}
			if w.Metadata.Name == "" {
				w.Metadata.Name = r.Name
			}
			g.Workflows[w.Metadata.Name] = &w
		case spec.KindPolicy:
			var p spec.PolicyResource
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				continue
			}
			if p.Metadata.Name == "" {
				p.Metadata.Name = r.Name
			}
			g.Policies[p.Metadata.Name] = &p
		case spec.KindEnvironment:
			var e spec.EnvironmentResource
			if err := json.Unmarshal([]byte(raw), &e); err != nil {
				continue
			}
			if e.Metadata.Name == "" {
				e.Metadata.Name = r.Name
			}
			g.Environments[e.Metadata.Name] = &e
		}
	}
	return g
}

func boundSections(gb effects.GraphBounds) []BoundSection {
	var out []BoundSection
	for _, name := range sortedKeys(gb.Workflows) {
		sec := boundSectionFrom(gb.Workflows[name])
		if len(sec.Items) > 0 {
			out = append(out, sec)
		}
	}
	for _, name := range sortedKeys(gb.Agents) {
		sec := boundSectionFrom(gb.Agents[name])
		if len(sec.Items) > 0 {
			out = append(out, sec)
		}
	}
	return out
}

func boundSectionFrom(b effects.Bound) BoundSection {
	target := boundTarget(b.RootKind, b.RootName)
	var items []RiskItem
	for _, e := range b.Effects {
		reach := effectReachability(e.Witness)
		ident := e.Ident
		if e.Unknown {
			ident = "unknown"
		}
		detail := boundReachDetail(e, reach)
		items = append(items, RiskItem{
			Category:     RiskCategoryEffectBound,
			Severity:     boundSeverity(reach, e.Unknown),
			Reason:       formatBoundLine(ident, string(reach), detail),
			Target:       target,
			Witness:      witnessFromEffects(e.Witness),
			Ident:        ident,
			Reachability: WitnessReachability(reach),
		})
	}
	reachable := map[string]struct{}{}
	for _, e := range b.Effects {
		if !e.Unknown && e.Ident != "" {
			reachable[e.Ident] = struct{}{}
		}
	}
	for _, u := range b.Unreachable {
		ident := u.Ident
		if u.Unknown || ident == "" {
			ident = "unknown"
		} else if _, ok := reachable[ident]; ok {
			continue
		}
		detail := "no grant path to " + u.Uses
		items = append(items, RiskItem{
			Category: RiskCategoryEffectBound,
			Severity: RiskSeverityLow,
			Reason:   formatBoundLine(ident, "unreachable", detail),
			Target:   target,
			Ident:    ident,
		})
	}
	return BoundSection{
		RootKind: string(b.RootKind),
		RootName: b.RootName,
		Items:    items,
	}
}

func boundSeverity(r effects.Reachability, unknown bool) RiskSeverity {
	if r == effects.Autonomous || unknown {
		return RiskSeverityHigh
	}
	return RiskSeverityMedium
}

func formatBoundLine(ident, reach, detail string) string {
	ident = strings.TrimSpace(ident)
	reach = strings.TrimSpace(reach)
	detail = strings.TrimSpace(detail)
	return fmt.Sprintf("%-18s %-11s %s", ident, reach, detail)
}

func boundReachDetail(e effects.Effect, reach effects.Reachability) string {
	agentName, stepID := hopsAgentAndStep(e.Witness)
	uses := strings.TrimSpace(e.Uses)
	if reach == effects.Autonomous {
		if agentName != "" && uses != "" {
			return fmt.Sprintf("Agent/%s may select %s", agentName, uses)
		}
		if uses != "" {
			return "may select " + uses
		}
		if e.Unknown {
			return strings.TrimSpace(e.Message)
		}
		return "autonomous grant"
	}
	if stepID != "" {
		return "step " + stepID
	}
	if uses != "" {
		return uses
	}
	if e.Unknown {
		return strings.TrimSpace(e.Message)
	}
	return "static uses"
}

func hopsAgentAndStep(hops []effects.Hop) (agentName, stepID string) {
	for _, h := range hops {
		switch h.Kind {
		case effects.KindAgent:
			if agentName == "" {
				agentName = h.Name
			}
		case effects.KindStep:
			if stepID == "" {
				if h.ID != "" {
					stepID = h.ID
				} else {
					stepID = h.Name
				}
			}
		}
	}
	return agentName, stepID
}

func effectReachability(hops []effects.Hop) effects.Reachability {
	for _, h := range hops {
		if h.Reachability == effects.Autonomous {
			return effects.Autonomous
		}
	}
	return effects.Static
}

func witnessFromEffects(hops []effects.Hop) []WitnessHop {
	if len(hops) == 0 {
		return nil
	}
	out := make([]WitnessHop, len(hops))
	for i, h := range hops {
		out[i] = WitnessHop{
			Kind:         WitnessHopKind(h.Kind),
			Name:         h.Name,
			ID:           h.ID,
			Reachability: WitnessReachability(h.Reachability),
		}
	}
	return out
}

func boundTarget(kind effects.HopKind, name string) RiskTarget {
	switch kind {
	case effects.KindWorkflow:
		return RiskTarget{Kind: RiskTargetWorkflow, Name: name}
	default:
		return RiskTarget{Kind: RiskTargetAgent, Name: name}
	}
}

func collectIdents(gb effects.GraphBounds) map[identKey]identState {
	out := map[identKey]identState{}
	merge := func(b effects.Bound) {
		for _, e := range b.Effects {
			key := identKey{ident: e.Ident}
			if e.Unknown {
				key = identKey{unknown: true, uses: e.Uses}
			}
			reach := effectReachability(e.Witness)
			prev, ok := out[key]
			if ok && !(reach == effects.Autonomous && prev.reach != effects.Autonomous) {
				continue
			}
			out[key] = identState{
				key:        key,
				reach:      reach,
				witness:    e.Witness,
				rootKind:   b.RootKind,
				rootName:   b.RootName,
				uses:       e.Uses,
				unknown:    e.Unknown,
				unknownMsg: e.Message,
			}
		}
	}
	for _, name := range sortedKeys(gb.Workflows) {
		merge(gb.Workflows[name])
	}
	for _, name := range sortedKeys(gb.Agents) {
		merge(gb.Agents[name])
	}
	return out
}

func agentCapabilities(g *spec.ProjectGraph) map[string][]string {
	out := map[string][]string{}
	if g == nil {
		return out
	}
	for _, name := range sortedKeys(g.Agents) {
		agent := g.Agents[name]
		if agent == nil {
			continue
		}
		listed, err := spec.ResolveAgentAdvertisedTools(agent, g.Tools)
		if err != nil {
			var uses []string
			for _, raw := range agent.Spec.Tools {
				raw = strings.TrimSpace(raw)
				if raw != "" {
					uses = append(uses, raw)
				}
			}
			sort.Strings(uses)
			out[name] = uses
			continue
		}
		uses := make([]string, 0, len(listed))
		for _, a := range listed {
			u := strings.TrimSpace(a.Uses)
			if u != "" {
				uses = append(uses, u)
			}
		}
		sort.Strings(uses)
		if len(uses) > 0 {
			out[name] = uses
		}
	}
	return out
}

func capabilityDeltaItems(desired, deployed map[string][]string) []RiskItem {
	var items []RiskItem
	for _, name := range sortedKeys(desired) {
		oldSet := stringSet(deployed[name])
		for _, uses := range desired[name] {
			if _, ok := oldSet[uses]; ok {
				continue
			}
			wit := []WitnessHop{
				{Kind: WitnessKindAgent, Name: name, Reachability: WitnessStatic},
				{Kind: WitnessKindToolOperation, Name: uses, Reachability: WitnessAutonomous},
			}
			items = append(items, RiskItem{
				Category:     RiskCategoryCapabilityDelta,
				Severity:     RiskSeverityHigh,
				Reason:       fmt.Sprintf("Agent/%s gained %s.", name, uses),
				Target:       RiskTarget{Kind: RiskTargetAgent, Name: name},
				Witness:      wit,
				Ident:        uses,
				Reachability: WitnessAutonomous,
			})
		}
	}
	return items
}

func effectDeltaItems(desired, deployed map[identKey]identState) []RiskItem {
	var keys []identKey
	for k := range desired {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].unknown != keys[j].unknown {
			return keys[i].unknown
		}
		if keys[i].ident != keys[j].ident {
			return keys[i].ident < keys[j].ident
		}
		return keys[i].uses < keys[j].uses
	})
	var items []RiskItem
	for _, k := range keys {
		des := desired[k]
		prev, had := deployed[k]
		label := des.key.ident
		if des.unknown {
			label = "unknown"
			if des.uses != "" {
				label = "unknown (" + des.uses + ")"
			}
		}
		target := boundTarget(des.rootKind, des.rootName)
		wit := witnessFromEffects(des.witness)
		reach := WitnessReachability(des.reach)
		switch {
		case !had:
			sev := RiskSeverityMedium
			reason := fmt.Sprintf("Newly static effect %s (%s/%s).", label, des.rootKind, des.rootName)
			if des.reach == effects.Autonomous {
				sev = RiskSeverityHigh
				reason = fmt.Sprintf("Newly autonomous effect %s (%s/%s).", label, des.rootKind, des.rootName)
			}
			items = append(items, RiskItem{
				Category:     RiskCategoryEffectDelta,
				Severity:     sev,
				Reason:       reason,
				Target:       target,
				Witness:      wit,
				Ident:        label,
				Reachability: reach,
			})
		case des.reach == effects.Autonomous && prev.reach != effects.Autonomous:
			items = append(items, RiskItem{
				Category:     RiskCategoryEffectDelta,
				Severity:     RiskSeverityHigh,
				Reason:       fmt.Sprintf("Effect %s became autonomously reachable (was static) (%s/%s).", label, des.rootKind, des.rootName),
				Target:       target,
				Witness:      wit,
				Ident:        label,
				Reachability: WitnessAutonomous,
			})
		}
	}
	return items
}

func compareAuthority(desiredIdents, deployedIdents map[identKey]identState, desiredCaps, deployedCaps map[string][]string) AuthorityDelta {
	auth := AuthorityDelta{Static: AuthorityUnchanged, Autonomous: AuthorityUnchanged}
	for k, des := range desiredIdents {
		prev, had := deployedIdents[k]
		if !had {
			if des.reach == effects.Autonomous {
				auth.Autonomous = AuthorityWidened
			} else {
				auth.Static = AuthorityWidened
			}
			continue
		}
		if des.reach == effects.Autonomous && prev.reach != effects.Autonomous {
			auth.Autonomous = AuthorityWidened
		}
	}
	for name, uses := range desiredCaps {
		oldSet := stringSet(deployedCaps[name])
		for _, u := range uses {
			if _, ok := oldSet[u]; !ok {
				auth.Autonomous = AuthorityWidened
				break
			}
		}
	}
	return auth
}

func authorityTarget(gb effects.GraphBounds) RiskTarget {
	if names := sortedKeys(gb.Workflows); len(names) > 0 {
		return RiskTarget{Kind: RiskTargetWorkflow, Name: names[0]}
	}
	if names := sortedKeys(gb.Agents); len(names) > 0 {
		return RiskTarget{Kind: RiskTargetAgent, Name: names[0]}
	}
	return RiskTarget{}
}

func stringSet(vals []string) map[string]struct{} {
	out := make(map[string]struct{}, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		out[v] = struct{}{}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
