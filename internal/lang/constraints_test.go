package lang

import "testing"

func TestParseAgentDescriptionAndConstraints(t *testing.T) {
	src := "agent A {\n" +
		"    model mock/gpt-4\n" +
		"    description \"a helper agent\"\n" +
		"    constraints {\n" +
		"        maxIterations 8\n" +
		"        maxTokens 32000\n" +
		"        timeoutSeconds 120\n" +
		"        temperature 0.2\n" +
		"        requireStructuredOutput true\n" +
		"    }\n" +
		"}\n"
	f, diags := Parse("a.agent", src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %s", diags.Error())
	}
	a := f.Decls[0].(*AgentDecl)
	if a.Description == nil || a.Description.Value != "a helper agent" {
		t.Fatalf("description: %+v", a.Description)
	}
	c := a.Constraints
	if c == nil {
		t.Fatalf("constraints not parsed")
	}
	if c.MaxIterations == nil || *c.MaxIterations != 8 {
		t.Fatalf("maxIterations: %+v", c.MaxIterations)
	}
	if c.MaxTokens == nil || *c.MaxTokens != 32000 {
		t.Fatalf("maxTokens: %+v", c.MaxTokens)
	}
	if c.TimeoutSeconds == nil || *c.TimeoutSeconds != 120 {
		t.Fatalf("timeoutSeconds: %+v", c.TimeoutSeconds)
	}
	if c.Temperature == nil || *c.Temperature != 0.2 {
		t.Fatalf("temperature: %+v", c.Temperature)
	}
	if c.RequireStructuredOutput == nil || *c.RequireStructuredOutput != true {
		t.Fatalf("requireStructuredOutput: %+v", c.RequireStructuredOutput)
	}
}

func TestParseConstraints_Diagnostics(t *testing.T) {
	cases := map[string]string{
		"unknown field":   "agent A {\n    constraints { nope 3 }\n}\n",
		"duplicate":       "agent A {\n    constraints { maxIterations 3\n maxIterations 4 }\n}\n",
		"non-int maxIter": "agent A {\n    constraints { maxIterations 3.5 }\n}\n",
		"non-int maxTok":  "agent A {\n    constraints { maxTokens 3.5 }\n}\n",
		"zero timeout":    "agent A {\n    constraints { timeoutSeconds 0 }\n}\n",
		"bad bool":        "agent A {\n    constraints { requireStructuredOutput 1 }\n}\n",
		"duplicate desc":  "agent A {\n    description \"one\"\n    description \"two\"\n}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if _, diags := Parse("a.agent", src); len(diags) == 0 {
				t.Fatalf("expected a diagnostic for %s", name)
			}
		})
	}
}

func TestParseWorkflowDescription(t *testing.T) {
	src := "workflow W(input: T) -> T\n    description \"does a thing\"\n    effects { a.read }\n{\n    return input\n}\n"
	f, diags := Parse("w.agent", src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %s", diags.Error())
	}
	w := f.Decls[0].(*WorkflowDecl)
	if w.Description == nil || w.Description.Value != "does a thing" {
		t.Fatalf("workflow description: %+v", w.Description)
	}
	if w.Effects == nil {
		t.Fatalf("effects clause dropped alongside description")
	}
}

func TestConstraintsAndDescriptionRoundTrip(t *testing.T) {
	src := "agent A {\n" +
		"    model mock/gpt-4\n" +
		"    description \"\"\"\n    multi\n    line\n    \"\"\"\n" +
		"    constraints {\n        maxIterations 8\n        timeoutSeconds 60\n    }\n" +
		"}\n\n" +
		"workflow W(input: T) -> T\n    description \"wf\"\n    effects { a.read }\n{\n    return input\n}\n"
	once, d1 := Format("a.agent", src)
	if len(d1) != 0 {
		t.Fatalf("format diags: %s", d1.Error())
	}
	twice, d2 := Format("a.agent", once)
	if len(d2) != 0 {
		t.Fatalf("reformat diags: %s", d2.Error())
	}
	if once != twice {
		t.Fatalf("not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestParseWorkflowPolicy(t *testing.T) {
	src := "workflow W(input: T) -> T\n    description \"d\"\n    policy cheap-ceiling\n    effects { a.read }\n{\n    return input\n}\n"
	f, diags := Parse("w.agent", src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %s", diags.Error())
	}
	w := f.Decls[0].(*WorkflowDecl)
	if w.Policy == nil || w.Policy.Name != "cheap-ceiling" {
		t.Fatalf("workflow policy: %+v", w.Policy)
	}
	// Round-trips.
	once, _ := Format("w.agent", src)
	twice, _ := Format("w.agent", once)
	if once != twice {
		t.Fatalf("not idempotent:\n%s\n---\n%s", once, twice)
	}
	f2, _ := Parse("w.agent", once)
	if f2.Decls[0].(*WorkflowDecl).Policy.Name != "cheap-ceiling" {
		t.Fatalf("policy lost on round-trip")
	}
}
