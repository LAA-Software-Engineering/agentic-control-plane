package spec

import "gopkg.in/yaml.v3"

// MarshalYAML preserves the DAG-mode signal across YAML interchange (issue #207, ADR 003). Needs is
// yaml:"omitempty" and NeedsDeclared is not a YAML field, so a step whose only graph-mode signal is
// an empty declared `needs:` (a parallel root, or an .agent `parallel { }` root the lowerer sets
// NeedsDeclared on with empty Needs) would export without a needs key and reload as implicit
// sequential — silently switching concurrent roots to a chain. When needs is declared but empty,
// emit an explicit `needs: []` so terfyn export → load round-trips to the same graph mode
// ([WorkflowUsesExplicitNeeds]). A non-empty or undeclared needs marshals exactly as the default
// encoder would (this only ever adds the empty sequence). Mirrors [ToolSpec.MarshalYAML].
func (s WorkflowStep) MarshalYAML() (any, error) {
	// alias drops the MarshalYAML method so node.Encode does not recurse.
	type alias WorkflowStep
	var node yaml.Node
	if err := node.Encode(alias(s)); err != nil {
		return nil, err
	}
	if s.NeedsDeclared && len(s.Needs) == 0 {
		injectEmptySequence(&node, "needs")
	}
	return &node, nil
}
