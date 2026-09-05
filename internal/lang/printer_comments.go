package lang

import "strings"

// printer is the formatter's output sink: a strings.Builder plus a forward cursor over the file's
// comments, so `terfyn fmt` round-trips // line comments instead of deleting them (issue #509).
//
// The print functions write in source order, so comments are re-emitted by position:
//   - leadingBefore(line, indent) flushes every not-yet-emitted comment that starts on a source line
//     ABOVE the construct about to be printed, each on its own line at the construct's indent.
//   - trailingOn(line) attaches a same-line comment to the construct just written (before its
//     newline); it is a no-op unless the next pending comment trails code on exactly that line.
//   - flushRemaining is called once at the end so comments after the last declaration survive.
//
// The cursor only advances, and leadingBefore flushes ALL comments strictly above the next construct,
// so any comment a print path does not explicitly attach is still emitted (as its own line just above
// the following construct) rather than lost — the formatter never silently drops a comment.
type printer struct {
	strings.Builder
	comments []Comment
	ci       int
}

// leadingBefore emits every pending comment that starts on a line above `line`, each as its own
// `indent// text` line. Comments are emitted at the indent of the construct they precede.
func (p *printer) leadingBefore(line int, indent string) {
	for p.ci < len(p.comments) && p.comments[p.ci].Pos.Line < line {
		p.writeCommentLine(indent, p.comments[p.ci].Text)
		p.ci++
	}
}

// trailingOn attaches a same-line (trailing) comment to the line just written, before its newline.
// It consumes at most one comment and only when that comment trails code on exactly `line`.
func (p *printer) trailingOn(line int) {
	if p.ci < len(p.comments) {
		c := p.comments[p.ci]
		if !c.Standalone && c.Pos.Line == line {
			p.WriteString(" //")
			if c.Text != "" {
				p.WriteString(" ")
				p.WriteString(c.Text)
			}
			p.ci++
		}
	}
}

// flushRemaining emits any comments left after the last construct (trailing lines at end of file),
// so nothing is dropped. Called once at the end of Print.
func (p *printer) flushRemaining(indent string) {
	for p.ci < len(p.comments) {
		p.writeCommentLine(indent, p.comments[p.ci].Text)
		p.ci++
	}
}

// field writes a single-line construct — `indent+text` — then attaches a trailing comment for
// srcLine if one is pending, then the newline. Use it for leaf lines that can carry an inline comment
// (a `model x // note`, a grant, a workflow return). Emit any leading comments with leadingBefore
// before calling this.
func (p *printer) field(indent, text string, srcLine int) {
	p.WriteString(indent)
	p.WriteString(text)
	p.trailingOn(srcLine)
	p.WriteString("\n")
}

func (p *printer) writeCommentLine(indent, text string) {
	p.WriteString(indent)
	p.WriteString("//")
	if text != "" {
		p.WriteString(" ")
		p.WriteString(text)
	}
	p.WriteString("\n")
}
