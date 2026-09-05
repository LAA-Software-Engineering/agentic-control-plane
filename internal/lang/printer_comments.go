package lang

import (
	"sort"
	"strings"
)

// Comment preservation for `terfyn fmt` (issue #509).
//
// The printer emits fields in a fixed CANONICAL order, which is not the source order, so comments
// cannot be re-emitted by a "flush everything above the current print position" cursor — that would
// let a comment authored above one field glue itself to whichever field prints first, or leak out of
// its block entirely. Instead every comment is attached, up front, to a specific anchor by SOURCE
// position and brace scope, and emitted when that anchor prints — independent of print order:
//
//   - a trailing comment (same source line as code) attaches to that line;
//   - a standalone comment attaches as a LEADING comment of the next construct on a later line within
//     the SAME brace block (so it stays glued to what it documents even after canonical reordering);
//   - a standalone comment with no such following construct is a block TAIL comment, emitted at the
//     block's inner indent just before its closing brace, so it never escapes the block.
//
// A safety net (flushRemaining) emits anything an anchor never printed, so a comment is never dropped.
type commentIndex struct {
	texts      []string      // comment content by id, source order
	leading    map[int][]int // anchor line -> comment ids emitted before that line's construct
	trailing   map[int]int   // source line -> comment id emitted inline on that line
	tail       map[int][]int // block open line -> comment ids emitted before that block's closing brace
	unattached []int         // ids with no anchor (e.g. malformed input) — flushed at the end
}

// buildCommentIndex attaches every comment to an anchor by source position and brace scope. src is the
// original source (for the brace scan); comments are the lexer's collected comments in source order.
func buildCommentIndex(src string, comments []Comment) *commentIndex {
	idx := &commentIndex{
		leading:  map[int][]int{},
		trailing: map[int]int{},
		tail:     map[int][]int{},
	}
	if len(comments) == 0 {
		return idx
	}
	idx.texts = make([]string, len(comments))
	for i, c := range comments {
		idx.texts[i] = c.Text
	}

	// Brace scan over the token stream (so braces inside strings/comments are ignored): the depth at
	// each code line's first token, the sorted anchor lines, and each block's open->close line span.
	lineDepth := map[int]int{}
	var anchorLines []int
	seen := map[int]bool{}
	type span struct{ open, close int }
	var spans []span
	var openStack []int
	depth := 0
	// Comment depth is the brace depth at the comment's position; compute it by walking comments in
	// lockstep with the tokens (both are in source order).
	commentDepth := make([]int, len(comments))
	ck := 0
	assignCommentsBefore := func(pos Pos) {
		for ck < len(comments) && posBefore(comments[ck].Pos, pos) {
			commentDepth[ck] = depth
			ck++
		}
	}
	lx := NewLexer("", src)
	for {
		t := lx.Next()
		if t.Kind == KindEOF {
			break
		}
		assignCommentsBefore(t.Pos)
		if t.Kind == KindRBrace {
			if len(openStack) > 0 {
				open := openStack[len(openStack)-1]
				openStack = openStack[:len(openStack)-1]
				spans = append(spans, span{open: open, close: t.Pos.Line})
			}
			if depth > 0 {
				depth--
			}
		}
		if !seen[t.Pos.Line] {
			seen[t.Pos.Line] = true
			lineDepth[t.Pos.Line] = depth
			anchorLines = append(anchorLines, t.Pos.Line)
		}
		if t.Kind == KindLBrace {
			openStack = append(openStack, t.Pos.Line)
			depth++
		}
	}
	for ; ck < len(comments); ck++ {
		commentDepth[ck] = depth
	}
	sort.Ints(anchorLines)

	// enclosing returns the innermost block span containing line L (the one with the largest open
	// below L whose close is at or after L). ok=false means top level (no enclosing block).
	enclosing := func(L int) (span, bool) {
		best := span{}
		found := false
		for _, s := range spans {
			if s.open < L && L <= s.close {
				if !found || s.open > best.open {
					best = s
					found = true
				}
			}
		}
		return best, found
	}

	for id, c := range comments {
		if !c.Standalone {
			// Inline: attach to its own line. Keep the first when two share a line (rare).
			if _, exists := idx.trailing[c.Pos.Line]; !exists {
				idx.trailing[c.Pos.Line] = id
			} else {
				idx.unattached = append(idx.unattached, id)
			}
			continue
		}
		d := commentDepth[id]
		encl, hasEncl := enclosing(c.Pos.Line)
		closeLine := 1<<62 - 1
		openLine := 0
		if hasEncl {
			closeLine = encl.close
			openLine = encl.open
		}
		// Leading: the next construct on a later line, same depth, still inside this block.
		target := -1
		for _, a := range anchorLines {
			if a <= c.Pos.Line {
				continue
			}
			if a >= closeLine {
				break
			}
			if lineDepth[a] == d {
				target = a
				break
			}
		}
		if target >= 0 {
			idx.leading[target] = append(idx.leading[target], id)
			continue
		}
		// No following construct in this block -> a block tail comment, emitted before the block's `}`.
		if hasEncl {
			idx.tail[openLine] = append(idx.tail[openLine], id)
			continue
		}
		idx.unattached = append(idx.unattached, id)
	}
	return idx
}

// posBefore reports whether a precedes b in source order.
func posBefore(a, b Pos) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Column < b.Column
}

// printer is the formatter's output sink: a strings.Builder plus the comment attachment index and a
// per-id emitted flag, so each comment is written exactly once (issue #509).
type printer struct {
	strings.Builder
	idx     *commentIndex
	emitted []bool
}

func newPrinter(idx *commentIndex) *printer {
	if idx == nil {
		idx = &commentIndex{leading: map[int][]int{}, trailing: map[int]int{}, tail: map[int][]int{}}
	}
	return &printer{idx: idx, emitted: make([]bool, len(idx.texts))}
}

// leadingBefore emits the standalone comments attached as leading to `line`, each at the given indent.
func (p *printer) leadingBefore(line int, indent string) {
	for _, id := range p.idx.leading[line] {
		p.emitComment(id, indent)
	}
}

// blockTail emits the dangling comments attached to the block that opens on `openLine`, at the block's
// inner indent, just before its closing brace — so a comment at a block's end stays inside it.
func (p *printer) blockTail(openLine int, indent string) {
	for _, id := range p.idx.tail[openLine] {
		p.emitComment(id, indent)
	}
}

// trailingOn attaches the inline comment for `line` to the line just written, before its newline.
func (p *printer) trailingOn(line int) {
	id, ok := p.idx.trailing[line]
	if !ok || p.emitted[id] {
		return
	}
	p.emitted[id] = true
	p.WriteString(" //")
	if t := p.idx.texts[id]; t != "" {
		p.WriteString(" ")
		p.WriteString(t)
	}
}

// flushRemaining emits any comment no anchor printed (unattached, or an anchor line that was never
// reached), so nothing is ever dropped. Called once at the end of Print.
func (p *printer) flushRemaining(indent string) {
	for id := range p.emitted {
		if !p.emitted[id] {
			p.emitComment(id, indent)
		}
	}
}

func (p *printer) emitComment(id int, indent string) {
	if p.emitted[id] {
		return
	}
	p.emitted[id] = true
	p.WriteString(indent)
	p.WriteString("//")
	if t := p.idx.texts[id]; t != "" {
		p.WriteString(" ")
		p.WriteString(t)
	}
	p.WriteString("\n")
}

// field writes a single-line construct — `indent+text` — then attaches a trailing comment for srcLine
// if one is pending, then the newline. Emit leading comments with leadingBefore before calling this.
func (p *printer) field(indent, text string, srcLine int) {
	p.WriteString(indent)
	p.WriteString(text)
	p.trailingOn(srcLine)
	p.WriteString("\n")
}
