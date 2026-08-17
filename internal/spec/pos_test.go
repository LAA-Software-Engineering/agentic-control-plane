package spec

import "testing"

func TestPosString_fileLineCol(t *testing.T) {
	p := Pos{File: "workflow.yaml", Line: 8, Column: 14}
	if got := p.String(); got != "workflow.yaml:8:14" {
		t.Fatalf("String() = %q", got)
	}
}

func TestPosErrorf_prefixesLocation(t *testing.T) {
	p := Pos{File: "agent.yaml", Line: 3, Column: 1}
	err := p.Errorf("Agent/%s: bad", "x")
	if err.Error() != "agent.yaml:3:1: Agent/x: bad" {
		t.Fatalf("got %q", err.Error())
	}
}

func TestPosErrorf_zeroIsUnprefixed(t *testing.T) {
	err := Pos{}.Errorf("Agent/%s: bad", "x")
	if err.Error() != "Agent/x: bad" {
		t.Fatalf("got %q", err.Error())
	}
}
