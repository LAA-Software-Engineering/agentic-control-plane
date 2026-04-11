package project

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"

// ResolveReferences checks symbolic references and workflow step rules (§9.1, §9.4).
func ResolveReferences(g *spec.ProjectGraph) error {
	return spec.ResolveReferences(g)
}
