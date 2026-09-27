package syntax

//go:generate go run golang.org/x/tools/cmd/stringer@v0.42.0 -type=TokenKind,NodeKind,DiagnosticKind -output=kind_string.go

// TokenKind identifies a lexical element. Its numeric value is not a stable format.
type TokenKind uint8

const (
	Invalid TokenKind = iota
	EOF
	// BOM is outside native HCL syntax, but Terraform/OpenTofu/HCL tooling may
	// accept or strip a leading BOM. This token preserves those input bytes;
	// placement acceptance belongs to parser policy, and canonical omission or
	// preservation to printer policy. Comments and template text keep it as content.
	BOM
	Whitespace
	Newline
	LineComment
	BlockComment
	Identifier
	// Number preserves an upstream-compatible numeric candidate. Malformed
	// candidates such as 1.0.2 remain one token for the parser to diagnose.
	Number
	OpenBrace
	CloseBrace
	OpenBracket
	CloseBracket
	OpenParen
	CloseParen
	Plus
	Minus
	Star
	Slash
	Percent
	And
	Or
	Bang
	Equal
	EqualEqual
	NotEqual
	Less
	LessEqual
	Greater
	GreaterEqual
	Arrow
	Colon
	DoubleColon
	Question
	Dot
	Ellipsis
	Comma
	QuoteOpen
	QuoteClose
	// TemplateText preserves literal bytes and escapes without decoding them.
	TemplateText
	InterpolationOpen
	DirectiveOpen
	// StripMarker is a separate '~' adjacent to a template sequence boundary.
	StripMarker
	TemplateSequenceEnd
	// HeredocOpen covers << or <<-; HeredocMarker and Newline follow separately.
	HeredocOpen
	HeredocMarker
	// HeredocEndMarker includes the closing line's indentation and trailing
	// whitespace; its LF/CRLF is a separate Newline token.
	HeredocEndMarker
	tokenKindCount
)

// IsTrivia reports whether k carries no syntax of its own: whitespace, a
// newline, or a comment.
func (k TokenKind) IsTrivia() bool { return classifyTrivia(k) != notTrivia }

// IsComment reports whether k is a line or block comment.
func (k TokenKind) IsComment() bool { return k == LineComment || k == BlockComment }

// Span is a half-open byte interval [Start, End) in the original source.
type Span struct {
	Start int
	End   int
}

// lexResult contains tokens and lexical errors, in source order. An input with
// diagnostics must not be formatted. The token partition remains lossless.
type lexResult struct {
	tokens      []Token
	diagnostics []Diagnostic
}
