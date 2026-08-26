package lang

import "unicode/utf8"

// Lexer scans .agent source into a token stream. It is newline-insensitive:
// statement and field boundaries are recovered by the grammar (each construct
// has a deterministic shape), so newlines are ordinary whitespace and are not
// emitted as tokens. Line comments (// to end of line) are skipped.
type Lexer struct {
	src  string
	file string

	// offset is the byte index of the next rune to read.
	offset int
	// line and col are the 1-based position of the rune at offset.
	line, col int

	diags Diagnostics
}

// NewLexer returns a lexer over src. file is recorded in every token's Pos.
func NewLexer(file, src string) *Lexer {
	return &Lexer{src: src, file: file, offset: 0, line: 1, col: 1}
}

// Diagnostics returns any lexical errors accumulated so far (stray runes).
func (l *Lexer) Diagnostics() Diagnostics { return l.diags }

// pos returns the current position.
func (l *Lexer) pos() Pos { return Pos{File: l.file, Line: l.line, Column: l.col} }

// peek returns the rune at offset without consuming it, and its byte width.
// It returns (utf8.RuneError, 0) at end of input.
func (l *Lexer) peek() (rune, int) {
	if l.offset >= len(l.src) {
		return utf8.RuneError, 0
	}
	r, w := utf8.DecodeRuneInString(l.src[l.offset:])
	return r, w
}

// advance consumes one rune, updating offset, line, and column.
func (l *Lexer) advance() rune {
	r, w := l.peek()
	if w == 0 {
		return utf8.RuneError
	}
	l.offset += w
	if r == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return r
}

// Next returns the next token. After end of input it returns KindEOF
// repeatedly.
func (l *Lexer) Next() Token {
	l.skipTrivia()
	start := l.pos()
	r, w := l.peek()
	if w == 0 {
		return Token{Kind: KindEOF, Pos: start}
	}

	switch {
	case isIdentStart(r):
		return l.scanIdent(start)
	case r == '{':
		l.advance()
		return Token{Kind: KindLBrace, Lit: "{", Pos: start}
	case r == '}':
		l.advance()
		return Token{Kind: KindRBrace, Lit: "}", Pos: start}
	case r == '(':
		l.advance()
		return Token{Kind: KindLParen, Lit: "(", Pos: start}
	case r == ')':
		l.advance()
		return Token{Kind: KindRParen, Lit: ")", Pos: start}
	case r == '.':
		l.advance()
		return Token{Kind: KindDot, Lit: ".", Pos: start}
	case r == '/':
		l.advance()
		return Token{Kind: KindSlash, Lit: "/", Pos: start}
	case r == ',':
		l.advance()
		return Token{Kind: KindComma, Lit: ",", Pos: start}
	case r == ':':
		l.advance()
		return Token{Kind: KindColon, Lit: ":", Pos: start}
	case r == '=':
		l.advance()
		return Token{Kind: KindEquals, Lit: "=", Pos: start}
	case r == '-':
		// The only multi-rune operator: -> . A lone '-' is not an identifier
		// start (isIdentStart excludes it) and is a lexer error.
		l.advance()
		if next, nw := l.peek(); nw != 0 && next == '>' {
			l.advance()
			return Token{Kind: KindArrow, Lit: "->", Pos: start}
		}
		l.errorf(start, "unexpected %q (did you mean '->'?)", "-")
		return Token{Kind: KindError, Lit: "-", Pos: start}
	default:
		l.advance()
		l.errorf(start, "unexpected character %q", string(r))
		return Token{Kind: KindError, Lit: string(r), Pos: start}
	}
}

// scanIdent reads an identifier starting at start and classifies it as a
// keyword or KindIdent.
func (l *Lexer) scanIdent(start Pos) Token {
	begin := l.offset
	l.advance() // first rune (already validated as an ident start)
	for {
		r, w := l.peek()
		if w == 0 || !isIdentPart(r) {
			break
		}
		l.advance()
	}
	lit := l.src[begin:l.offset]
	if kind, ok := keywords[lit]; ok {
		return Token{Kind: kind, Lit: lit, Pos: start}
	}
	return Token{Kind: KindIdent, Lit: lit, Pos: start}
}

// skipTrivia consumes whitespace (including newlines) and // line comments.
func (l *Lexer) skipTrivia() {
	for {
		r, w := l.peek()
		if w == 0 {
			return
		}
		switch {
		case r == ' ' || r == '\t' || r == '\r' || r == '\n':
			l.advance()
		case r == '/':
			// Look ahead for a second '/'. A single slash is a real token.
			if l.offset+1 < len(l.src) && l.src[l.offset+1] == '/' {
				l.skipLineComment()
				continue
			}
			return
		default:
			return
		}
	}
}

// skipLineComment consumes a // comment through the end of the line (the
// terminating newline is left for skipTrivia to count).
func (l *Lexer) skipLineComment() {
	for {
		r, w := l.peek()
		if w == 0 || r == '\n' {
			return
		}
		l.advance()
	}
}

func (l *Lexer) errorf(pos Pos, format string, args ...any) {
	l.diags = append(l.diags, diagf(pos, format, args...))
}

// isIdentStart reports whether r may begin an identifier.
func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isIdentPart reports whether r may continue an identifier. Hyphens and digits
// are allowed after the first rune so guarded-writes and gpt-5 lex as one token.
func isIdentPart(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9') || r == '-'
}
