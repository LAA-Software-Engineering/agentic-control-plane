package gemini

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Terfyn/terfyn/internal/runtime/agentcli"
)

// parseGeminiJSON parses `gemini -p --output-format json` output into an agentcli.Session.
//
// UNVERIFIED SCHEMA. The shape below is the spike's best reading; it must be confirmed against a
// pinned Gemini. It is tolerant: the final assistant text is taken from "response" (falling back to
// the raw trimmed output if the payload is not the expected object), and cost/turns are best-effort
// since Gemini's JSON does not report a USD cost. A process that exits 0 is a success; RunSession
// overrides StopReason to error on a non-zero exit. AdvertisedTools is not present in this output, so
// the Gemini S9 test relies on the execution-layer control (a built-in write that must not happen)
// rather than the init-layer advertised-tools control the Claude test can use.
func parseGeminiJSON(stdout string) (agentcli.Session, error) {
	raw := strings.TrimSpace(stdout)
	if raw == "" {
		return agentcli.Session{}, fmt.Errorf("gemini: empty output")
	}
	var out geminiOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		// Not the expected object (e.g. a plain-text response). Keep the text; the exit code is the
		// real success signal, applied by RunSession.
		return agentcli.Session{
			FinalText:  raw,
			Model:      "",
			NumTurns:   1,
			Turns:      []agentcli.Turn{{Text: raw}},
			StopReason: agentcli.StopSuccess,
		}, nil
	}
	text := out.Response
	if strings.TrimSpace(text) == "" {
		text = raw
	}
	s := agentcli.Session{
		SessionID:  out.SessionID,
		Model:      out.Model,
		FinalText:  text,
		NumTurns:   1,
		Turns:      []agentcli.Turn{{Text: text}},
		StopReason: agentcli.StopSuccess,
	}
	if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
		s.IsError = true
		s.StopReason = agentcli.StopError
		s.FinalText = out.Error.Message
	}
	return s, nil
}

type geminiOutput struct {
	Response  string `json:"response"`
	SessionID string `json:"sessionId"`
	Model     string `json:"model"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}
