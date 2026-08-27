package check

import (
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/effects"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/lang/lower"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/project"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// Options supplies context Check cannot derive from f alone.
type Options struct {
	// Project supplies already-loaded sibling YAML resources (agents, tools,
	// policies, other workflows) so a .agent file can reference and interoperate
	// with them. Nil is treated as an empty graph. Check never mutates Project —
	// it computes against a shallow clone.
	Project *spec.ProjectGraph

	// Files supplies other .agent ASTs in the same compilation unit, so a
	// cross-file agent/workflow reference resolves to the right step kind. This
	// is the project-wide symbol table internal/lang/lower/lower.go's
	// Options.Workflows doc comment names as this package's replacement.
	Files []*lang.File

	// SchemaDir overrides where TypeRef names resolve to schema files
	// (<SchemaDir>/schemas/<Name>.json). Defaults to the directory of f's own
	// position (f.Pos.File) when empty. See docs/plans/198-type-effect-checking.md
	// design decision 4.
	SchemaDir string
}

// Program is the checked program: f plus the resolved graph and effect bound
// it type/effect-checks against. A non-nil Program is always returned, even
// when Check reports errors, so a caller can inspect partial results —
// Diagnostics (via Diagnostics.HasErrors) is the authority on pass/fail.
type Program struct {
	File   *lang.File
	Graph  *spec.ProjectGraph
	Bounds effects.GraphBounds
}

// Check resolves, type-checks, and effect-checks f.
//
// The effect bound is computed by lowering f through lower.LowerFile into a
// *spec.ProjectGraph (merged with Options.Project) and calling
// effects.Compute on the result — see doc.go for why this package never walks
// the AST for effects on its own.
func Check(f *lang.File, opts Options) (*Program, lang.Diagnostics) {
	prog := &Program{File: f}
	if f == nil {
		return prog, nil
	}

	var diags lang.Diagnostics

	workflowNames := collectWorkflowNames(f, opts)
	result, lowerDiags := lower.LowerFile(f, lower.Options{Workflows: workflowNames})
	diags = append(diags, lowerDiags...)

	graph := cloneGraph(opts.Project)
	if err := project.MergeLowered(graph, result); err != nil {
		diags = append(diags, lang.Diagnostic{Pos: f.Pos, Msg: err.Error()})
	}
	prog.Graph = graph
	prog.Bounds = effects.Compute(graph)

	tu := resolveTypes(f, opts)
	diags = append(diags, checkTypes(f, tu)...)
	diags = append(diags, checkEffectsClauses(f, prog.Bounds)...)

	return prog, diags.Sorted()
}

// collectWorkflowNames gathers workflow names declared OUTSIDE f — in
// Options.Files or the already-loaded YAML project — so a single-identifier
// callee in f classifies as a workflow: step rather than defaulting to
// agent:. Names f declares itself are detected by lower.LowerFile directly and
// need not be listed here.
func collectWorkflowNames(f *lang.File, opts Options) map[string]bool {
	out := map[string]bool{}
	for _, other := range opts.Files {
		if other == nil || other == f {
			continue
		}
		for _, d := range other.Decls {
			if wd, ok := d.(*lang.WorkflowDecl); ok {
				if name := identName(wd.Name); name != "" {
					out[name] = true
				}
			}
		}
	}
	if opts.Project != nil {
		for name := range opts.Project.Workflows {
			out[name] = true
		}
	}
	return out
}

// cloneGraph returns a shallow copy of g (or an empty graph for nil) so
// project.MergeLowered's in-place mutation never surprises a caller holding g.
// Resource pointers are shared; only the top-level maps are copied.
func cloneGraph(g *spec.ProjectGraph) *spec.ProjectGraph {
	out := &spec.ProjectGraph{
		Agents:       map[string]*spec.AgentResource{},
		Tools:        map[string]*spec.ToolResource{},
		Workflows:    map[string]*spec.WorkflowResource{},
		Policies:     map[string]*spec.PolicyResource{},
		Environments: map[string]*spec.EnvironmentResource{},
	}
	if g == nil {
		return out
	}
	out.Meta = g.Meta
	out.Pos = g.Pos
	out.Spec = g.Spec
	for k, v := range g.Agents {
		out.Agents[k] = v
	}
	for k, v := range g.Tools {
		out.Tools[k] = v
	}
	for k, v := range g.Workflows {
		out.Workflows[k] = v
	}
	for k, v := range g.Policies {
		out.Policies[k] = v
	}
	for k, v := range g.Environments {
		out.Environments[k] = v
	}
	return out
}

func identName(id *lang.Ident) string {
	if id == nil {
		return ""
	}
	return id.Name
}
