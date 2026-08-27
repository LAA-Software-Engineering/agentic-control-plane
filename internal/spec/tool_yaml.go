package spec

import "gopkg.in/yaml.v3"

// MarshalYAML preserves the closed-empty capability manifest across YAML interchange (issue #204,
// ADR 003). Operations is yaml:"omitempty", so an empty operations map is dropped by the encoder,
// and OperationsDeclared is not a YAML field — so a plain marshal of a locked tool would emit no
// operations key and a reload would reopen the callable set. When the manifest is declared but
// empty, emit an explicit operations: {} so terfyn export → load round-trips to the same closed
// world that plan/apply identity and CheckToolCall enforce. A non-empty or undeclared manifest
// marshals exactly as the default encoder would (this only ever adds the empty mapping).
func (s ToolSpec) MarshalYAML() (any, error) {
	// alias drops the MarshalYAML method so node.Encode does not recurse.
	type alias ToolSpec
	var node yaml.Node
	if err := node.Encode(alias(s)); err != nil {
		return nil, err
	}
	if s.OperationsDeclared && len(s.Operations) == 0 {
		injectEmptyMapping(&node, "operations")
	}
	return &node, nil
}

// injectEmptyMapping appends key: {} to a mapping node when key is not already present.
func injectEmptyMapping(node *yaml.Node, key string) {
	injectEmptyContainer(node, key, yaml.MappingNode, "!!map")
}

// injectEmptySequence appends key: [] to a mapping node when key is not already present.
func injectEmptySequence(node *yaml.Node, key string) {
	injectEmptyContainer(node, key, yaml.SequenceNode, "!!seq")
}

func injectEmptyContainer(node *yaml.Node, key string, kind yaml.Kind, tag string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return
		}
	}
	node.Content = append(node.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: kind, Tag: tag, Style: yaml.FlowStyle},
	)
}
