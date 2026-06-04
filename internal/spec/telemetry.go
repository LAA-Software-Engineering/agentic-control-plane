package spec

import (
	"fmt"
	"strings"
)

// TelemetryEnabled reports whether spec.telemetry.enabled is true on the merged project graph.
func TelemetryEnabled(g *ProjectGraph) bool {
	if g == nil || g.Spec.Telemetry == nil {
		return false
	}
	return g.Spec.Telemetry.Enabled
}

// validateProjectTelemetry returns validation errors for spec.telemetry (issue #108).
func validateProjectTelemetry(g *ProjectGraph) []error {
	if g == nil || g.Spec.Telemetry == nil {
		return nil
	}
	t := g.Spec.Telemetry
	if !t.Enabled {
		return nil
	}
	var errs []error
	prefix := "Project.spec.telemetry"
	if strings.TrimSpace(t.ServiceName) == "" {
		errs = append(errs, fmt.Errorf("%s: serviceName is required when enabled is true", prefix))
	}
	ep := strings.TrimSpace(t.Endpoint)
	if ep == "" && !t.ConsoleExport {
		errs = append(errs, fmt.Errorf("%s: endpoint or consoleExport must be set when enabled is true", prefix))
	}
	if ep != "" && !strings.HasPrefix(ep, "env:") {
		if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
			errs = append(errs, fmt.Errorf("%s: endpoint must be http(s) URL or env:VAR", prefix))
		}
	}
	return errs
}
