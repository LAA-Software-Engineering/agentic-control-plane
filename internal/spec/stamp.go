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
	case *WorkflowResource:
		r.Pos = p
		stampWorkflowSteps(file, yamlMapValue(m, "spec"), &r.Spec)
	case *PolicyResource:
		r.Pos = p
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
	}
}
