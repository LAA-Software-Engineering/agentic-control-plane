package claudecode

import (
	"context"
	"strings"
	"testing"
)

func TestCheckNoBuiltinToolExposure_AdapterArgvIsSound(t *testing.T) {
	// The adapter's own argv (empty --tools denial + strict MCP config) must pass the guard.
	c := ClaudeCodeRuntime{Bin: "claude"}
	argv := c.argv(RunSpec{Prompt: "go", MCPConfig: "/tmp/run.json"})
	if err := checkNoBuiltinToolExposure(argv); err != nil {
		t.Fatalf("the adapter's own argv must be sound, got %v", err)
	}
}

func TestCheckNoBuiltinToolExposure_MCPAllowlistIsSound(t *testing.T) {
	// Allow-listing Terfyn's own MCP tools (mcp__ namespace) is sound — they route through Terfyn.
	sound := [][]string{
		{"claude", "--allowedTools", "mcp__terfyn__workspace_read_file,mcp__terfyn__workspace_run_tests"},
		{"claude", "--tools", ""},
		{"claude", "--disallowedTools", "Bash"}, // restricting is always sound
		{"claude", "--allowed-tools=mcp__terfyn__x"},
	}
	for _, argv := range sound {
		if err := checkNoBuiltinToolExposure(argv); err != nil {
			t.Fatalf("argv %v should be sound, got %v", argv, err)
		}
	}
}

func TestCheckNoBuiltinToolExposure_BuiltinExposureRejected(t *testing.T) {
	cases := [][]string{
		{"claude", "--allowedTools", "Bash"},
		{"claude", "--allowedTools", "mcp__terfyn__ok,Edit"}, // one bad token taints the list
		{"claude", "--tools", "Bash,WebFetch"},               // non-empty --tools value
		{"claude", "--allowed-tools=Read"},                   // = form
		{"claude", "--dangerously-skip-permissions"},         // boundary bypass
		{"claude", "--permission-mode", "bypassPermissions"}, // boundary bypass
		{"claude", "--permission-mode=bypassPermissions"},    // = form
	}
	for _, argv := range cases {
		err := checkNoBuiltinToolExposure(argv)
		if err == nil {
			t.Fatalf("argv %v must be rejected as unsound", argv)
		}
		if !strings.Contains(err.Error(), "S9") {
			t.Fatalf("error should cite S9, got %v", err)
		}
	}
}

// The guard runs inside RunSession: an ExtraArgs that smuggles a built-in is refused before any
// process spawn (the fake runner must never be called).
func TestRunSession_ExtraArgsBuiltinExposureRefused(t *testing.T) {
	called := false
	runner := func(_ context.Context, _ []string, _ string) (string, error) {
		called = true
		return successStream, nil
	}
	c := ClaudeCodeRuntime{Run: runner}
	_, err := c.RunSession(context.Background(), RunSpec{Prompt: "x", ExtraArgs: []string{"--allowedTools", "Bash"}})
	if err == nil || !strings.Contains(err.Error(), "S9") {
		t.Fatalf("smuggled built-in via ExtraArgs must be refused, got %v", err)
	}
	if called {
		t.Fatal("the process must not be spawned when the argv is unsound")
	}
}
