package sqlite

import "github.com/LAA-Software-Engineering/agentic-control-plane/internal/state"

// Compile-time check: *Store implements state facades (issue #11).
var (
	_ state.DeploymentStore         = (*Store)(nil)
	_ state.TransactionalDeployment = (*Store)(nil)
	_ state.RuntimeStore            = (*Store)(nil)
)
