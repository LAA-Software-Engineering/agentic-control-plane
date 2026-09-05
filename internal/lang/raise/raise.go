// Package raise is the inverse of internal/lang/lower: it reconstructs a .agent AST (lang.File) from
// a compiled spec.ProjectGraph so a YAML-authored project can be migrated to .agent source (issue
// #440, Phase 2b). lang.Print then renders canonical .agent text.
//
// Correctness discipline: raising is lossless or it refuses. Every spec field that has a .agent
// grammar form is reconstructed; any field that is set but has NO .agent form (e.g. ToolSpec.Permissions)
// is a hard error naming the resource and field, never a silent drop. The intended end-to-end invariant (checked by the migrate tool, not this package) is that
// re-lowering the raised AST reproduces the original graph — the mirror of the ADR 005 §2 equivalence
// goldens the forward path already guarantees.
package raise

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/lang/lower"
	"github.com/Terfyn/terfyn/internal/spec"
)

// Unsupported reports a spec construct that has no .agent authoring form and therefore cannot be
// migrated without losing information. The migrate tool surfaces these so the author can resolve
// them (keep the resource in YAML, or drop an obsolete field) rather than getting lossy output.
type Unsupported struct {
	Kind   string // resource kind, e.g. "Agent"
	Name   string // resource metadata name
	Field  string // the offending field path, e.g. "spec.memory"
	Detail string // human-readable explanation
}

func (u Unsupported) Error() string {
	return fmt.Sprintf("%s %q: %s (%s) has no .agent authoring form", u.Kind, u.Name, u.Field, u.Detail)
}

// raiser accumulates Unsupported findings while building the AST.
type raiser struct {
	unsupported []Unsupported
}

func (r *raiser) reject(kind, name, field, detail string) {
	r.unsupported = append(r.unsupported, Unsupported{Kind: kind, Name: name, Field: field, Detail: detail})
}

// Graph raises every resource in g to a .agent AST in a stable, source-order-independent order
// (providers, tools, policies, environments, agents, workflows — each sorted by name). It returns the
// assembled file plus any Unsupported findings; when findings are present the file is best-effort and
// must not be written as a faithful migration.
//
// Workflows are raised to behavioral/semantic equivalence, not byte-identical round-trip (ADR 007):
// interpolation strings become .agent expressions, the step DAG is linearized into a topological
// statement order, and the object-literal output becomes a `return { … }`. A construct with no .agent
// form (a step with a steps.<id>.meta reference, an array value, or a non-object output) yields an
// Unsupported so the workflow is refused rather than mistranslated (see workflows.go).
func Graph(g *spec.ProjectGraph) (*lang.File, []Unsupported) {
	r := &raiser{}
	f := &lang.File{}
	if g == nil {
		return f, nil
	}

	// Custom provider aliases (project config). Built-in namespaces need no declaration and are not
	// raised; the migrate tool drops them separately.
	if g.Spec.Providers != nil {
		for _, name := range sortedKeys(g.Spec.Providers.Models) {
			f.Decls = append(f.Decls, r.provider(name, g.Spec.Providers.Models[name]))
		}
	}
	// Project-wide defaults (project config). Raised only when at least one field is set, so an
	// all-empty spec.defaults does not print an empty block.
	if d := r.defaults(g.Spec.Defaults); d != nil {
		f.Decls = append(f.Decls, d)
	}
	// Project-wide execution-limit baseline (project config), raised only when at least one field is set.
	if d := r.projectLimits(g.Spec.Limits); d != nil {
		f.Decls = append(f.Decls, d)
	}
	for _, name := range sortedKeys(g.Tools) {
		f.Decls = append(f.Decls, r.tool(g.Tools[name]))
	}
	for _, name := range sortedKeys(g.Policies) {
		f.Decls = append(f.Decls, r.policy(g.Policies[name]))
	}
	for _, name := range sortedKeys(g.Environments) {
		f.Decls = append(f.Decls, r.environment(g.Environments[name]))
	}
	for _, name := range sortedKeys(g.Agents) {
		f.Decls = append(f.Decls, r.agent(g.Agents[name]))
	}
	for _, name := range sortedKeys(g.Workflows) {
		if wd, ok := r.workflow(g.Workflows[name]); ok {
			f.Decls = append(f.Decls, wd)
		}
	}
	return f, r.unsupported
}

// agent raises an AgentResource to a lang.AgentDecl (issue #440).
func (r *raiser) agent(a *spec.AgentResource) *lang.AgentDecl {
	d := &lang.AgentDecl{Name: ident(a.Metadata.Name)}
	s := a.Spec
	if s.Model != "" {
		d.Model = modelRef(s.Model)
	}
	if s.Policy != "" {
		d.Policy = ident(s.Policy)
	}
	if s.Description != "" {
		d.Description = strLit(s.Description)
	}
	if s.Instructions != "" {
		d.Instructions = strLit(s.Instructions)
	}
	if s.Constraints != nil {
		d.Constraints = constraints(s.Constraints)
	}
	for _, uses := range s.Tools {
		if g := grant(uses); g != nil {
			d.Grants = append(d.Grants, g)
		} else {
			r.reject("Agent", a.Metadata.Name, "spec.tools", fmt.Sprintf("grant %q is not a tool.<name>.<operation> path", uses))
		}
	}
	if s.Input != nil {
		if t := typeRefFromSchema(s.Input.Schema); t != "" {
			d.Input = &lang.TypeRef{Name: t}
		} else {
			r.reject("Agent", a.Metadata.Name, "spec.input.schema", fmt.Sprintf("schema ref %q is not the schemas/<Type>.json convention", s.Input.Schema))
		}
	}
	if s.Output != nil {
		if t := typeRefFromSchema(s.Output.Schema); t != "" {
			d.Output = &lang.TypeRef{Name: t}
		} else {
			r.reject("Agent", a.Metadata.Name, "spec.output.schema", fmt.Sprintf("schema ref %q is not the schemas/<Type>.json convention", s.Output.Schema))
		}
	}
	// spec.runtime and spec.memory were removed from the canonical model (ADR 007 step 1), so an
	// AgentSpec can no longer carry them; there is nothing to refuse or raise.
	return d
}

// --- shared helpers ---------------------------------------------------------

func ident(name string) *lang.Ident {
	if name == "" {
		return nil
	}
	return &lang.Ident{Name: name}
}

func strLit(v string) *lang.StringLit {
	return &lang.StringLit{Value: v}
}

// modelRef reconstructs `model <provider>/<name>` from the raw "provider/name" string. The printer
// emits ModelRef.Raw verbatim, so Raw carries the authored form.
func modelRef(raw string) *lang.ModelRef {
	m := &lang.ModelRef{Raw: raw}
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		m.Provider = raw[:i]
		m.Name = raw[i+1:]
	}
	return m
}

// grant reconstructs a Grant from a "tool.<name>.<operation>" uses string, or nil when the string is
// not a tool grant path (an agent-only reference, malformed, etc.).
func grant(uses string) *lang.Grant {
	parts := strings.Split(uses, ".")
	if len(parts) < 3 || parts[0] != "tool" {
		return nil
	}
	segs := make([]*lang.Ident, len(parts))
	for i, p := range parts {
		if p == "" {
			return nil
		}
		segs[i] = ident(p)
	}
	ops := make([]*lang.Ident, len(parts)-2)
	for i, p := range parts[2:] {
		ops[i] = ident(p)
	}
	return &lang.Grant{Name: ident(parts[1]), Operation: ops, Segments: segs}
}

// constraints raises spec.AgentConstraints, copying only the set fields (mirrors lower.lowerConstraints).
func constraints(c *spec.AgentConstraints) *lang.Constraints {
	out := &lang.Constraints{}
	if c.MaxIterations != 0 {
		v := c.MaxIterations
		out.MaxIterations = &v
	}
	if c.MaxTokens != 0 {
		v := c.MaxTokens
		out.MaxTokens = &v
	}
	if c.TimeoutSeconds != 0 {
		v := c.TimeoutSeconds
		out.TimeoutSeconds = &v
	}
	if c.Temperature != nil {
		v := *c.Temperature
		out.Temperature = &v
	}
	if c.RequireStructuredOutput {
		v := true
		out.RequireStructuredOutput = &v
	}
	return out
}

// typeRefFromSchema reverses lower.SchemaRef ("schemas/<Type>.json" -> "<Type>"), or "" when the ref
// does not follow the convention (so the caller can refuse rather than emit a bad type name).
func typeRefFromSchema(ref string) string {
	if ref == "" {
		return ""
	}
	want := lower.SchemaRef(strings.TrimSuffix(strings.TrimPrefix(ref, "schemas/"), ".json"))
	if want != ref {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(ref, "schemas/"), ".json")
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
