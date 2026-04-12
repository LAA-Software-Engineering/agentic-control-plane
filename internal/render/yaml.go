package render

import (
	"io"

	"gopkg.in/yaml.v3"
)

// WriteYAML encodes v as YAML (design doc section 11.1).
func WriteYAML(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	defer enc.Close()
	return enc.Encode(v)
}
