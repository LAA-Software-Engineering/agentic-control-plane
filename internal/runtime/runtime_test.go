package runtime

import (
	"path/filepath"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/config"
	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestWorkflowRuntimeName_defaultsToLocal(t *testing.T) {
	if got := WorkflowRuntimeName(nil, "demo"); got != NameLocal {
		t.Fatalf("got %q", got)
	}
}

func TestWorkflowRuntimeName_workflowOverride(t *testing.T) {
	g := &spec.ProjectGraph{
		Spec: spec.ProjectSpec{
			Defaults: &spec.ProjectDefaults{Runtime: NameLocal},
		},
		Workflows: map[string]*spec.WorkflowResource{
			"w": {Spec: spec.WorkflowSpec{Runtime: NameLocal}},
		},
	}
	if got := WorkflowRuntimeName(g, "w"); got != NameLocal {
		t.Fatalf("got %q", got)
	}
}

func TestHealthStatus_values(t *testing.T) {
	if HealthOK != "ok" || HealthDegraded != "degraded" || HealthError != "error" {
		t.Fatalf("unexpected health state constants")
	}
}

func TestRunResult_zeroValue(t *testing.T) {
	var r RunResult
	if r.RunID != "" {
		t.Fatal("expected empty run id")
	}
}

func TestInvokeOptions_fields(t *testing.T) {
	opts := InvokeOptions{
		RunID: "r1", WorkflowName: "demo", Env: "prod",
		InputJSON: []byte(`{"k":"v"}`),
	}
	if opts.WorkflowName != "demo" {
		t.Fatalf("opts %+v", opts)
	}
	_ = config.ResolveOptions{ProjectRoot: filepath.Join("..", "..", "examples")}
}
