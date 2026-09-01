package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// parseStreamJSON parses Claude Code's `--output-format stream-json` output: newline-delimited JSON
// events. It is tolerant — unknown event types and fields are ignored — so a CLI that adds events
// does not break the adapter. The event shapes it reads:
//
//	{"type":"system","subtype":"init","session_id":..,"model":..,"tools":[..]}
//	{"type":"assistant","message":{"content":[{"type":"text","text":..} | {"type":"tool_use","id":..,"name":..,"input":{..}}]}}
//	{"type":"result","subtype":"success"|"error_max_turns"|..,"total_cost_usd":..,"num_turns":..,"result":"..","is_error":bool}
//
// The result event is authoritative for cost / turn count / stop reason / final text.
func parseStreamJSON(r io.Reader) (Session, error) {
	var s Session
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024) // allow long lines (tool inputs, final text)

	sawResult := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// A non-JSON line (a stray log) is skipped rather than failing the whole session.
			continue
		}
		switch ev.Type {
		case "system":
			if ev.Subtype == "init" {
				s.SessionID = ev.SessionID
				s.Model = ev.Model
				s.AdvertisedTools = append(s.AdvertisedTools, ev.Tools...)
			}
		case "assistant":
			turn := assistantTurn(ev.Message)
			s.Turns = append(s.Turns, turn)
			s.ToolUses = append(s.ToolUses, turn.ToolUses...)
		case "result":
			sawResult = true
			s.FinalText = ev.Result
			s.NumTurns = ev.NumTurns
			s.CostUSD = ev.TotalCostUSD
			s.IsError = ev.IsError
			s.StopReason = normalizeStop(ev.Subtype, ev.IsError)
		}
	}
	if err := sc.Err(); err != nil {
		return s, fmt.Errorf("claudecode: read stream: %w", err)
	}
	if !sawResult {
		return s, fmt.Errorf("claudecode: stream ended without a result event")
	}
	return s, nil
}

func assistantTurn(m *streamMessage) Turn {
	var t Turn
	if m == nil {
		return t
	}
	var text strings.Builder
	for _, block := range m.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "tool_use":
			t.ToolUses = append(t.ToolUses, ToolUse{ID: block.ID, Name: block.Name, Input: block.Input})
		}
	}
	t.Text = text.String()
	return t
}

func normalizeStop(subtype string, isError bool) StopReason {
	switch strings.TrimSpace(subtype) {
	case "success":
		return StopSuccess
	case "error_max_turns":
		return StopMaxTurns
	}
	if isError {
		return StopError
	}
	if subtype == "" {
		return StopSuccess
	}
	return StopError
}

type streamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	// system/init
	SessionID string   `json:"session_id"`
	Model     string   `json:"model"`
	Tools     []string `json:"tools"`
	// assistant
	Message *streamMessage `json:"message"`
	// result
	Result       string  `json:"result"`
	NumTurns     int     `json:"num_turns"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	IsError      bool    `json:"is_error"`
}

type streamMessage struct {
	Content []streamBlock `json:"content"`
}

type streamBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
