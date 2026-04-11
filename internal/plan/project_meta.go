package plan

import (
	"fmt"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

// ProjectDeploymentMeta returns the Project resource name and spec_hash for applied_projects.version
// after the same normalization as [Planner.ComputePlan] (issue #15).
func ProjectDeploymentMeta(g *spec.ProjectGraph) (projectName string, projectSpecHash string, err error) {
	if g == nil {
		return "", "", fmt.Errorf("plan: nil project graph")
	}
	rows, err := desiredRows(g)
	if err != nil {
		return "", "", err
	}
	for _, r := range rows {
		if r.id.Kind == spec.KindProject {
			return r.id.Name, r.hash, nil
		}
	}
	return "", "", fmt.Errorf("plan: graph has no Project resource")
}
