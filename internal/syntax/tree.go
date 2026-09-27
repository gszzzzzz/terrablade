package syntax

// NodeKind identifies a grammatical structure.
//
// The comment on each kind lists its children in source order. "Expression"
// means a node of any expression kind, or an empty ErrorNode where the operand
// is missing. Trivia tokens may appear between the listed children; only File
// and Body begin or end with trivia. An "optional" child may be absent in valid
// source; a "missing" one is absent only after recovery, which reports a
// diagnostic.
type NodeKind uint8

const (
	// InvalidNode is the kind of a zero Node. Parse never produces it.
	InvalidNode NodeKind = iota
	// File: optional BOM, Body, EOF. After NestingLimitExceeded, trivia and an
	// ErrorNode holding the unparsed remainder may follow Body.
	File
	// ErrorNode: zero or more raw tokens and no nodes. It is empty where an
	// operand was expected but missing, and otherwise holds the malformed
	// material up to the recovery boundary its producer chose.
	ErrorNode
	// LiteralExpression: one Number token, or one Identifier spelled true,
	// false, or null.
	LiteralExpression
	// VariableExpression: one Identifier token.
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
	// both missing when no separator was found. A bare VariableExpression key
	// denotes a literal key; a parenthesized key is computed.
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
	// Body: zero or more Attribute, Block, and ErrorNode children, and the
	// trivia around them, including the newline that ends each item.
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

// Element is a child of a Node: either a Node or a Token. The zero Element is
// neither.
type Element struct {
	arena *arena
	ref   elementRef
}

// Node returns e as a Node, reporting whether it is one.
func (e Element) Node() (Node, bool) {
	if e.arena == nil || e.ref <= 0 {
		return Node{}, false
	}
	return Node{arena: e.arena, index: int(e.ref) - 1}, true
}

// Token returns e as a Token, reporting whether it is one.
func (e Element) Token() (Token, bool) {
	if e.arena == nil || e.ref >= 0 {
		return Token{}, false
	}
	return e.arena.tokens[-int(e.ref)-1], true
}

// Span returns the source bytes the element covers.
func (e Element) Span() Span {
	if node, ok := e.Node(); ok {
		return node.Span()
	}
	token, _ := e.Token()
	return token.Span()
}

// Node is an immutable handle to a node in a parsed tree; it keeps the tree
// alive. The zero Node has kind InvalidNode and no children.
type Node struct {
	arena *arena
	index int
}

func (n Node) record() nodeRecord {
	if n.arena == nil {
		return nodeRecord{}
	}
	return n.arena.nodes[n.index]
}

// Kind returns the node's kind.
func (n Node) Kind() NodeKind { return n.record().kind }

// Span returns the source bytes covered by the node's children.
func (n Node) Span() Span { return n.record().span }

// ChildCount returns the number of immediate children.
func (n Node) ChildCount() int { return n.record().childCount }

// Child returns the child at index, which must be in [0, ChildCount()).
func (n Node) Child(index int) Element {
	record := n.record()
	if index < 0 || index >= record.childCount {
		panic("syntax: child index out of range")
	}
	return Element{arena: n.arena, ref: n.arena.children[record.firstChild+index]}
}

// Element returns n as an Element.
func (n Node) Element() Element {
	if n.arena == nil {
		return Element{}
	}
	return Element{arena: n.arena, ref: elementRef(n.index + 1)}
}

// An elementRef is a node index plus one, or a negated token index minus one,
// so that zero is invalid.
type elementRef int

type nodeRecord struct {
	kind       NodeKind
	span       Span
	firstChild int
	childCount int
}

// An arena stores a whole tree. Only the parser writes to it.
type arena struct {
	nodes    []nodeRecord
	tokens   []Token
	children []elementRef
}

// Token is a leaf of the tree. Use Result.Text to read its source.
type Token struct {
	kind TokenKind
	span Span
}

// Kind returns the token's kind.
func (t Token) Kind() TokenKind { return t.kind }

// Span returns the token's source bytes. Of the tokens Parse produces, only
// EOF is empty.
func (t Token) Span() Span { return t.span }
