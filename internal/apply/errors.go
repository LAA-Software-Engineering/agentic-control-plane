package apply

import "errors"

// ErrDeploymentStateChanged means SQLite deployment state changed after the plan was computed.
// Scripts should re-run plan, then apply. Surfaces as CLI exit code 3 (issue #78).
var ErrDeploymentStateChanged = errors.New("deployment state changed since plan was computed; re-run plan")
