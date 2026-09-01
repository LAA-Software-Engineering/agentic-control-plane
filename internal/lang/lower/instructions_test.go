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
