package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// WriteTable writes a fixed-width column table to w (design doc section 5.1 / table output MVP).
// noColor is reserved for future ANSI styling; when true, callers should omit color codes.
func WriteTable(w io.Writer, headers []string, rows [][]string, noColor bool) error {
	_ = noColor
	var b strings.Builder
	for _, h := range headers {
		b.WriteString(h)
		b.WriteByte('\t')
	}
	b.WriteByte('\n')
	for _, row := range rows {
		for _, cell := range row {
			b.WriteString(cell)
			b.WriteByte('\t')
		}
		b.WriteByte('\n')
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := tw.Write([]byte(b.String())); err != nil {
		return err
	}
	return tw.Flush()
}

// WriteKeyValueTable writes two columns (key, value), one pair per row.
func WriteKeyValueTable(w io.Writer, pairs [][2]string, noColor bool) error {
	rows := make([][]string, len(pairs))
	for i := range pairs {
		rows[i] = []string{pairs[i][0], pairs[i][1]}
	}
	return WriteTable(w, []string{"KEY", "VALUE"}, rows, noColor)
}

// Fprintf is a thin wrapper for formatted human output (table mode).
func Fprintf(w io.Writer, format string, a ...any) (int, error) {
	return fmt.Fprintf(w, format, a...)
}
