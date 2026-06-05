package cli

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// prepareResolvedConfig loads and resolves the project configuration using the full
// precedence ladder (CLI > environment > project > user-local > defaults).
func prepareResolvedConfig(g *Global) (*config.ResolvedConfig, error) {
	if g == nil {
		return nil, fmt.Errorf("cli: nil globals")
	}
	return config.Resolve(config.ResolveOptions{
		ProjectRoot: g.ProjectRoot,
		Env:         g.Env,
		StatePath:   g.StatePath,
	})
}

// prepareProjectGraph resolves configuration and returns the validated graph and root.
// Prefer [prepareResolvedConfig] when the resolved snapshot or state path is needed.
func prepareProjectGraph(g *Global) (*spec.ProjectGraph, string, error) {
	rc, err := prepareResolvedConfig(g)
	if err != nil {
		return nil, "", err
	}
	return rc.Graph(), rc.ProjectRoot(), nil
}
