package cli

import (
	"strings"
	"testing"

	"github.com/Terfyn/terfyn/internal/trace"
)

func TestFormatVerboseEvent(t *testing.T) {
	cases := []struct {
		name    string
		ev      trace.StreamEvent
		wantSub []string // substrings the rendered line must contain
		skip    bool     // event kind not part of the live view
	}{
		{
			name:    "tool_selection",
			ev:      trace.StreamEvent{StepID: "triage", Type: trace.EventToolSelection, Actor: trace.ActorAgent, Data: map[string]any{"uses": "tool.github.issues.get"}},
			wantSub: []string{"triage", "tool_selection", "tool.github.issues.get"},
		},
		{
			name:    "tool_execution ok",
			ev:      trace.StreamEvent{StepID: "triage", Type: trace.EventToolExecution, Data: map[string]any{"uses": "tool.workspace.read_file", "success": true, "durationMs": int64(4)}},
			wantSub: []string{"tool_execution", "tool.workspace.read_file", "ok", "(4ms)"},
		},
		{
			name:    "tool_execution err",
			ev:      trace.StreamEvent{StepID: "triage", Type: trace.EventToolExecution, Data: map[string]any{"uses": "tool.workspace.read_file", "success": false}},
			wantSub: []string{"tool_execution", "err"},
		},
		{
			name:    "llm_completion shows agent and cost",
			ev:      trace.StreamEvent{StepID: "s1", Type: trace.EventLLMCompletion, Data: map[string]any{"agent": "Triager", "costUsd": 0.0041}},
			wantSub: []string{"Triager", "llm_completion", "$0.0041"},
		},
		{
			name:    "limit_hit",
			ev:      trace.StreamEvent{StepID: "s1", Type: trace.EventLimitHit, Data: map[string]any{"kind": "max_iterations"}},
			wantSub: []string{"limit_hit", "max_iterations"},
		},
		{
			name:    "approval pause",
			ev:      trace.StreamEvent{StepID: "publish", Type: trace.EventHitlRequestCreated, Data: map[string]any{"uses": "tool.github.pull_request.create"}},
			wantSub: []string{"approval required", "tool.github.pull_request.create"},
		},
		{
			name: "run_started is skipped",
			ev:   trace.StreamEvent{Type: trace.EventRunStarted, Actor: trace.ActorAgent},
			skip: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line, ok := formatVerboseEvent(tc.ev, false)
			if tc.skip {
				if ok {
					t.Fatalf("event %s should be skipped, got %q", tc.ev.Type, line)
				}
				return
			}
			if !ok {
				t.Fatalf("event %s should render", tc.ev.Type)
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(line, sub) {
					t.Fatalf("line %q missing %q", line, sub)
				}
			}
		})
	}
}

func TestFormatVerboseEvent_noColorUsesAscii(t *testing.T) {
	ev := trace.StreamEvent{StepID: "s1", Type: trace.EventToolSelection, Data: map[string]any{"uses": "tool.x.y"}}
	line, ok := formatVerboseEvent(ev, true)
	if !ok {
		t.Fatal("should render")
	}
	if strings.ContainsAny(line, "▸⚠✗⏸…") {
		t.Fatalf("no-color line must not contain glyphs: %q", line)
	}
	if !strings.HasPrefix(line, "-") {
		t.Fatalf("no-color step mark should be '-': %q", line)
	}
}
