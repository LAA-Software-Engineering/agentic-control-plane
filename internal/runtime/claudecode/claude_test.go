package claudecode

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRunner returns canned stdout (and optional error) and records the argv it was given.
func fakeRunner(stdout string, err error, gotArgv *[]string) processRunner {
	return func(_ context.Context, argv []string, _ string) (string, error) {
		if gotArgv != nil {
			*gotArgv = argv
		}
		return stdout, err
	}
}

const successStream = `
{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus","tools":["workspace_read_file"]}
{"type":"assistant","message":{"content":[{"type":"text","text":"Looking at the code."},{"type":"tool_use","id":"t1","name":"workspace_read_file","input":{"path":"main.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Done."}]}}
{"type":"result","subtype":"success","total_cost_usd":0.0123,"num_turns":2,"result":"All good.","is_error":false}
`

func TestParseStreamJSON_Success(t *testing.T) {
	s, err := parseStreamJSON(strings.NewReader(successStream))
	if err != nil {
		t.Fatal(err)
	}
	if s.SessionID != "s1" || s.Model != "claude-opus" || len(s.AdvertisedTools) != 1 {
		t.Fatalf("init not parsed: %+v", s)
	}
	if len(s.Turns) != 2 || len(s.ToolUses) != 1 || s.ToolUses[0].Name != "workspace_read_file" {
		t.Fatalf("turns/tooluses: %+v", s)
	}
	if s.FinalText != "All good." || s.NumTurns != 2 || s.CostUSD != 0.0123 || s.StopReason != StopSuccess {
		t.Fatalf("result fields: %+v", s)
	}
}

func TestParseStreamJSON_TolerantOfNoise(t *testing.T) {
	stream := "not json at all\n" + `{"type":"unknown_future_event"}` + "\n" + successStream
	s, err := parseStreamJSON(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("noise/unknown events must be skipped, got %v", err)
	}
	if s.StopReason != StopSuccess {
		t.Fatalf("stop: %+v", s)
	}
}

func TestParseStreamJSON_MissingResultIsError(t *testing.T) {
	stream := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`
	if _, err := parseStreamJSON(strings.NewReader(stream)); err == nil {
		t.Fatal("a stream with no result event must error")
	}
}

func TestRunSession_Success(t *testing.T) {
	var argv []string
	c := ClaudeCodeRuntime{Bin: "claude", Run: fakeRunner(successStream, nil, &argv)}
	s, err := c.RunSession(context.Background(), RunSpec{Prompt: "fix it", SystemPrompt: "you are a fixer", MaxTurns: 3, MCPConfig: "/tmp/run.json"})
	if err != nil {
		t.Fatal(err)
	}
	if s.FinalText != "All good." || s.StopReason != StopSuccess {
		t.Fatalf("session: %+v", s)
	}
	// argv carries the non-interactive contract + the spec-derived flags.
	joined := strings.Join(argv, " ")
	for _, want := range []string{"claude -p fix it", "--output-format stream-json", "--strict-mcp-config", "--system-prompt you are a fixer", "--max-turns 3", "--mcp-config /tmp/run.json"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("argv missing %q: %v", want, argv)
		}
	}
	// no built-in tools
	if !containsPair(argv, "--tools", "") {
		t.Fatalf("expected --tools \"\": %v", argv)
	}
}

func TestRunSession_MaxTurns(t *testing.T) {
	stream := `{"type":"result","subtype":"error_max_turns","num_turns":3,"total_cost_usd":0.05,"is_error":true}`
	c := ClaudeCodeRuntime{Run: fakeRunner(stream, nil, nil)}
	s, err := c.RunSession(context.Background(), RunSpec{Prompt: "x"})
	var mt *MaxTurnsError
	if !errors.As(err, &mt) || mt.NumTurns != 3 {
		t.Fatalf("expected *MaxTurnsError{3}, got %v", err)
	}
	if s.StopReason != StopMaxTurns { // session is still returned
		t.Fatalf("session should carry max_turns: %+v", s)
	}
}

func TestRunSession_ProcessErrorNoStream(t *testing.T) {
	c := ClaudeCodeRuntime{Run: fakeRunner("", errors.New("exec: \"claude\": not found"), nil)}
	if _, err := c.RunSession(context.Background(), RunSpec{Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "run agent") {
		t.Fatalf("a failed spawn with no stream must surface a run error, got %v", err)
	}
}

func TestRunSession_ErrorResult(t *testing.T) {
	stream := `{"type":"result","subtype":"error_during_execution","num_turns":1,"is_error":true}`
	c := ClaudeCodeRuntime{Run: fakeRunner(stream, nil, nil)}
	if _, err := c.RunSession(context.Background(), RunSpec{Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "ended in error") {
		t.Fatalf("an error result must be an error, got %v", err)
	}
}

func containsPair(argv []string, flag, val string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == val {
			return true
		}
	}
	return false
}
