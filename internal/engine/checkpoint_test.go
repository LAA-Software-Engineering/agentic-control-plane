package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LAA-Software-Engineering/terfyn/internal/spec"
)

func TestMarshalCheckpointPayload_stableKeyOrder(t *testing.T) {
	ictx := Context{
		Input: map[string]any{"b": 2, "a": 1},
		Steps: map[string]StepResult{
			"fetch": {Output: map[string]any{"x": 1}, Meta: map[string]any{"costUsd": 0.1}},
		},
	}
	s1, err := marshalCheckpointPayload(ictx, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := marshalCheckpointPayload(ictx, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatalf("non-deterministic: %q vs %q", s1, s2)
	}
	if !json.Valid([]byte(s1)) {
		t.Fatalf("invalid json: %s", s1)
	}

	wf := demoWorkflowGraph(t).Workflows["demo"]
	gotCtx, cost, err := unmarshalCheckpointPayload(s1, demoWorkflowGraph(t), wf, 0)
	if err != nil {
		t.Fatal(err)
	}
	if cost != 0.5 {
		t.Fatalf("cost = %v", cost)
	}
	b, _ := json.Marshal(gotCtx.Input)
	if string(b) != `{"a":1,"b":2}` && string(b) != `{"b":2,"a":1}` {
		t.Fatalf("input round-trip %s", b)
	}
	if len(gotCtx.Steps) != 1 {
		t.Fatalf("steps = %d", len(gotCtx.Steps))
	}
}

func TestUnmarshalCheckpointPayload_malformed(t *testing.T) {
	wf := demoWorkflowGraph(t).Workflows["demo"]
	_, _, err := unmarshalCheckpointPayload(`not-json`, demoWorkflowGraph(t), wf, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUnmarshalCheckpointPayload_nestedUnknownInnerStep(t *testing.T) {
	g := subworkflowGraph()
	raw, err := marshalCheckpointPayload(Context{
		Input: map[string]any{},
		Steps: map[string]StepResult{},
		Nested: &NestedRunState{
			StepID:    "call",
			Workflow:  "child",
			Steps:     map[string]StepResult{"nope": {Output: map[string]any{"x": 1}}},
			Completed: []string{"nope"},
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = unmarshalCheckpointPayload(raw, g, g.Workflows["parent"], -1)
	if err == nil {
		t.Fatal("expected nested inner id error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnmarshalCheckpointPayload_nestedUnknownCallee(t *testing.T) {
	g := &spec.ProjectGraph{
		Workflows: map[string]*spec.WorkflowResource{
			"parent": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{{ID: "call", Workflow: "ghost"}},
				},
			},
		},
	}
	raw, err := marshalCheckpointPayload(Context{
		Input: map[string]any{},
		Steps: map[string]StepResult{},
		Nested: &NestedRunState{
			StepID:    "call",
			Workflow:  "ghost",
			Completed: []string{"inner"},
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = unmarshalCheckpointPayload(raw, g, g.Workflows["parent"], -1)
	if err == nil {
		t.Fatal("expected unknown nested workflow error")
	}
	if !strings.Contains(err.Error(), "unknown workflow") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnmarshalCheckpointPayload_nestedParentStepNotWorkflowCall(t *testing.T) {
	g := subworkflowGraph()
	raw, err := marshalCheckpointPayload(Context{
		Input: map[string]any{},
		Steps: map[string]StepResult{},
		Nested: &NestedRunState{
			StepID:    "after",
			Workflow:  "child",
			Completed: []string{"echo"},
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = unmarshalCheckpointPayload(raw, g, g.Workflows["parent"], -1)
	if err == nil {
		t.Fatal("expected parent step mismatch error")
	}
	if !strings.Contains(err.Error(), "is not a workflow:") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnmarshalCheckpointPayload_nestedCalleeNameMismatch(t *testing.T) {
	g := subworkflowGraph()
	raw, err := marshalCheckpointPayload(Context{
		Input: map[string]any{},
		Steps: map[string]StepResult{},
		Nested: &NestedRunState{
			StepID:    "call",
			Workflow:  "ghost",
			Completed: []string{"echo"},
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = unmarshalCheckpointPayload(raw, g, g.Workflows["parent"], -1)
	if err == nil {
		t.Fatal("expected callee name mismatch")
	}
	if !strings.Contains(err.Error(), "calls") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnmarshalCheckpointPayload_nestedDepthExceedsResolvedCap(t *testing.T) {
	g := &spec.ProjectGraph{
		Workflows: map[string]*spec.WorkflowResource{
			"leaf": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "leaf"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{{ID: "done", Uses: "tool.helper.echo"}},
				},
			},
			"child": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "child"},
				Spec: spec.WorkflowSpec{
					Steps: []spec.WorkflowStep{{ID: "inner", Workflow: "leaf"}},
				},
			},
			"parent": {
				APIVersion: spec.APIVersionV0,
				Kind:       spec.KindWorkflow,
				Metadata:   spec.Metadata{Name: "parent"},
				Spec: spec.WorkflowSpec{
					Limits: &spec.ExecutionLimits{MaxWorkflowNesting: 1},
					Steps:  []spec.WorkflowStep{{ID: "call", Workflow: "child"}},
				},
			},
		},
	}
	raw, err := marshalCheckpointPayload(Context{
		Input: map[string]any{},
		Steps: map[string]StepResult{},
		Nested: &NestedRunState{
			StepID:    "call",
			Workflow:  "child",
			Completed: []string{"inner"},
			Nested: &NestedRunState{
				StepID:    "inner",
				Workflow:  "leaf",
				Completed: []string{"done"},
			},
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = unmarshalCheckpointPayload(raw, g, g.Workflows["parent"], -1)
	if err == nil {
		t.Fatal("expected nested depth error")
	}
	if !strings.Contains(err.Error(), "maxWorkflowNesting 1") {
		t.Fatalf("error = %v", err)
	}
}

func TestUnmarshalCheckpointPayload_nestedValidInnerStep(t *testing.T) {
	g := subworkflowGraph()
	raw, err := marshalCheckpointPayload(Context{
		Input: map[string]any{},
		Steps: map[string]StepResult{"call": {Output: map[string]any{}}},
		Nested: &NestedRunState{
			StepID:    "call",
			Workflow:  "child",
			Steps:     map[string]StepResult{"echo": {Output: map[string]any{"msg": "hi"}}},
			Completed: []string{"echo"},
		},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := unmarshalCheckpointPayload(raw, g, g.Workflows["parent"], -1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Nested == nil || got.Nested.Workflow != "child" {
		t.Fatalf("nested = %+v", got.Nested)
	}
	if _, ok := got.Nested.Steps["echo"]; !ok {
		t.Fatalf("inner echo missing: %+v", got.Nested.Steps)
	}
}
