package lang

import (
	"strings"
	"testing"
)

func TestParse_instructionsFileRef(t *testing.T) {
	src := "agent R {\n    model mock/gpt-4\n    instructions file(\"prompts/r.md\")\n}\n"
	f, diags := Parse("t.agent", src)
	if diags.HasErrors() {
		t.Fatalf("parse errors: %v", diags)
	}
	ad := f.Decls[0].(*AgentDecl)
	if ad.Instructions != nil {
		t.Fatal("inline Instructions should be nil for the file() form")
	}
	if ad.InstructionsFile == nil || ad.InstructionsFile.Path == nil {
		t.Fatalf("InstructionsFile not parsed: %+v", ad.InstructionsFile)
	}
	if ad.InstructionsFile.Path.Value != "prompts/r.md" {
		t.Fatalf("path = %q", ad.InstructionsFile.Path.Value)
	}
	if ad.InstructionsFile.Resolved != "" {
		t.Fatal("Resolved must be empty after a bare parse (the loader fills it)")
	}
	// fmt round-trips the file() reference, never resolved text, and is idempotent.
	out := Print(f)
	if !strings.Contains(out, "instructions file(\"prompts/r.md\")") {
		t.Fatalf("printer did not round-trip file():\n%s", out)
	}
	f2, d2 := Parse("t.agent", out)
	if d2.HasErrors() || Print(f2) != out {
		t.Fatalf("file() form is not print-idempotent:\n%s", out)
	}
}

func TestParse_instructionsInlineUnaffected(t *testing.T) {
	f, diags := Parse("t.agent", "agent R {\n    instructions \"inline prompt\"\n}\n")
	if diags.HasErrors() {
		t.Fatal(diags)
	}
	ad := f.Decls[0].(*AgentDecl)
	if ad.InstructionsFile != nil {
		t.Fatal("inline form must not set InstructionsFile")
	}
	if ad.Instructions == nil || ad.Instructions.Value != "inline prompt" {
		t.Fatalf("inline instructions broke: %+v", ad.Instructions)
	}
}

func TestParse_instructionsDuplicateRejected(t *testing.T) {
	_, diags := Parse("t.agent", "agent R {\n    instructions \"x\"\n    instructions file(\"y.md\")\n}\n")
	if !diags.HasErrors() {
		t.Fatal("instructions given twice (inline then file) must be a duplicate-field error")
	}
}

func TestParse_instructionsFileMissingParen(t *testing.T) {
	_, diags := Parse("t.agent", "agent R {\n    instructions file\n}\n")
	if !diags.HasErrors() {
		t.Fatal("file without (\"...\") must error")
	}
}
