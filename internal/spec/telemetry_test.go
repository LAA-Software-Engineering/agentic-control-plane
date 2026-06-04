package spec_test

import (
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestValidateProjectGraph_telemetryDisabledOK(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Telemetry: &spec.ProjectTelemetryConfig{Enabled: false},
		},
	}
	if err := spec.ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProjectGraph_telemetryEnabledRequiresServiceName(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Telemetry: &spec.ProjectTelemetryConfig{
				Enabled:       true,
				ConsoleExport: true,
			},
		},
	}
	err := spec.ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "serviceName") {
		t.Fatalf("want serviceName error, got %v", err)
	}
}

func TestValidateProjectGraph_telemetryEnabledRequiresExporter(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Telemetry: &spec.ProjectTelemetryConfig{
				Enabled:     true,
				ServiceName: "demo",
			},
		},
	}
	err := spec.ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "endpoint or consoleExport") {
		t.Fatalf("want exporter error, got %v", err)
	}
}

func TestValidateProjectGraph_telemetryEndpointEnvOK(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Telemetry: &spec.ProjectTelemetryConfig{
				Enabled:     true,
				ServiceName: "demo",
				Endpoint:    "env:OTEL_EXPORTER_OTLP_ENDPOINT",
			},
		},
	}
	if err := spec.ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateProjectGraph_telemetryEndpointBadScheme(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Telemetry: &spec.ProjectTelemetryConfig{
				Enabled:     true,
				ServiceName: "demo",
				Endpoint:    "ftp://collector",
			},
		},
	}
	err := spec.ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "http(s)") {
		t.Fatalf("want scheme error, got %v", err)
	}
}

func TestTelemetryEnabled(t *testing.T) {
	if spec.TelemetryEnabled(nil) {
		t.Fatal("nil graph should be disabled")
	}
	g := &spec.ProjectGraph{Spec: spec.ProjectSpec{Telemetry: &spec.ProjectTelemetryConfig{Enabled: true}}}
	if !spec.TelemetryEnabled(g) {
		t.Fatal("expected enabled")
	}
}
