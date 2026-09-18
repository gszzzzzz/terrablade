package syntax

// NodeKind identifies a grammatical structure, separately from TokenKind.
// Its numeric value is not a stable storage format.
type NodeKind uint8

const (
	InvalidNode NodeKind = iota
	File
	Error
	LiteralExpression
	VariableExpression
	ParenthesizedExpression
	UnaryExpression
	BinaryExpression
	ConditionalExpression
	FunctionCallExpression
	TraversalExpression
	AttributeAccess
	IndexAccess
	LegacyIndexAccess
	AttributeSplat
	FullSplat
	TupleExpression
	ObjectExpression
	ObjectItem
	ForExpression
	TemplateExpression
	TemplateInterpolation
	TemplateDirective
	TemplateIf
	TemplateFor
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
// Its text is the file's source[Span().Start:Span().End]; it owns no source bytes.
type SyntaxToken struct {
	kind TokenKind
	span Span
}

// Kind identifies the lexical element.
func (t SyntaxToken) Kind() TokenKind { return t.kind }

// Span identifies the original source bytes. Only EOF has an empty span.
func (t SyntaxToken) Span() Span { return t.span }

// syntaxFile owns a source snapshot and its lossless tree. It stays private
// until configuration-body parsing can validate a complete native HCL file.
type syntaxFile struct {
	source      string
	root        SyntaxNode
	diagnostics []Diagnostic
}
