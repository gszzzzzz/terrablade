package syntax

// tokenSet records parser boundaries without allocating or repeating token lists.
type tokenSet uint64

// Fail compilation when the token vocabulary no longer fits in this bit set.
const _ tokenSet = 1 << tokenKindCount

// has reports whether kind is a member of the set.
func (s tokenSet) has(kind TokenKind) bool { return s&(tokenSet(1)<<kind) != 0 }

const (
	closingDelimiters tokenSet = 1<<CloseParen | 1<<CloseBracket | 1<<CloseBrace |
		1<<QuoteClose | 1<<HeredocEndMarker | 1<<TemplateSequenceEnd

	// An incomplete expression must leave these for its enclosing production.
	expressionBoundaries = closingDelimiters | 1<<EOF | 1<<StripMarker

	// Inside a template sequence's malformed tail, ordinary expression closers
	// are stray material. Template closers and strip markers still belong outside.
	templateBoundaries = expressionBoundaries &^ (1<<CloseParen | 1<<CloseBracket | 1<<CloseBrace)

	lineSeparators = tokenSet(1)<<Newline | 1<<LineComment

	// Delimited lookahead skips line separators; line-sensitive object items stop
	// there. Calls and collections otherwise share the same recovery boundaries.
	itemBoundaries = expressionBoundaries | lineSeparators | 1<<Comma

	operandTerminators = itemBoundaries | 1<<Colon | 1<<Arrow | 1<<Ellipsis

	// A body item owns no following newline or containing block's closing brace.
	// Other expression closers are stray body material, not recovery boundaries.
	bodyItemBoundaries = lineSeparators | 1<<CloseBrace | 1<<EOF
)
