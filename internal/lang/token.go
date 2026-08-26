// Package lang implements the lexer, parser, and typed AST for the .agent
// surface syntax fixed by ADR 002 (docs/adr/002-language-frontend-and-ir-expressiveness.md).
//
// Scope is parsing only: no lowering to the resource model (#197), no type or
// effect checking (#198), and no conditionals, loops, or dynamic fan-out (#199).
// Every AST node carries a spec.Pos so positions are compatible with the IR
// positions threaded by #187, and the parser recovers from errors to report
// multiple diagnostics per file rather than stopping at the first.
package lang

import "fmt"

// Kind enumerates the lexical token classes of the .agent language.
type Kind int

const (
	// KindError is a lexer-level malformed token (e.g. a stray rune). The
	// offending text is carried in Token.Lit and a diagnostic is emitted.
	KindError Kind = iota
	// KindEOF marks the end of input. The lexer always terminates the stream
	// with exactly one KindEOF token.
	KindEOF

	// KindIdent is a bare identifier: [A-Za-z_][A-Za-z0-9_-]*. Hyphens are
	// permitted after the first rune so DNS-style resource references
	// (guarded-writes) and model name segments (gpt-5) are single tokens; the
	// language has no arithmetic, so '-' is never an operator.
	KindIdent

	// Structural keywords. These four always begin a construct and never serve
	// as a name in the ADR 002 surface, so they are reserved. The field words
	// (model, policy, grants, input, output, effects) are NOT reserved because
	// they double as parameter names; the parser treats them contextually.
	KindAgent    // agent
	KindWorkflow // workflow
	KindParallel // parallel
	KindReturn   // return

	// Punctuation.
	KindLBrace // {
	KindRBrace // }
	KindLParen // (
	KindRParen // )
	KindDot    // .
	KindSlash  // /
	KindComma  // ,
	KindColon  // :
	KindEquals // =
	KindArrow  // ->
)

// keywords maps reserved lexemes to their token kind.
var keywords = map[string]Kind{
	"agent":    KindAgent,
	"workflow": KindWorkflow,
	"parallel": KindParallel,
	"return":   KindReturn,
}

// String renders a Kind for diagnostics and tests.
func (k Kind) String() string {
	switch k {
	case KindError:
		return "error"
	case KindEOF:
		return "EOF"
	case KindIdent:
		return "identifier"
	case KindAgent:
		return "'agent'"
	case KindWorkflow:
		return "'workflow'"
	case KindParallel:
		return "'parallel'"
	case KindReturn:
		return "'return'"
	case KindLBrace:
		return "'{'"
	case KindRBrace:
		return "'}'"
	case KindLParen:
		return "'('"
	case KindRParen:
		return "')'"
	case KindDot:
		return "'.'"
	case KindSlash:
		return "'/'"
	case KindComma:
		return "','"
	case KindColon:
		return "':'"
	case KindEquals:
		return "'='"
	case KindArrow:
		return "'->'"
	default:
		return fmt.Sprintf("Kind(%d)", int(k))
	}
}

// Token is one lexeme with its class, source text, and position.
type Token struct {
	Kind Kind
	// Lit is the source text of the token. For punctuation it is the literal
	// symbol; for KindIdent and keywords it is the matched identifier; for
	// KindEOF it is empty.
	Lit string
	// Pos is the 1-based start position of the token (spec.Pos, #187).
	Pos Pos
}

// String renders a token for test output and diagnostics.
func (t Token) String() string {
	if t.Lit != "" {
		return fmt.Sprintf("%s(%q)", t.Kind, t.Lit)
	}
	return t.Kind.String()
}
