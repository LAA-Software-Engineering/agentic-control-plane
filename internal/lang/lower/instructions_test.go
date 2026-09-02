package lower_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Terfyn/terfyn/internal/lang"
	"github.com/Terfyn/terfyn/internal/lang/lower"
	"github.com/Terfyn/terfyn/internal/spec"
)

// TestLower_InstructionsCopiedExactly asserts the `.agent` instructions field
// lowers verbatim into AgentSpec.Instructions (the multiline body already
// normalized by the lexer), and survives a YAML export/reload round trip.
func TestLower_InstructionsCopiedExactly(t *testing.T) {
	src := "agent Implementer {\n" +
		"    model anthropic/claude-sonnet-4-20250514\n" +
		"    instructions \"\"\"\n" +
		"    You are the implementation agent.\n" +
		"\n" +
		"    Preserve the original task.\n" +
		"    \"\"\"\n" +
		"}\n"
	const want = "You are the implementation agent.\n\nPreserve the original task."

	f, diags := lang.Parse("impl.agent", src)
	if len(diags) > 0 {
		t.Fatalf("parse diags: %s", diags.Error())
	}
	res, ld := lower.LowerFile(f, lower.Options{})
	if len(ld) > 0 {
		t.Fatalf("lower diags: %s", ld.Error())
	}
	if len(res.Agents) != 1 {
		t.Fatalf("want 1 agent, got %d", len(res.Agents))
	}
	if got := res.Agents[0].Spec.Instructions; got != want {
		t.Fatalf("lowered instructions:\n got %q\nwant %q", got, want)
	}

	// Export to YAML then reload: the field must survive unchanged.
	out, err := yaml.Marshal(res.Agents[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "instructions:") {
		t.Fatalf("exported YAML has no instructions field:\n%s", out)
	}
	var reloaded spec.AgentResource
	if err := yaml.Unmarshal(out, &reloaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if reloaded.Spec.Instructions != want {
		t.Fatalf("reload changed instructions:\n got %q\nwant %q", reloaded.Spec.Instructions, want)
	}
}

// TestLower_InstructionsFileUnresolvedIsDiagnostic asserts a file("...") reference that reaches
// lowering WITHOUT loader resolution (Resolved == nil) is a hard diagnostic, not a silent empty
// prompt pinned into the spec hash (#360 review). The project loader resolves the ref before
// lowering; this guards a future path that skips it.
func TestLower_InstructionsFileUnresolvedIsDiagnostic(t *testing.T) {
	f, diags := lang.Parse("a.agent", "agent R {\n    instructions file(\"prompts/r.md\")\n}\n")
	if diags.HasErrors() {
		t.Fatalf("parse diags: %s", diags.Error())
	}
	_, ld := lower.LowerFile(f, lower.Options{})
	if !ld.HasErrors() {
		t.Fatal("an unresolved instructions file() ref must be a lowering diagnostic")
	}
	if !strings.Contains(ld.Error(), "without loader resolution") {
		t.Fatalf("diagnostic = %q", ld.Error())
	}
}
