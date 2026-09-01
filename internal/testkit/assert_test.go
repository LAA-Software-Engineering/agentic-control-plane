package testkit

import "testing"

func TestIsAssertSuiteBytes(t *testing.T) {
	if !isAssertSuiteBytes([]byte("assert:\n  forbidEffect:\n    - {agent: R, effect: x}\n")) {
		t.Fatal("an assert suite should be detected")
	}
	if isAssertSuiteBytes([]byte("workflow: demo\ncases:\n  - name: c\n")) {
		t.Fatal("a workflow suite must not be detected as an assert suite")
	}
}

func TestParseAssertSuiteBytes(t *testing.T) {
	s, err := ParseAssertSuiteBytes([]byte(`
name: caps
assert:
  forbidEffect:
    - {agent: Reviewer, effect: workspace.write}
  expectAutonomous:
    - {agent: Implementer, effect: workspace.write}
  expectGated:
    - tool.git.push_branch
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Assert.ForbidEffect) != 1 || s.Assert.ForbidEffect[0].rootName() != "Reviewer" || s.Assert.ForbidEffect[0].Effect != "workspace.write" {
		t.Fatalf("forbidEffect not parsed: %+v", s.Assert)
	}
	if len(s.Assert.ExpectAutonomous) != 1 || s.Assert.ExpectAutonomous[0].rootName() != "Implementer" {
		t.Fatalf("expectAutonomous not parsed: %+v", s.Assert)
	}
	if len(s.Assert.ExpectGated) != 1 || s.Assert.ExpectGated[0] != "tool.git.push_branch" {
		t.Fatalf("expectGated not parsed: %+v", s.Assert)
	}
}

func TestParseAssertSuiteBytes_Invalid(t *testing.T) {
	if _, err := ParseAssertSuiteBytes([]byte("name: empty\nassert: {}\n")); err == nil {
		t.Fatal("an assert suite with no assertions should error")
	}
	if _, err := ParseAssertSuiteBytes([]byte("assert:\n  forbidEffect:\n    - {agent: R}\n")); err == nil {
		t.Fatal("forbidEffect missing an effect should error")
	}
	if _, err := ParseAssertSuiteBytes([]byte("assert:\n  forbidEffect:\n    - {effect: x}\n")); err == nil {
		t.Fatal("forbidEffect missing a root should error")
	}
}

func TestRootEffectAliases(t *testing.T) {
	for _, re := range []rootEffectYAML{{Root: "A"}, {Agent: "A"}, {Workflow: "A"}} {
		if re.rootName() != "A" {
			t.Fatalf("root alias not resolved: %+v", re)
		}
	}
}
