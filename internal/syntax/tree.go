package syntax

// NodeKind identifies a grammatical structure, separately from TokenKind.
// Its numeric value is not a stable storage format.
//
// The comment on each kind lists its children in source order. Tokens are
// named by their TokenKind, nodes by their NodeKind, and "expression" means a
// node of any expression kind or, where the operand is missing, an empty
// ErrorNode. Every node may also hold trivia tokens (Whitespace, Newline,
// LineComment, BlockComment) between the listed children; only File and Body
// begin or end with trivia. Two words distinguish absence: a child marked
// optional may be absent in valid source because the grammar allows it, while
// a child that is "missing" is absent only when recovery from malformed input
// ended the node early, and a diagnostic always accompanies that.
type NodeKind uint8

const (
	// InvalidNode is the kind of a zero SyntaxNode. Parse never produces it.
	InvalidNode NodeKind = iota
	// File: optional BOM, Body, EOF. After a NestingLimitExceeded diagnostic,
	// trivia, an ErrorNode holding the unparsed remainder, and further trivia
	// may appear between Body and EOF. The parseExpressionSource test seam puts
	// an expression in place of Body.
	File
	// ErrorNode: zero or more raw tokens and no nodes. It is empty where an
	// operand was expected but missing, and otherwise holds the malformed
	// material up to the recovery boundary its producer chose.
	ErrorNode
	// LiteralExpression: one Number token, or one Identifier spelled true,
	// false, or null.
	LiteralExpression
	// VariableExpression: one Identifier token. In an ObjectItem's key
	// position it denotes a literal key; see the package documentation.
	VariableExpression
	// ParenthesizedExpression: OpenParen, expression, CloseParen (possibly
	// missing).
	ParenthesizedExpression
	// UnaryExpression: a Minus or Bang token, then its operand expression.
	UnaryExpression
	// BinaryExpression: left expression, operator token, right expression.
	BinaryExpression
	// ConditionalExpression: condition expression, Question, true expression,
	// then Colon and false expression, both missing when the Colon was not
	// found.
	ConditionalExpression
	// FunctionCallExpression: Identifier, then zero or more DoubleColon and
	// Identifier pairs, OpenParen, arguments, CloseParen. Arguments are
	// expressions separated by Comma tokens, with an optional trailing Comma
	// or Ellipsis after the last one. Recovery can end the node after any
	// child and can place ErrorNode children among the arguments.
	FunctionCallExpression
	// TraversalExpression: an operand expression followed by one or more step
	// nodes: AttributeAccess, IndexAccess, LegacyIndexAccess, AttributeSplat,
	// FullSplat, or an ErrorNode for a stray Dot.
	TraversalExpression
	// AttributeAccess: Dot, Identifier.
	AttributeAccess
	// IndexAccess: OpenBracket, expression, CloseBracket (possibly missing).
	IndexAccess
	// LegacyIndexAccess: Dot, Number.
	LegacyIndexAccess
	// AttributeSplat: Dot, Star, then zero or more AttributeAccess or
	// LegacyIndexAccess steps, or an ErrorNode for a nested Dot Star or a
	// stray Dot. A bracket step ends the splat and follows it as a sibling.
	AttributeSplat
	// FullSplat: OpenBracket, Star, CloseBracket, then zero or more steps of
	// any kind nested inside. A missing CloseBracket also leaves out the steps.
	FullSplat
	// TupleExpression: OpenBracket, elements, CloseBracket (possibly missing).
	// Elements are expressions separated by Comma tokens with an optional
	// trailing Comma; recovery can place ErrorNode children among them.
	TupleExpression
	// ObjectExpression: OpenBrace, items, CloseBrace (possibly missing). Items
	// are ObjectItem nodes separated by Comma tokens or by newline trivia;
	// recovery can place ErrorNode children among them.
	ObjectExpression
	// ObjectItem: key expression, then Equal or Colon and value expression,
	// both missing when no separator was found.
	ObjectItem
	// ForExpression: OpenBracket or OpenBrace, Identifier "for", Identifier,
	// optional Comma and Identifier, Identifier "in", collection expression,
	// Colon, expression, optional Arrow and expression, optional Ellipsis,
	// optional Identifier "if" and expression, CloseBracket or CloseBrace.
	// Recovery can end the node early, place an ErrorNode before the closer,
	// or omit the closer.
	ForExpression
	// TemplateExpression: QuoteOpen or HeredocOpen, for a heredoc then
	// HeredocMarker, then content, then QuoteClose or HeredocEndMarker
	// (missing at EOF). Content is any sequence of TemplateText tokens and
	// TemplateInterpolation, TemplateIf, TemplateFor, and TemplateDirective
	// nodes, the last for a directive with no matching scope, plus ErrorNode
	// for a token the lexer should not have produced there.
	TemplateExpression
	// TemplateInterpolation: InterpolationOpen, optional StripMarker,
	// expression, optional StripMarker, TemplateSequenceEnd (possibly
	// missing); recovery can place an ErrorNode before the closer.
	TemplateInterpolation
	// TemplateDirective: DirectiveOpen, optional StripMarker, Identifier
	// keyword, the keyword's header, optional StripMarker, TemplateSequenceEnd
	// (possibly missing). The header of "if" is an expression; of "for" it is
	// Identifier, optional Comma and Identifier, Identifier "in", and an
	// expression; "else", "endif", and "endfor" have none. A missing or
	// unknown keyword and a malformed header can leave an ErrorNode in the
	// header's place.
	TemplateDirective
	// TemplateIf: the "if" TemplateDirective, content, optionally the "else"
	// TemplateDirective and more content, then the "endif" TemplateDirective,
	// missing when unmatched. Content is as in TemplateExpression.
	TemplateIf
	// TemplateFor: the "for" TemplateDirective, content, then the "endfor"
	// TemplateDirective, missing when unmatched.
	TemplateFor
	// Body: zero or more Attribute, Block, and ErrorNode children. Body owns
	// the trivia between and around its items, including the newline that
	// ends each item.
	Body
	// Attribute: Identifier name, Equal, value expression.
	Attribute
	// Block: Identifier type, zero or more BlockLabel nodes, OpenBrace, Body,
	// CloseBrace. A header that reaches no OpenBrace ends the node after its
	// last label; a body that reaches EOF omits the CloseBrace.
	Block
	// BlockLabel: one Identifier, or QuoteOpen, then any sequence of
	// TemplateText tokens and ErrorNode children (one per template sequence,
	// which labels forbid), then QuoteClose, missing at EOF.
	BlockLabel
	nodeKindCount
)

// SyntaxElement is a read-only handle to a node or token in source order.
// Copies share immutable tree storage. The zero value is neither a node nor a
// token and has an empty span. A handle keeps its tree storage alive.
type SyntaxElement struct {
	arena *syntaxArena
	ref   elementRef
}

// Node returns the node view, or the zero node and false for a token or zero
// element. Converting views does not allocate.
func (e SyntaxElement) Node() (SyntaxNode, bool) {
	if e.arena == nil || e.ref <= 0 {
		return SyntaxNode{}, false
	}
	return SyntaxNode{arena: e.arena, index: int(e.ref) - 1}, true
}

// Token returns the lexical value, or the zero token and false for a node or
// zero element. The returned value exposes no mutable tree storage.
func (e SyntaxElement) Token() (SyntaxToken, bool) {
	if e.arena == nil || e.ref >= 0 {
		return SyntaxToken{}, false
	}
	return e.arena.tokens[-int(e.ref)-1], true
}

// Span covers the original source bytes represented by the element.
func (e SyntaxElement) Span() Span {
	if node, ok := e.Node(); ok {
		return node.Span()
	}
	token, _ := e.Token()
	return token.Span()
}

// SyntaxNode is a read-only grammatical structure. Children mix nodes and tokens
// in source order. Trivia remains token children rather than node metadata.
// Copies share immutable tree storage; a handle keeps that storage alive. The
// zero node has InvalidNode kind, an empty span, and no children.
type SyntaxNode struct {
	arena *syntaxArena
	index int
}

func (n SyntaxNode) record() nodeRecord {
	if n.arena == nil {
		return nodeRecord{}
	}
	return n.arena.nodes[n.index]
}

// Kind identifies the node's grammatical structure.
func (n SyntaxNode) Kind() NodeKind { return n.record().kind }

// Span covers the node's children, including any trivia between them.
func (n SyntaxNode) Span() Span { return n.record().span }

// ChildCount returns the number of immediate children.
func (n SyntaxNode) ChildCount() int { return n.record().childCount }

// Child returns a read-only element in source order without allocating.
// An index outside [0, ChildCount()) panics, like ordinary slice indexing.
func (n SyntaxNode) Child(index int) SyntaxElement {
	record := n.record()
	if index < 0 || index >= record.childCount {
		panic("syntax: child index out of range")
	}
	return SyntaxElement{arena: n.arena, ref: n.arena.children[record.firstChild+index]}
}

// Element returns the generic view without allocating. A zero node produces a
// zero element.
func (n SyntaxNode) Element() SyntaxElement {
	if n.arena == nil {
		return SyntaxElement{}
	}
	return SyntaxElement{arena: n.arena, ref: elementRef(n.index + 1)}
}

// References use positive node indices and negative token indices, both offset
// by one so zero remains invalid. Indices survive growth of the backing slices.
// They never escape this package or imply a stable serialization format.
type elementRef int

type nodeRecord struct {
	kind       NodeKind
	span       Span
	firstChild int
	childCount int
}

// The parser is the sole writer; published handles only read these slices.
// Tokens reuse the lexer's storage rather than being copied into every parent.
type syntaxArena struct {
	nodes    []nodeRecord
	tokens   []SyntaxToken
	children []elementRef
}

// SyntaxToken is a read-only source leaf, including whitespace and comments.
// Its text is available through Result.Text(t.Span()); it owns no source bytes.
type SyntaxToken struct {
	kind TokenKind
	span Span
}

// Kind identifies the lexical element.
func (t SyntaxToken) Kind() TokenKind { return t.kind }

// Span identifies the original source bytes. Among tokens produced by Parse,
// only EOF has an empty span. A zero token also has an empty span.
func (t SyntaxToken) Span() Span { return t.span }
