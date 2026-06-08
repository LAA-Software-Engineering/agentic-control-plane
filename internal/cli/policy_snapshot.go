package cli

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
)

// persistSnapshots writes resolved-config and compiled policy snapshots for plan→run checks.
func persistSnapshots(rc *config.ResolvedConfig) error {
	if err := config.WriteSnapshot(rc); err != nil {
		return fmt.Errorf("write resolved config snapshot: %w", err)
	}
	if err := writePolicySnapshot(rc); err != nil {
		return err
	}
	return nil
}

func writePolicySnapshot(rc *config.ResolvedConfig) error {
	if rc == nil {
		return fmt.Errorf("policy snapshot: nil resolved config")
	}
	graph := rc.Graph()
	policies, err := policy.CompileReferenced(graph)
	if err != nil {
		return fmt.Errorf("policy snapshot: compile: %w", err)
	}
	if err := policy.WriteSnapshotSet(rc.ProjectRoot(), rc.Digest(), policies); err != nil {
		return fmt.Errorf("policy snapshot: %w", err)
	}
	return nil
}

func assertPolicySnapshotMatches(rc *config.ResolvedConfig) error {
	if rc == nil {
		return fmt.Errorf("policy snapshot: nil resolved config")
	}
	return policy.AssertSnapshotMatchesCompiled(rc.ProjectRoot(), rc.Graph(), rc.Digest())
}
