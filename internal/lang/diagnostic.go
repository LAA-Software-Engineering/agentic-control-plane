package lang

import (
	"fmt"
	"sort"
	"strings"
)

// Severity distinguishes a fatal diagnostic from an advisory one. The zero
// value is SeverityError so every pre-existing call site (lexer, parser,
// lowering) that constructs a Diagnostic{Pos, Msg} without setting Severity is
// unaffected — every diagnostic before #198 was an error.
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

func (s Severity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return "error"
}

// Diagnostic is one positioned parse, lowering, or checking problem. The parser
// recovers after each error (see parser.synchronize) so a single file yields
// every diagnostic it can find in one pass rather than stopping at the first.
type Diagnostic struct {
	Pos      Pos
	Msg      string
	Severity Severity
}

// Error formats the diagnostic as "file:line:col: message" using the shared
// spec.Pos formatting; the location prefix is omitted when unknown. A warning
// is prefixed so it reads distinctly from an error in combined output.
func (d Diagnostic) Error() string {
	msg := d.Msg
	if d.Severity == SeverityWarning {
		msg = "warning: " + msg
	}
	if loc := d.Pos.String(); loc != "" {
		return loc + ": " + msg
	}
	return msg
}

// Diagnostics is an ordered collection of parse diagnostics.
type Diagnostics []Diagnostic

// Sorted returns the diagnostics ordered by position (file, then line, then
// column) so caller output is deterministic regardless of recovery order.
func (ds Diagnostics) Sorted() Diagnostics {
	out := make(Diagnostics, len(ds))
	copy(out, ds)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Pos, out[j].Pos
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Column < b.Column
	})
	return out
}

// HasErrors reports whether ds contains at least one SeverityError diagnostic.
// A caller should treat a Diagnostics value with only warnings as non-fatal.
func (ds Diagnostics) HasErrors() bool {
	for _, d := range ds {
		if d.Severity != SeverityWarning {
			return true
		}
	}
	return false
}

// Error joins every diagnostic on its own line so Diagnostics satisfies the
// error interface for callers that only need a combined message.
func (ds Diagnostics) Error() string {
	parts := make([]string, len(ds))
	for i, d := range ds {
		parts[i] = d.Error()
	}
	return strings.Join(parts, "\n")
}

// diagf is a small helper for constructing a diagnostic with a formatted message.
func diagf(pos Pos, format string, args ...any) Diagnostic {
	return Diagnostic{Pos: pos, Msg: fmt.Sprintf(format, args...)}
}
