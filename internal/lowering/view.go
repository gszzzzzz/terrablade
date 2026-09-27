package lowering

import "github.com/gszzzzzz/terrablade/internal/syntax"

// expressionView is a normalized, immutable copy of an expression's syntax.
// Source tokens keep their spans; only delimiters are synthesized. Its
// accessors mirror syntax.Node.
type expressionView struct {
	kind        syntax.NodeKind
	children    []expressionElement
	exposedLine bool // See hasExposedLine.
	endsHeredoc bool // See expressionEndsHeredoc.
}

// expressionElement is one child of a view: a nested view when node is set,
// otherwise a token.
type expressionElement struct {
	node  *expressionView
	token expressionToken
}

// expressionToken is a source token or a synthesized delimiter.
type expressionToken struct {
	source syntax.Token
	kind   syntax.TokenKind
	text   string // Nonempty only for a synthesized delimiter.
}

func (n *expressionView) Kind() syntax.NodeKind            { return n.kind }
func (n *expressionView) ChildCount() int                  { return len(n.children) }
func (n *expressionView) Child(i int) expressionElement    { return n.children[i] }
func (e expressionElement) Node() (*expressionView, bool)  { return e.node, e.node != nil }
func (e expressionElement) Token() (expressionToken, bool) { return e.token, e.node == nil }
func (t expressionToken) Kind() syntax.TokenKind           { return t.kind }

func (t expressionToken) spelling(result syntax.Result) string {
	if t.text != "" {
		return t.text
	}
	return result.Text(t.source.Span())
}

// delimiter synthesizes a punctuation token that has no source span.
func delimiter(kind syntax.TokenKind, text string) expressionElement {
	return expressionElement{token: expressionToken{kind: kind, text: text}}
}
