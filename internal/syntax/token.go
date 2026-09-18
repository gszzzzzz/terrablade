package syntax

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

// Span is a half-open byte interval [Start, End) in the original source.
type Span struct {
	Start int
	End   int
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
	ExpectedExpression
	UnexpectedToken
	ExpectedClosingParen
	ExpectedClosingBracket
	ExpectedConditionalColon
	ExpectedArgumentSeparator
	ExpectedAttributeName
	ExpectedFunctionName
	ExpectedOpeningParen
	InvalidLegacyIndex
	NestedAttributeSplat
	UnsupportedExpression
	NestingLimitExceeded
	InvalidNumber
	ExpectedTupleSeparator
	ExpectedClosingBrace
	ExpectedObjectValueSeparator
	ExpectedObjectItemSeparator
	ExpectedForVariable
	ExpectedForIn
	ExpectedForColon
	ExpectedForArrow
	UnexpectedForKey
	UnexpectedForGrouping
	diagnosticKindCount
)

// Diagnostic points to the source responsible for a lexical or syntax error.
// An error does not require an Invalid token: an unterminated comment, for
// example, retains its BlockComment kind so its source remains recognizable.
type Diagnostic struct {
	Kind DiagnosticKind
	Span Span
}

// lexResult contains tokens and lexical errors, in source order. An input with
// diagnostics must not be formatted. The token partition remains lossless.
type lexResult struct {
	Tokens      []SyntaxToken
	Diagnostics []Diagnostic
}
