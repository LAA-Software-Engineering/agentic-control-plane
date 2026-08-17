package spec

import "fmt"

// Pos is diagnostic source location on IR nodes (issue #187, ADR 003).
// It is metadata only — never identity. Fields on resources and steps use
// `json:"-" yaml:"-"` so Pos is stripped from canonicalResourceJSON, SpecHash,
// WorkflowSpecHash, and ResolvedGraphDigest. Zero value means unknown
// (machine-constructed resources).
type Pos struct {
	File   string `json:"file,omitempty" yaml:"file,omitempty"`
	Line   int    `json:"line,omitempty" yaml:"line,omitempty"`
	Column int    `json:"column,omitempty" yaml:"column,omitempty"`
}

// IsZero reports whether p has no line or column (file-only is still unknown for underlining).
func (p Pos) IsZero() bool {
	return p.Line <= 0 && p.Column <= 0
}

// String formats file:line:col for diagnostics. Empty if nothing useful is set.
func (p Pos) String() string {
	switch {
	case p.File != "" && p.Line > 0 && p.Column > 0:
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Column)
	case p.File != "" && p.Line > 0:
		return fmt.Sprintf("%s:%d", p.File, p.Line)
	case p.File != "":
		return p.File
	default:
		return ""
	}
}

// Errorf prefixes format with p when p has a location; otherwise it is a plain formatted error.
func (p Pos) Errorf(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if loc := p.String(); loc != "" {
		return fmt.Errorf("%s: %w", loc, err)
	}
	return err
}

// RelocateFile rewrites File on every Pos attached to res (project-relative paths).
func RelocateFile(res any, file string) {
	switch r := res.(type) {
	case *ProjectResource:
		relocatePos(&r.Pos, file)
	case *AgentResource:
		relocatePos(&r.Pos, file)
		for i := range r.Spec.ToolsPos {
			relocatePos(&r.Spec.ToolsPos[i], file)
		}
	case *ToolResource:
		relocatePos(&r.Pos, file)
		for k, op := range r.Spec.Operations {
			relocatePos(&op.Pos, file)
			for i := range op.EffectsPos {
				relocatePos(&op.EffectsPos[i], file)
			}
			r.Spec.Operations[k] = op
		}
	case *WorkflowResource:
		relocatePos(&r.Pos, file)
		for i := range r.Spec.Steps {
			relocatePos(&r.Spec.Steps[i].Pos, file)
			relocatePos(&r.Spec.Steps[i].UsesPos, file)
			relocatePos(&r.Spec.Steps[i].AgentPos, file)
		}
	case *PolicyResource:
		relocatePos(&r.Pos, file)
		if r.Spec.Approvals != nil {
			for i := range r.Spec.Approvals.RequiredForPos {
				relocatePos(&r.Spec.Approvals.RequiredForPos[i], file)
			}
		}
		if r.Spec.Hitl != nil {
			for k, p := range r.Spec.Hitl.InterruptOnPos {
				relocatePos(&p, file)
				r.Spec.Hitl.InterruptOnPos[k] = p
			}
		}
	case *EnvironmentResource:
		relocatePos(&r.Pos, file)
	}
}

func relocatePos(p *Pos, file string) {
	if p == nil || p.IsZero() {
		return
	}
	p.File = file
}
