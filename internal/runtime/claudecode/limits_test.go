package claudecode

import (
	"testing"

	"github.com/Terfyn/terfyn/internal/runtime/agentcli"
)

// TestLimitsApplyTo lives here (not in agentcli) because it asserts the Claude argv reflects the
// mapped RunSpec — the ApplyTo mapping itself is covered in agentcli's limits_test.
func TestLimitsApplyTo(t *testing.T) {
	var rs agentcli.RunSpec
	agentcli.Limits{MaxTurns: 12, BudgetUSD: 3.0}.ApplyTo(&rs)
	if rs.MaxTurns != 12 || rs.MaxBudgetUSD != 3.0 {
		t.Fatalf("applied spec = %+v", rs)
	}
	// The mapped budget reaches the argv as --max-budget-usd.
	argv := ClaudeCodeRuntime{Bin: "claude"}.argv(rs)
	if !containsPair(argv, "--max-budget-usd", "3") || !containsPair(argv, "--max-turns", "12") {
		t.Fatalf("argv missing mapped limits: %v", argv)
	}
}
