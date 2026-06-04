package telemetry

import (
	"fmt"
	"os"
	"strings"
)

// ResolveEndpoint expands endpoint values from project YAML, e.g. "env:OTEL_EXPORTER_OTLP_ENDPOINT".
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
