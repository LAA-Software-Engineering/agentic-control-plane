package raise

import (
	"sort"

	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/spec"
)

// tool raises a ToolResource to a lang.ToolDecl (issue #440). Fields with no .agent form
// (permissions, retry, limits, per-op schema, workspace) are refused, not dropped.
func (r *raiser) tool(t *spec.ToolResource) *lang.ToolDecl {
	d := &lang.ToolDecl{Name: ident(t.Metadata.Name)}
	s := t.Spec
	if s.Type != "" {
		d.Type = ident(s.Type)
	}
	if m := s.MCP; m != nil {
		d.MCP = &lang.ToolMCPBlock{
			Transport: strLitOrNil(m.Transport),
			Command:   strLitOrNil(m.Command),
			Args:      strLitList(m.Args),
			URL:       strLitOrNil(m.URL),
			Headers:   headerPairs(m.Headers),
		}
	}
	if h := s.HTTP; h != nil {
		d.HTTP = &lang.ToolHTTPBlock{
			BaseURL: strLitOrNil(h.BaseURL),
			Headers: headerPairs(h.Headers),
		}
	}
	if w := s.Workspace; w != nil {
		d.Workspace = &lang.ToolWorkspaceBlock{
			Root:        strLitOrNil(w.Root),
			TestCommand: strLitOrNil(w.TestCommand),
		}
	}
	if r := s.Retry; r != nil {
		rb := &lang.ToolRetryBlock{Backoff: strLitOrNil(r.Backoff)}
		if r.MaxAttempts != 0 {
			v := r.MaxAttempts
			rb.MaxAttempts = &v
		}
		d.Retry = rb
	}
	if sf := s.Safety; sf != nil {
		d.Safety = &lang.ToolSafetyBlock{Trusted: sf.Trusted, SideEffects: sf.SideEffects, RequiresApproval: sf.RequiresApproval}
	}
	if s.OperationsDeclared {
		ops := &lang.ToolOperations{}
		for _, name := range sortedKeys(s.Operations) {
			op := s.Operations[name]
			decl := &lang.ToolOperationDecl{Name: ident(name), Schema: strLitOrNil(op.Schema)}
			for _, e := range op.Effects {
				decl.Effects = append(decl.Effects, &lang.EffectRef{Name: e})
			}
			ops.Ops = append(ops.Ops, decl)
		}
		d.Operations = ops
	}
	if s.Permissions != nil {
		// Removed from the canonical model in a follow-up (ADR 007 step 1); still refused here until then.
		r.reject("Tool", t.Metadata.Name, "spec.permissions", "tool permissions")
	}
	if s.Limits != nil {
		r.reject("Tool", t.Metadata.Name, "spec.limits", "per-tool execution limits")
	}
	return d
}

// policy raises a PolicyResource to a lang.PolicyDecl (issue #440). ResolvedPreset is derived and
// skipped; tools.forbidUnknownTools and security have no .agent form and are refused.
func (r *raiser) policy(p *spec.PolicyResource) *lang.PolicyDecl {
	d := &lang.PolicyDecl{Name: ident(p.Metadata.Name)}
	s := p.Spec
	if s.Preset != "" {
		d.Preset = ident(s.Preset)
	}
	if e := s.Execution; e != nil {
		d.Execution = execution(e)
	}
	if a := s.Approvals; a != nil {
		d.Approvals = approvals(a)
	}
	if e := s.Effects; e != nil {
		d.Effects = &lang.PolicyEffectsBlock{Permit: effectRefs(e.Permit), PermitWithApproval: effectRefs(e.PermitWithApproval)}
	}
	if s.Hitl != nil {
		d.Hitl = r.hitl(p.Metadata.Name, s.Hitl)
	}
	if t := s.Tools; t != nil {
		v := t.ForbidUnknownTools
		d.Tools = &lang.PolicyToolsBlock{ForbidUnknownTools: &v}
	}
	if s.Security != nil {
		// Removed from the canonical model in a follow-up (ADR 007 step 1); still refused here until then.
		r.reject("Policy", p.Metadata.Name, "spec.security", "network/secret access controls")
	}
	return d
}

func execution(e *spec.PolicyExecution) *lang.PolicyExecutionBlock {
	b := &lang.PolicyExecutionBlock{}
	if e.MaxTotalCostUsd != 0 {
		v := e.MaxTotalCostUsd
		b.MaxTotalCostUsd = &v
	}
	if e.MaxWallClockSeconds != 0 {
		v := e.MaxWallClockSeconds
		b.MaxWallClockSeconds = &v
	}
	if e.RequireStructuredOutput {
		v := true
		b.RequireStructuredOutput = &v
	}
	return b
}

func approvals(a *spec.PolicyApprovals) *lang.PolicyApprovalsBlock {
	b := &lang.PolicyApprovalsBlock{RequireAllTools: a.RequireAllTools, Permissive: a.Permissive}
	for _, uses := range a.RequiredFor {
		if g := grant(uses); g != nil {
			b.RequiredFor = append(b.RequiredFor, g)
		}
	}
	return b
}

func effectRefs(names []string) []*lang.EffectRef {
	var out []*lang.EffectRef
	for _, n := range names {
		out = append(out, &lang.EffectRef{Name: n})
	}
	return out
}

// hitl raises a spec.HitlPolicy to a lang.HitlBlock (issue #440), the inverse of lower.lowerHitl.
func (r *raiser) hitl(policyName string, h *spec.HitlPolicy) *lang.HitlBlock {
	b := &lang.HitlBlock{
		DescriptionPrefix: strLitOrNil(h.DescriptionPrefix),
		RedactKeys:        strLitList(h.RedactKeys),
		ToolSwitchMap:     switchMapEntries(h.ToolSwitchMap),
	}
	for _, name := range sortedKeys(h.InterruptOn) {
		v := h.InterruptOn[name]
		e := &lang.InterruptEntry{Name: ident(name)}
		if v.Config != nil {
			e.Config = interruptConfig(v.Config)
		}
		b.InterruptOn = append(b.InterruptOn, e)
	}
	return b
}

func interruptConfig(c *spec.HitlInterruptConfig) *lang.InterruptConfig {
	out := &lang.InterruptConfig{
		Description:      strLitOrNil(c.Description),
		AllowedEditArgs:  strLitList(c.AllowedEditArgs),
		DeniedEditArgs:   strLitList(c.DeniedEditArgs),
		AllowedEditPaths: strLitList(c.AllowedEditPaths),
		DeniedEditPaths:  strLitList(c.DeniedEditPaths),
		AllowedEditTools: strLitList(c.AllowedEditTools),
		SwitchMap:        switchMapEntries(c.SwitchMap),
		RedactKeys:       strLitList(c.RedactKeys),
	}
	for _, d := range c.AllowedDecisions {
		out.AllowedDecisions = append(out.AllowedDecisions, ident(string(d)))
	}
	return out
}

// switchMapEntries raises a map[source][]targets into ordered SwitchMapEntry nodes (sorted by source
// for determinism; the source/targets are dotted operation names joined into single Idents).
func switchMapEntries(m map[string][]string) []*lang.SwitchMapEntry {
	if len(m) == 0 {
		return nil
	}
	var out []*lang.SwitchMapEntry
	for _, src := range sortedKeys(m) {
		e := &lang.SwitchMapEntry{Source: ident(src)}
		for _, t := range m[src] {
			e.Targets = append(e.Targets, ident(t))
		}
		out = append(out, e)
	}
	return out
}

// environment raises an EnvironmentResource to a lang.EnvironmentDecl (issue #440).
func (r *raiser) environment(e *spec.EnvironmentResource) *lang.EnvironmentDecl {
	d := &lang.EnvironmentDecl{Name: ident(e.Metadata.Name)}
	ov := e.Spec.Overrides
	if ov == nil {
		return d
	}
	block := &lang.EnvOverridesBlock{}
	for _, name := range sortedKeys(ov.Agents) {
		a := ov.Agents[name]
		entry := &lang.AgentOverrideEntry{Name: ident(name)}
		if a.Model != "" {
			entry.Model = modelRef(a.Model)
		}
		if a.Constraints != nil {
			entry.Constraints = constraints(a.Constraints)
		}
		block.Agents = append(block.Agents, entry)
	}
	for _, name := range sortedKeys(ov.Policies) {
		p := ov.Policies[name]
		entry := &lang.PolicyOverrideEntry{Name: ident(name)}
		if p.Execution != nil {
			entry.Execution = execution(p.Execution)
		}
		if p.Approvals != nil {
			entry.Approvals = approvals(p.Approvals)
		}
		block.Policies = append(block.Policies, entry)
	}
	d.Overrides = block
	return d
}

// provider raises one ProjectSpec.Providers.Models entry to a lang.ProviderDecl (issue #440).
func (r *raiser) provider(alias string, cfg spec.ModelProviderConfig) *lang.ProviderDecl {
	return &lang.ProviderDecl{
		Name:            ident(alias),
		Type:            ident(cfg.Type),
		APIKeyFrom:      strLitOrNil(cfg.APIKeyFrom),
		WorkspaceIDFrom: strLitOrNil(cfg.WorkspaceIDFrom),
	}
}

// --- value helpers ----------------------------------------------------------

func strLitOrNil(v string) *lang.StringLit {
	if v == "" {
		return nil
	}
	return &lang.StringLit{Value: v}
}

func strLitList(items []string) []*lang.StringLit {
	if len(items) == 0 {
		return nil
	}
	out := make([]*lang.StringLit, len(items))
	for i, s := range items {
		out[i] = &lang.StringLit{Value: s}
	}
	return out
}

// headerPairs raises a header map to ordered key/value pairs, sorted by key for deterministic output.
func headerPairs(h map[string]string) []*lang.HeaderPair {
	if len(h) == 0 {
		return nil
	}
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*lang.HeaderPair, len(keys))
	for i, k := range keys {
		out[i] = &lang.HeaderPair{Key: &lang.StringLit{Value: k}, Value: &lang.StringLit{Value: h[k]}}
	}
	return out
}
