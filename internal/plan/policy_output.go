package plan

import (
	"fmt"
	"strings"

	"github.com/LAA-Software-Engineering/agentic-control-plane/internal/policy"
)

// FormatEffectivePolicy renders compiled per-tool decisions for human-readable plan output.
func FormatEffectivePolicy(policyName string, cp *policy.CompiledPolicy) string {
	if cp == nil || len(cp.Tools) == 0 {
		return ""
	}
	entries := cp.EffectivePolicyEntries()
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nEffective policy (%s):\n", strings.TrimSpace(policyName))
	for _, e := range entries {
		fmt.Fprintf(&b, "- Tool/%s decision=%s source=%s\n", e.Tool, e.Decision, e.Source)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
