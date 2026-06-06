package scaffold

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// appendProjectImport adds importPath to spec.imports when absent.
// Comments on existing nodes are preserved via yaml.v3 Node round-trip.
func appendProjectImport(src []byte, importPath string) ([]byte, bool, error) {
	importPath = normalizeImportPath(importPath)
	if importPath == "" {
		return nil, false, fmt.Errorf("scaffold: empty import path")
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, false, fmt.Errorf("scaffold: parse project.yaml: %w", err)
	}
	root := docContent(&doc)
	if root == nil {
		return nil, false, fmt.Errorf("scaffold: project.yaml: empty document")
	}

	specNode := mappingValue(root, "spec")
	if specNode == nil {
		return nil, false, fmt.Errorf("scaffold: project.yaml: missing spec")
	}
	importsNode := mappingValue(specNode, "imports")
	if importsNode == nil || importsNode.Kind != yaml.SequenceNode {
		return nil, false, fmt.Errorf("scaffold: project.yaml: missing spec.imports sequence")
	}

	for _, child := range importsNode.Content {
		if child == nil {
			continue
		}
		if normalizeImportPath(child.Value) == importPath {
			return src, false, nil
		}
	}

	newItem := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: importPath}
	importsNode.Content = append(importsNode.Content, newItem)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		_ = enc.Close()
		return nil, false, fmt.Errorf("scaffold: encode project.yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, false, err
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	if len(out) > 0 {
		out = append(out, '\n')
	}
	return out, true, nil
}

func normalizeImportPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "./") && !strings.HasPrefix(p, "/") {
		p = "./" + p
	}
	return p
}

func docContent(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	return doc
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k != nil && k.Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
