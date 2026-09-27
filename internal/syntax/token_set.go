package syntax

// tokenSet is a set of token kinds.
type tokenSet uint64

// Fail to compile if TokenKind outgrows tokenSet.
const _ tokenSet = 1 << tokenKindCount

func (s tokenSet) has(kind TokenKind) bool { return s&(tokenSet(1)<<kind) != 0 }

const (
	closingDelimiters tokenSet = 1<<CloseParen | 1<<CloseBracket | 1<<CloseBrace |
		1<<QuoteClose | 1<<HeredocEndMarker | 1<<TemplateSequenceEnd

	// An incomplete expression must leave these for its enclosing production.
	expressionBoundaries = closingDelimiters | 1<<EOF | 1<<StripMarker

	// Recovery inside a template sequence skips ordinary closers.
	templateBoundaries = expressionBoundaries &^ (1<<CloseParen | 1<<CloseBracket | 1<<CloseBrace)

	lineSeparators = tokenSet(1)<<Newline | 1<<LineComment

	itemBoundaries = expressionBoundaries | lineSeparators | 1<<Comma

	operandTerminators = itemBoundaries | 1<<Colon | 1<<Arrow | 1<<Ellipsis

	// Other closers are stray material in a body.
	bodyItemBoundaries = lineSeparators | 1<<CloseBrace | 1<<EOF
)
