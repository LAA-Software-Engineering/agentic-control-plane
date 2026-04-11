package spec

import "strings"

// projectDefaults holds trimmed Project.spec.defaults values (design doc §7.1).
// Runtime is included for API symmetry with the YAML schema; see NormalizeProjectGraph
// for which fields are applied to MVP resource specs.
type projectDefaults struct {
	Runtime string
	Model   string
	Policy  string
}

// readProjectDefaults returns trimmed defaults from the merged project graph.
// Nil or missing ProjectSpec.Defaults yields zero values (no defaults applied).
func readProjectDefaults(g *ProjectGraph) projectDefaults {
	if g == nil || g.Spec.Defaults == nil {
		return projectDefaults{}
	}
	d := g.Spec.Defaults
	return projectDefaults{
		Runtime: strings.TrimSpace(d.Runtime),
		Model:   strings.TrimSpace(d.Model),
		Policy:  strings.TrimSpace(d.Policy),
	}
}
