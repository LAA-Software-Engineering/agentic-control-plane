package spec

// TraceRetentionDays returns spec.traces.retentionDays from the merged project graph, or 0 when
// unset or non-positive (no pruning; issue #75). Tracing config is project-global (not overridden
// per Environment in MVP).
func TraceRetentionDays(g *ProjectGraph) int {
	if g == nil || g.Spec.Traces == nil {
		return 0
	}
	d := g.Spec.Traces.RetentionDays
	if d <= 0 {
		return 0
	}
	return d
}
