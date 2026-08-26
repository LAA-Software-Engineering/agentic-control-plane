package lang

import (
	"fmt"
	"sort"
	"strings"
)

// Diagnostic is one positioned parse or lex error. The parser recovers after
// each error (see parser.synchronize) so a single file yields every diagnostic
// it can find in one pass rather than stopping at the first.
type Diagnostic struct {
	Pos Pos
	Msg string
}

// Error formats the diagnostic as "file:line:col: message" using the shared
// spec.Pos formatting; the location prefix is omitted when unknown.
func (d Diagnostic) Error() string {
	if loc := d.Pos.String(); loc != "" {
		return loc + ": " + d.Msg
	}
	return d.Msg
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
