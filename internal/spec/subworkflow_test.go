package spec

import (
	"strings"
	"testing"
)

// subStep returns a `workflow:` step invoking callee, optionally carrying a diagnostic position.
func subStep(id, callee string, pos Pos) WorkflowStep {
	return WorkflowStep{ID: id, Workflow: callee, WorkflowPos: pos}
}

func wfWithSteps(name string, steps ...WorkflowStep) *WorkflowResource {
	return &WorkflowResource{
		Kind:     KindWorkflow,
		Metadata: Metadata{Name: name},
		Spec:     WorkflowSpec{Steps: steps},
	}
}

func subGraph(wfs ...*WorkflowResource) *ProjectGraph {
	m := make(map[string]*WorkflowResource, len(wfs))
	for _, wf := range wfs {
		m[wf.Metadata.Name] = wf
	}
	return &ProjectGraph{Workflows: m}
}

func TestSubworkflow_directRecursion_failsWithPosition(t *testing.T) {
	g := subGraph(
		wfWithSteps("a", subStep("s", "a", Pos{File: "a.yaml", Line: 12, Column: 7})),
	)
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected recursion error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "subworkflow recursion via \"a\"") {
		t.Fatalf("message = %q", msg)
	}
	if !strings.Contains(msg, "a.yaml:12:7") {
		t.Fatalf("expected position in %q", msg)
	}
	if !strings.Contains(msg, "a -> a") {
		t.Fatalf("expected cycle path in %q", msg)
	}
}

func TestSubworkflow_mutualRecursion_failsWithPosition(t *testing.T) {
	g := subGraph(
		wfWithSteps("a", subStep("sa", "b", Pos{File: "a.yaml", Line: 3})),
		wfWithSteps("b", subStep("sb", "a", Pos{File: "b.yaml", Line: 5})),
	)
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected mutual recursion error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "subworkflow recursion") {
		t.Fatalf("message = %q", msg)
	}
	// One of the two closing edges anchors the error; both carry positions.
	if !strings.Contains(msg, "a.yaml:3") && !strings.Contains(msg, "b.yaml:5") {
		t.Fatalf("expected a position in %q", msg)
	}
}

func TestSubworkflow_depthLimitExceeded_fails(t *testing.T) {
	// Build a linear chain w0 -> w1 -> ... -> wN with N = limit+1 edges (depth limit+1).
	n := DefaultMaxSubworkflowDepth + 1
	var wfs []*WorkflowResource
	for i := 0; i <= n; i++ {
		name := "w" + itoa(i)
		if i == n {
			wfs = append(wfs, wfWithSteps(name)) // leaf
			continue
		}
		wfs = append(wfs, wfWithSteps(name, subStep("s", "w"+itoa(i+1), Pos{})))
	}
	err := ValidateProjectGraph(subGraph(wfs...), t.TempDir())
	if err == nil {
		t.Fatal("expected depth error")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestSubworkflow_depthAtLimit_ok(t *testing.T) {
	// Exactly the limit is allowed: limit edges, depth == DefaultMaxSubworkflowDepth.
	n := DefaultMaxSubworkflowDepth
	var wfs []*WorkflowResource
	for i := 0; i <= n; i++ {
		name := "w" + itoa(i)
		if i == n {
			wfs = append(wfs, wfWithSteps(name))
			continue
		}
		wfs = append(wfs, wfWithSteps(name, subStep("s", "w"+itoa(i+1), Pos{})))
	}
	if err := ValidateProjectGraph(subGraph(wfs...), t.TempDir()); err != nil {
		t.Fatalf("depth at limit should pass: %v", err)
	}
}

func TestSubworkflow_missingCallee_reportsMissingRef(t *testing.T) {
	g := subGraph(wfWithSteps("a", subStep("s", "ghost", Pos{File: "a.yaml", Line: 4})))
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil {
		t.Fatal("expected missing reference error")
	}
	if !strings.Contains(err.Error(), "Workflow/ghost") {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestSubworkflow_happyPath_validates(t *testing.T) {
	g := subGraph(
		wfWithSteps("caller", subStep("call", "callee", Pos{})),
		wfWithSteps("callee", WorkflowStep{ID: "leaf", Uses: "tool.helper.echo"}),
	)
	g.Tools = map[string]*ToolResource{
		"helper": {Kind: KindTool, Metadata: Metadata{Name: "helper"}, Spec: ToolSpec{Type: "native"}},
	}
	if err := ValidateProjectGraph(g, t.TempDir()); err != nil {
		t.Fatalf("happy path should validate: %v", err)
	}
}

func TestSubworkflow_stepExclusivity(t *testing.T) {
	g := subGraph(wfWithSteps("a", WorkflowStep{ID: "s", Agent: "x", Workflow: "a"}))
	err := ValidateProjectGraph(g, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "must set exactly one of agent, uses, or workflow") {
		t.Fatalf("expected exclusivity error, got %v", err)
	}
}

func TestSubworkflow_withCheckedAgainstCalleeInputSchema(t *testing.T) {
	root := t.TempDir()
	writeSchema(t, root, "schemas/caller-in.json", `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": { "topic": { "type": "string" } },
		"additionalProperties": false
	}`)
	writeSchema(t, root, "schemas/callee-in.json", `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": { "msg": { "type": "integer" } },
		"additionalProperties": false
	}`)

	callerYAML := `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: caller
spec:
  input:
    schema: ./schemas/caller-in.json
  steps:
    - id: call
      workflow: callee
      with:
        msg: ${input.topic}
`
	dec, err := ParseResourceFromBytes([]byte(callerYAML), "caller.yaml")
	if err != nil {
		t.Fatal(err)
	}
	caller := dec.Resource.(*WorkflowResource)
	g := &ProjectGraph{Workflows: map[string]*WorkflowResource{
		"caller": caller,
		"callee": {
			Kind:     KindWorkflow,
			Metadata: Metadata{Name: "callee"},
			Spec:     WorkflowSpec{Input: &WorkflowInput{Schema: "./schemas/callee-in.json"}},
		},
	}}

	err = ValidateProjectGraph(g, root)
	if err == nil {
		t.Fatal("expected a with/callee-input type mismatch")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Workflow/callee") {
		t.Fatalf("want callee schema name in %q", msg)
	}
	pos := caller.Spec.Steps[0].Pos.String()
	if pos == "" || !strings.Contains(msg, pos) {
		t.Fatalf("want positioned error containing %q, got %q", pos, msg)
	}
}

func TestSubworkflow_withMatchingCalleeInputSchema_ok(t *testing.T) {
	root := t.TempDir()
	writeSchema(t, root, "schemas/caller-in.json", `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": { "topic": { "type": "string" } },
		"additionalProperties": false
	}`)
	writeSchema(t, root, "schemas/callee-in.json", `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": { "msg": { "type": "string" } },
		"additionalProperties": false
	}`)

	callerYAML := `apiVersion: agentic.dev/v0
kind: Workflow
metadata:
  name: caller
spec:
  input:
    schema: ./schemas/caller-in.json
  steps:
    - id: call
      workflow: callee
      with:
        msg: ${input.topic}
`
	dec, err := ParseResourceFromBytes([]byte(callerYAML), "caller.yaml")
	if err != nil {
		t.Fatal(err)
	}
	caller := dec.Resource.(*WorkflowResource)
	g := &ProjectGraph{Workflows: map[string]*WorkflowResource{
		"caller": caller,
		"callee": {
			Kind:     KindWorkflow,
			Metadata: Metadata{Name: "callee"},
			Spec:     WorkflowSpec{Input: &WorkflowInput{Schema: "./schemas/callee-in.json"}},
		},
	}}
	if err := ValidateProjectGraph(g, root); err != nil {
		t.Fatalf("matching schema should validate: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
