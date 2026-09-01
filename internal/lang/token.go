// Package lang implements the lexer, parser, and typed AST for the .agent
// surface syntax fixed by ADR 002 (docs/adr/002-language-frontend-and-ir-expressiveness.md).
//
// Scope is parsing (this package), resource lowering (#197,
// internal/lang/lower), type and effect checking (#198, internal/lang/check),
// and — added in #199 — conditionals, loops, and dynamic fan-out with the
// boolean expression language they require, lowered to the execution IR
// (internal/execir). Every AST node carries a spec.Pos so positions are
// compatible with the IR positions threaded by #187, and the parser recovers
// from errors to report multiple diagnostics per file rather than stopping at
// the first.
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

	// Structural keywords. These always begin a construct and never serve as a
	// name in the ADR 002 surface, so they are reserved. The field words
	// (model, policy, grants, input, output, effects) are NOT reserved because
	// they double as parameter names; the parser treats them contextually. The
	// loop keyword `in` (as in `for x in coll`) is likewise contextual — it is
	// lexed as an ordinary identifier and matched by the parser only in loop
	// position, so a parameter may still be named `in`. `true`/`false` are
	// contextual boolean literals recognized by the expression parser (#199),
	// not reserved words.
	KindAgent    // agent
	KindWorkflow // workflow
	KindParallel // parallel
	KindReturn   // return
	KindIf       // if
	KindElse     // else
	KindFor      // for
	KindWhile    // while

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

	// Comparison, logical, and grouping operators for the expression language
	// that conditionals and loops require (#199). The surface has no arithmetic;
	// these appear only in `if` conditions and other boolean positions.
	KindEqEq   // ==
	KindBangEq // !=
	KindLt     // <
	KindLte    // <=
	KindGt     // >
	KindGte    // >=
	KindAndAnd // &&
	KindOrOr   // ||
	KindBang   // !

	// Literals (#199): string and number literals usable in conditions and as
	// call arguments. For KindString, Token.Lit holds the DECODED string value
	// (escapes already applied); for KindNumber, Token.Lit holds the raw source
	// text, which the parser converts to an int64 or float64.
	KindString // "..."
	KindNumber // 123, 1.5
)

// keywords maps reserved lexemes to their token kind.
var keywords = map[string]Kind{
	"agent":    KindAgent,
	"workflow": KindWorkflow,
	"parallel": KindParallel,
	"return":   KindReturn,
	"if":       KindIf,
	"else":     KindElse,
	"for":      KindFor,
	"while":    KindWhile,
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
	case KindIf:
		return "'if'"
	case KindElse:
		return "'else'"
	case KindFor:
		return "'for'"
	case KindWhile:
		return "'while'"
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
	case KindEqEq:
		return "'=='"
	case KindBangEq:
		return "'!='"
	case KindLt:
		return "'<'"
	case KindLte:
		return "'<='"
	case KindGt:
		return "'>'"
	case KindGte:
		return "'>='"
	case KindAndAnd:
		return "'&&'"
	case KindOrOr:
		return "'||'"
	case KindBang:
		return "'!'"
	case KindString:
		return "string"
	case KindNumber:
		return "number"
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
