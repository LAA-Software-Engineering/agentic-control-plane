package spec

import (
	"os"
)

// The YAML decode codec (LoadResourceFile / ParseResourceFromBytes) is a restricted
// isolated-compatibility internal under ADR 007, not a project-source ingress: YAML is no longer an
// accepted source language. Its only production caller is `terfyn migrate`'s legacy-YAML reader
// (internal/project.LoadYAMLResources → loadYAMLGraph, and yamlpaths); everything else loads `.agent`
// (internal/project.LoadProject) or builds the typed ResourceGraph directly (config.ResolveGraph). The
// encode side is output-only serialization (project.ExportYAML). Do NOT add callers that treat a YAML
// file as a resource source — that reintroduces the second source language ADR 007 removed.

// LoadResourceFile reads path and decodes exactly one YAML MVP resource. Compat-only: see the codec
// note above — this is `terfyn migrate`'s legacy-YAML reader, not a project-source loader.
func LoadResourceFile(path string) (*Decoded, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &LoadError{Path: path, Msg: "read file", Err: err}
	}
	return ParseResourceFromBytes(data, path)
}
