package project

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/spec"
)

func TestResolveReferences_missingAgent(t *testing.T) {
	root := filepath.Join("testdata", "refs_missing_agent")
	g, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	err = ResolveReferences(g)
	var mr *MissingRefError
	if !errors.As(err, &mr) {
		t.Fatalf("want *MissingRefError, got %T: %v", err, err)
	}
	if mr.Referrer != (spec.ResourceID{Kind: spec.KindWorkflow, Name: "badwf"}) {
		t.Fatalf("Referrer = %v", mr.Referrer)
	}
	if mr.Missing != (spec.ResourceID{Kind: spec.KindAgent, Name: "ghost"}) {
		t.Fatalf("Missing = %v", mr.Missing)
	}
}

func TestResolveReferences_unknownTool(t *testing.T) {
	root := filepath.Join("testdata", "refs_unknown_tool")
	g, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	err = ResolveReferences(g)
	var mr *MissingRefError
	if !errors.As(err, &mr) {
		t.Fatalf("want *MissingRefError, got %T: %v", err, err)
	}
	if mr.Referrer != (spec.ResourceID{Kind: spec.KindWorkflow, Name: "uses-unknown"}) {
		t.Fatalf("Referrer = %v", mr.Referrer)
	}
	if mr.Missing != (spec.ResourceID{Kind: spec.KindTool, Name: "nope"}) {
		t.Fatalf("Missing = %v", mr.Missing)
	}
}

func TestResolveReferences_forwardRefRejected(t *testing.T) {
	root := filepath.Join("testdata", "refs_forward_bad")
	g, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	err = ResolveReferences(g)
	if err == nil {
		t.Fatal("expected forward reference error")
	}
	if !strings.Contains(err.Error(), "forward reference") {
		t.Fatalf("expected forward reference in error: %v", err)
	}
}

func TestResolveReferences_validInterpolationOrder(t *testing.T) {
	root := filepath.Join("testdata", "refs_forward_ok")
	g, err := LoadProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResolveReferences(g); err != nil {
		t.Fatal(err)
	}
}
