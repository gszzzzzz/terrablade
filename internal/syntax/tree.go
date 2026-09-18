package syntax

// NodeKind identifies a grammatical structure, separately from lexical Kind.
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
	nodeKindCount
)

// SyntaxElement is a node or a token in source order. Trees produced by this
// package contain SyntaxNode and SyntaxToken values, never pointers or nil.
// The private marker identifies element implementations; embedding an existing
// implementation can also satisfy this interface outside the package.
type SyntaxElement interface {
	Span() Span
	syntaxElement()
}

// SyntaxNode is a read-only grammatical structure. Children mix nodes and tokens
// in source order. Trivia remains token children rather than node metadata.
// Values may be copied freely; no mutable tree storage is exposed.
type SyntaxNode struct {
	kind     NodeKind
	span     Span
	children []SyntaxElement
}

func (n SyntaxNode) syntaxElement() {}

// Kind identifies the node's grammatical structure.
func (n SyntaxNode) Kind() NodeKind { return n.kind }

// Span covers the node's children, including any trivia between them.
func (n SyntaxNode) Span() Span { return n.span }

// ChildCount returns the number of immediate children.
func (n SyntaxNode) ChildCount() int { return len(n.children) }

// Child returns a read-only element in source order without allocating.
// An index outside [0, ChildCount()) panics, like ordinary slice indexing.
func (n SyntaxNode) Child(index int) SyntaxElement { return n.children[index] }

// SyntaxToken is a read-only source leaf, including whitespace and comments.
// Its text is the file's source[Span().Start:Span().End]; it owns no source bytes.
type SyntaxToken struct {
	kind Kind
	span Span
}

func (t SyntaxToken) syntaxElement() {}

// Kind identifies the lexical element.
func (t SyntaxToken) Kind() Kind { return t.kind }

// Span identifies the original source bytes. Only EOF has an empty span.
func (t SyntaxToken) Span() Span { return t.span }

// syntaxFile owns a source snapshot and its lossless tree. It stays private
// until configuration-body parsing can validate a complete native HCL file.
type syntaxFile struct {
	source      string
	root        SyntaxNode
	diagnostics []Diagnostic
}
