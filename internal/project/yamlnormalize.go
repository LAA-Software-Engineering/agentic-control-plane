package project

import (
	"bytes"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// NormalizeYAML reformats one or more YAML documents with 2-space indentation by round-tripping
// through yaml.v3 [yaml.Node]. Map key order and document structure are preserved as parsed.
//
// Comments are not reliably preserved; callers should warn users (issue #74).
func NormalizeYAML(src []byte) ([]byte, error) {
	if len(bytes.TrimSpace(src)) == 0 {
		return nil, fmt.Errorf("yaml: empty file")
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for {
		var node yaml.Node
		err := dec.Decode(&node)
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = enc.Close()
			return nil, err
		}
		if err := enc.Encode(&node); err != nil {
			_ = enc.Close()
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := bytes.TrimRight(buf.Bytes(), "\n")
	if len(out) == 0 {
		return []byte{}, nil
	}
	return append(out, '\n'), nil
}
