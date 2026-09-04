package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/Terfyn/terfyn/internal/trace"
)

// newVerboseSink returns a [trace.EventSink] that renders each streamed trace event as one human
// line on w — the `terfyn run --verbose` live view (issue #450). It writes to stderr so `-o json`
// stdout stays clean and parseable. Only the events worth watching live are rendered (model
// completions, tool selection/execution, limit hits, approval pauses, errors); the rest are skipped
// to keep the stream focused. noColor swaps the leading glyphs for ASCII.
func newVerboseSink(w io.Writer, noColor bool) trace.EventSink {
	return func(ev trace.StreamEvent) {
		if line, ok := formatVerboseEvent(ev, noColor); ok {
			fmt.Fprintln(w, line)
		}
	}
}

type verboseMark int

const (
	markStep verboseMark = iota
	markWarn
	markErr
	markPause
)

func markGlyph(m verboseMark, noColor bool) string {
	if noColor {
		switch m {
		case markWarn:
			return "*"
		case markErr:
			return "x"
		case markPause:
			return "#"
		default:
			return "-"
		}
	}
	switch m {
	case markWarn:
		return "⚠"
	case markErr:
		return "✗"
	case markPause:
		return "⏸"
	default:
		return "▸"
	}
}

// formatVerboseEvent renders one streamed event, or (,"",false) when the event kind is not part of
// the live view.
func formatVerboseEvent(ev trace.StreamEvent, noColor bool) (string, bool) {
	label := verboseLabel(ev)
	switch ev.Type {
	case trace.EventLLMCompletion:
		return verboseLine(noColor, markStep, label, "llm_completion", verboseCost(ev.Data)), true
	case trace.EventToolSelection:
		return verboseLine(noColor, markStep, label, "tool_selection", stringField(ev.Data, "uses", "tool")), true
	case trace.EventToolExecution:
		status := "ok"
		if ok, _ := ev.Data["success"].(bool); !ok {
			status = "err"
		}
		detail := joinNonEmpty("  ", stringField(ev.Data, "uses", "tool"), status, verboseDuration(ev.Data))
		return verboseLine(noColor, markStep, label, "tool_execution", detail), true
	case trace.EventLimitHit:
		return verboseLine(noColor, markWarn, label, "limit_hit", stringField(ev.Data, "kind")), true
	case trace.EventRunError, trace.EventSystemError:
		return verboseLine(noColor, markErr, label, string(ev.Type), stringField(ev.Data, "reason", "error")), true
	case trace.EventHitlRequestCreated:
		uses := stringField(ev.Data, "uses")
		return verboseLine(noColor, markPause, label, "approval", strings.TrimSpace("approval required: "+uses)), true
	default:
		return "", false
	}
}

// verboseLabel picks a short actor label: the agent name when present, else the step id, else the
// event's actor kind.
func verboseLabel(ev trace.StreamEvent) string {
	if a := stringField(ev.Data, "agent"); a != "" {
		return a
	}
	if ev.StepID != "" {
		return ev.StepID
	}
	return string(ev.Actor)
}

func verboseLine(noColor bool, m verboseMark, label, typ, detail string) string {
	line := fmt.Sprintf("%s %s %s", markGlyph(m, noColor), padRight(truncate(label, 18), 18), padRight(typ, 16))
	if detail = strings.TrimSpace(detail); detail != "" {
		line += " " + detail
	}
	return strings.TrimRight(line, " ")
}

func verboseCost(data map[string]any) string {
	if c, ok := data["costUsd"].(float64); ok && c > 0 {
		return fmt.Sprintf("$%.4f", c)
	}
	return ""
}

func verboseDuration(data map[string]any) string {
	var ms int64
	switch d := data["durationMs"].(type) {
	case float64:
		ms = int64(d)
	case int64:
		ms = d
	case int:
		ms = int64(d)
	}
	if ms <= 0 {
		return ""
	}
	return fmt.Sprintf("(%dms)", ms)
}

// stringField returns the first present, non-empty string value among keys.
func stringField(data map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := data[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

func padRight(s string, n int) string {
	if len([]rune(s)) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len([]rune(s)))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}
