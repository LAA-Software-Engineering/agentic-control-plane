package telemetry

import (
	"fmt"
	"os"
	"strings"
)

// ResolveEndpoint expands endpoint values from project YAML, e.g. "env:OTEL_EXPORTER_OTLP_ENDPOINT".
//
// A missing or empty environment variable returns an error. [NewTracer] catches that error,
// logs a warning, and returns a disabled tracer so workflow runs are not affected.
func ResolveEndpoint(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", nil
	}
	if strings.HasPrefix(spec, "env:") {
		name := strings.TrimSpace(strings.TrimPrefix(spec, "env:"))
		if name == "" {
			return "", fmt.Errorf("telemetry: endpoint env: requires a variable name")
		}
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			return "", fmt.Errorf("telemetry: environment variable %q is not set", name)
		}
		return v, nil
	}
	return spec, nil
}
