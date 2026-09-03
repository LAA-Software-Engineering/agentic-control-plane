package cli

import (
	"fmt"
	"os"

	"github.com/Terfyn/terfyn/internal/config"
	"github.com/Terfyn/terfyn/internal/effects"
	"github.com/Terfyn/terfyn/internal/spec"
)

// prepareResolvedConfig loads and resolves the project configuration using the full
// precedence ladder (CLI > environment > project > user-local > defaults). Every project-loading
// command routes through here (directly or via [prepareProjectGraph]), so it is the single place the
// YAML-source deprecation notice (issue #430 Phase 2) is emitted — once per invocation, to stderr, so
// it never corrupts `-o json` output on stdout.
func prepareResolvedConfig(g *Global) (*config.ResolvedConfig, error) {
	if g == nil {
		return nil, fmt.Errorf("cli: nil globals")
	}
	rc, err := config.Resolve(config.ResolveOptions{
		ProjectRoot: g.ProjectRoot,
		Env:         g.Env,
		StatePath:   g.StatePath,
	})
	if err != nil {
		return nil, err
	}
	if w := rc.SourceDeprecation(); w != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	return rc, nil
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

// checkEffectBounds runs [effects.Check] after a successful graph validate.
// Issue #190 is validate/plan only; apply/run/test/inspect must still load the graph
// so runtime CheckToolCall can deny at exit 5.
func checkEffectBounds(graph *spec.ProjectGraph) error {
	if err := effects.Check(graph); err != nil {
		return NewExitError(ExitValidationError, err)
	}
	return nil
}
