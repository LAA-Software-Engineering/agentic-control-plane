package telemetry_test

import (
	"os"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/telemetry"
)

func TestResolveEndpoint_env(t *testing.T) {
	t.Setenv("ACP_OTEL_TEST_EP", "http://127.0.0.1:4318")
	got, err := telemetry.ResolveEndpoint("env:ACP_OTEL_TEST_EP")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://127.0.0.1:4318" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveEndpoint_envMissing(t *testing.T) {
	_, err := telemetry.ResolveEndpoint("env:ACP_OTEL_TEST_MISSING_XYZ")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveEndpoint_literalURL(t *testing.T) {
	got, err := telemetry.ResolveEndpoint("http://collector:4318/v1/traces")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://collector:4318/v1/traces" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveEndpoint_empty(t *testing.T) {
	got, err := telemetry.ResolveEndpoint("")
	if err != nil || got != "" {
		t.Fatalf("got %q err %v", got, err)
	}
	_ = os.Getenv
}
