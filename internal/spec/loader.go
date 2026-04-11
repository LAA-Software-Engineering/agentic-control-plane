package spec

import (
	"os"
)

// LoadResourceFile reads path and decodes exactly one YAML MVP resource.
func LoadResourceFile(path string) (*Decoded, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &LoadError{Path: path, Msg: "read file", Err: err}
	}
	return ParseResourceFromBytes(data, path)
}
