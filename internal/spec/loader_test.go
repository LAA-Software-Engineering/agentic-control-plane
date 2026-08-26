package spec

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadResourceFile_validKinds(t *testing.T) {
	cases := []struct {
		name string
		doc  any
	}{
		{"Project", sampleProject()},
		{"Agent", sampleAgent()},
		{"Tool_MCP", sampleToolMCP()},
		{"Tool_HTTP", sampleToolHTTP()},
		{"Workflow", sampleWorkflow()},
		{"Policy", samplePolicy()},
		{"Environment", sampleEnvironment()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := yaml.Marshal(tc.doc)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			p := filepath.Join(dir, "res.yaml")
			if err := os.WriteFile(p, data, 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := LoadResourceFile(p)
			if err != nil {
				t.Fatalf("LoadResourceFile: %v", err)
			}
			if got.Path != p {
				t.Fatalf("Path = %q, want %q", got.Path, p)
			}
			if !reflect.DeepEqual(stripResourcePos(got.Resource), tc.doc) {
				t.Fatalf("resource mismatch\n got %#v\nwant %#v", got.Resource, tc.doc)
			}
		})
	}
}

func TestParseResourceFromBytes_missingMetadataName(t *testing.T) {
	const y = `
apiVersion: agentic.dev/v0
kind: Agent
metadata: {}
spec: {}
`
	_, err := ParseResourceFromBytes([]byte(y), "/tmp/agent.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("want *LoadError, got %T: %v", err, err)
	}
	if !strings.Contains(le.Error(), "/tmp/agent.yaml") {
		t.Fatalf("error should mention path: %q", le.Error())
	}
	if !strings.Contains(le.Error(), "metadata.name") {
		t.Fatalf("error should mention metadata.name: %q", le.Error())
	}
}

func TestLoadResourceFile_invalidYAML_wrapsPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yaml")
	// Line 2: invalid indentation triggers a parser error with a line hint.
	content := "apiVersion: x\n kind: Agent\nmetadata: {name: a}\nspec: {}\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadResourceFile(p)
	if err == nil {
		t.Fatal("expected error")
	}
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("want *LoadError, got %T: %v", err, err)
	}
	if le.Path != p {
		t.Fatalf("LoadError.Path = %q, want %q", le.Path, p)
	}
	if !strings.Contains(le.Error(), p) {
		t.Fatalf("error string should contain path: %q", le.Error())
	}
	if le.Line != 0 || le.Column != 0 {
		t.Fatalf("syntax errors with no yaml.Node are Path-only, got line=%d col=%d", le.Line, le.Column)
	}
}

func TestParseResourceFromBytes_multipleDocuments(t *testing.T) {
	y := `
apiVersion: agentic.dev/v0
kind: Project
metadata: {name: a}
spec: {}
---
apiVersion: agentic.dev/v0
kind: Project
metadata: {name: b}
spec: {}
`
	_, err := ParseResourceFromBytes([]byte(y), "multi.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrMultipleDocuments) {
		t.Fatalf("want ErrMultipleDocuments in chain, got %v", err)
	}
}

func TestParseResourceFromBytes_unknownKind(t *testing.T) {
	y := `
apiVersion: agentic.dev/v0
kind: NotAKind
metadata: {name: x}
spec: {}
`
	_, err := ParseResourceFromBytes([]byte(y), "x.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("want ErrUnknownKind in chain, got %v", err)
	}
}

func stripResourcePos(res any) any {
	switch r := res.(type) {
	case *ProjectResource:
		r.Pos = Pos{}
	case *AgentResource:
		r.Pos = Pos{}
		r.Spec.ToolsPos = nil
	case *ToolResource:
		r.Pos = Pos{}
		for k, op := range r.Spec.Operations {
			op.Pos = Pos{}
			op.EffectsPos = nil
			r.Spec.Operations[k] = op
		}
	case *WorkflowResource:
		r.Pos = Pos{}
		for i := range r.Spec.Steps {
			r.Spec.Steps[i].Pos = Pos{}
			r.Spec.Steps[i].UsesPos = Pos{}
			r.Spec.Steps[i].AgentPos = Pos{}
			r.Spec.Steps[i].WorkflowPos = Pos{}
			r.Spec.Steps[i].ApprovalPos = Pos{}
		}
	case *PolicyResource:
		r.Pos = Pos{}
		if r.Spec.Approvals != nil {
			r.Spec.Approvals.RequiredForPos = nil
		}
		if r.Spec.Hitl != nil {
			r.Spec.Hitl.InterruptOnPos = nil
		}
		if r.Spec.Effects != nil {
			r.Spec.Effects.PermitPos = nil
			r.Spec.Effects.PermitWithApprovalPos = nil
		}
	case *EnvironmentResource:
		r.Pos = Pos{}
	}
	return res
}
