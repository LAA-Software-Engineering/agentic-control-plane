package cli

import (
	"path/filepath"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// resolveStateSQLitePath returns an absolute filesystem path for the SQLite deployment store.
// If override is non-empty ([Global.StatePath]), it is resolved relative to projectRoot when not absolute.
// Otherwise [spec.ProjectSpec.State] DSN is used when set, else defaultStateDSN under projectRoot.
func resolveStateSQLitePath(projectRoot string, graph *spec.ProjectGraph, override string) (string, error) {
	projectRoot = filepath.Clean(projectRoot)
	override = strings.TrimSpace(override)
	if override != "" {
		if filepath.IsAbs(override) {
			return filepath.Clean(override), nil
		}
		return filepath.Abs(filepath.Join(projectRoot, filepath.FromSlash(override)))
	}
	dsn := config.DefaultStateDSN
	if graph != nil && graph.Spec.State != nil {
		if s := strings.TrimSpace(graph.Spec.State.DSN); s != "" {
			dsn = s
		}
	}
	if filepath.IsAbs(dsn) {
		return filepath.Clean(dsn), nil
	}
	return filepath.Abs(filepath.Join(projectRoot, filepath.FromSlash(dsn)))
}
