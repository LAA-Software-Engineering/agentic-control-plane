package spec

import (
	"gopkg.in/yaml.v3"
)

// stampResourcePositions records yaml.Node Line/Column onto IR Pos fields.
func stampResourcePositions(file string, data []byte, res any) {
	if res == nil || len(data) == 0 {
		return
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return
	}
	m := unwrapYAMLDoc(&root)
	p := posFromNode(file, m)
	switch r := res.(type) {
	case *ProjectResource:
		r.Pos = p
	case *AgentResource:
		r.Pos = p
		stampAgentTools(file, yamlMapValue(m, "spec"), &r.Spec)
	case *ToolResource:
		r.Pos = p
		stampToolOperations(file, yamlMapValue(m, "spec"), &r.Spec)
	case *WorkflowResource:
		r.Pos = p
		stampWorkflowSteps(file, yamlMapValue(m, "spec"), &r.Spec)
	case *PolicyResource:
		r.Pos = p
		stampPolicySpec(file, yamlMapValue(m, "spec"), &r.Spec)
	case *EnvironmentResource:
		r.Pos = p
	}
}

func unwrapYAMLDoc(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

func yamlMapValue(n *yaml.Node, key string) *yaml.Node {
	n = unwrapYAMLDoc(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i] != nil && n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func posFromNode(file string, n *yaml.Node) Pos {
	if n == nil {
		return Pos{}
	}
	return Pos{File: file, Line: n.Line, Column: n.Column}
}

func stampAgentTools(file string, specNode *yaml.Node, s *AgentSpec) {
	if s == nil {
		return
	}
	tools := yamlMapValue(specNode, "tools")
	if tools == nil || tools.Kind != yaml.SequenceNode {
		return
	}
	s.ToolsPos = make([]Pos, len(s.Tools))
	for i, item := range tools.Content {
		if i >= len(s.ToolsPos) {
			break
		}
		s.ToolsPos[i] = posFromNode(file, item)
	}
}

func stampToolOperations(file string, specNode *yaml.Node, s *ToolSpec) {
	if s == nil || len(s.Operations) == 0 {
		return
	}
	ops := yamlMapValue(specNode, "operations")
	if ops == nil || ops.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(ops.Content); i += 2 {
		key := ops.Content[i]
		val := ops.Content[i+1]
		if key == nil {
			continue
		}
		op, ok := s.Operations[key.Value]
		if !ok {
			continue
		}
		op.Pos = posFromNode(file, key)
		effects := yamlMapValue(val, "effects")
		if effects != nil && effects.Kind == yaml.SequenceNode {
			op.EffectsPos = make([]Pos, len(op.Effects))
			for j, item := range effects.Content {
				if j >= len(op.EffectsPos) {
					break
				}
				op.EffectsPos[j] = posFromNode(file, item)
			}
		}
		s.Operations[key.Value] = op
	}
}

func stampWorkflowSteps(file string, specNode *yaml.Node, w *WorkflowSpec) {
	if w == nil {
		return
	}
	steps := yamlMapValue(specNode, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return
	}
	for i, item := range steps.Content {
		if i >= len(w.Steps) {
			break
		}
		w.Steps[i].Pos = posFromNode(file, item)
		if v := yamlMapValue(item, "agent"); v != nil {
			w.Steps[i].AgentPos = posFromNode(file, v)
		}
		if v := yamlMapValue(item, "uses"); v != nil {
			w.Steps[i].UsesPos = posFromNode(file, v)
		}
		if needs := yamlMapValue(item, "needs"); needs != nil {
			w.Steps[i].NeedsDeclared = true
			if needs.Kind == yaml.SequenceNode {
				w.Steps[i].NeedsPos = make([]Pos, len(needs.Content))
				for j, nitem := range needs.Content {
					if j >= len(w.Steps[i].NeedsPos) {
						break
					}
					w.Steps[i].NeedsPos[j] = posFromNode(file, nitem)
				}
			}
		}
	}
}

func stampPolicySpec(file string, specNode *yaml.Node, s *PolicySpec) {
	if s == nil {
		return
	}
	if s.Approvals != nil {
		req := yamlMapValue(yamlMapValue(specNode, "approvals"), "requiredFor")
		if req != nil && req.Kind == yaml.SequenceNode {
			s.Approvals.RequiredForPos = make([]Pos, len(s.Approvals.RequiredFor))
			for i, item := range req.Content {
				if i >= len(s.Approvals.RequiredForPos) {
					break
				}
				s.Approvals.RequiredForPos[i] = posFromNode(file, item)
			}
		}
	}
	if s.Hitl != nil {
		on := yamlMapValue(yamlMapValue(specNode, "hitl"), "interruptOn")
		if on != nil && on.Kind == yaml.MappingNode {
			s.Hitl.InterruptOnPos = make(map[string]Pos, len(on.Content)/2)
			for i := 0; i+1 < len(on.Content); i += 2 {
				key := on.Content[i]
				if key == nil {
					continue
				}
				s.Hitl.InterruptOnPos[key.Value] = posFromNode(file, key)
			}
		}
	}
	if s.Effects != nil {
		stampPolicyEffectList(file, yamlMapValue(yamlMapValue(specNode, "effects"), "permit"), s.Effects.Permit, &s.Effects.PermitPos)
		stampPolicyEffectList(file, yamlMapValue(yamlMapValue(specNode, "effects"), "permitWithApproval"), s.Effects.PermitWithApproval, &s.Effects.PermitWithApprovalPos)
	}
}

func stampPolicyEffectList(file string, seq *yaml.Node, idents []string, pos *[]Pos) {
	if seq == nil || seq.Kind != yaml.SequenceNode || len(idents) == 0 {
		return
	}
	out := make([]Pos, len(idents))
	for i, item := range seq.Content {
		if i >= len(out) {
			break
		}
		out[i] = posFromNode(file, item)
	}
	*pos = out
}
