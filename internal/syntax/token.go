// Package syntax tokenizes native HCL while preserving every source byte.
//
// Tokens refer to half-open byte spans in the caller's source. Lex does not
// retain or modify that source. Its non-EOF tokens partition the complete input,
// including malformed UTF-8 and trivia. A final zero-width EOF marks its end.
// Diagnostics describe lexical errors only; a successful Lex is not a syntax
// validation. Keywords remain identifiers for the parser to interpret.
package syntax

// Kind identifies a lexical element. Its numeric value is not a stable format.
type Kind uint8

const (
	Invalid Kind = iota
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
)

// Span is a half-open byte interval [Start, End) in the original source.
type Span struct {
	Start int
	End   int
}

// Token owns no source bytes. Its text is source[Span.Start:Span.End].
type Token struct {
	Kind Kind
	Span Span
}

// DiagnosticKind identifies an error without coupling lexing to presentation.
type DiagnosticKind uint8

const (
	InvalidUTF8 DiagnosticKind = iota
	InvalidCharacter
	UnterminatedBlockComment
	UnterminatedQuotedTemplate
	UnterminatedTemplateSequence
	InvalidEscape
	NewlineInQuotedTemplate
	UnterminatedHeredoc
)

// Diagnostic points to the source responsible for a lexical error.
// An error does not require an Invalid token: an unterminated comment, for
// example, retains its BlockComment kind so its source remains recognizable.
type Diagnostic struct {
	Kind DiagnosticKind
	Span Span
}

// Result contains tokens and lexical errors, in source order. An input with
// diagnostics must not be formatted. The token partition remains lossless.
type Result struct {
	Tokens      []Token
	Diagnostics []Diagnostic
}
