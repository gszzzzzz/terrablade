package lowering

import "github.com/gszzzzzz/terrablade/internal/syntax"

// expressionView is a private, immutable syntax overlay. Original tokens keep
// their source spans; only canonical delimiters are synthesized. The lossless
// CST and the public lowering interface never expose these rewrite decisions.
// Its accessors mirror syntax.Node so the layout code reads a view the
// same way it would read the CST.
type expressionView struct {
	kind     syntax.NodeKind
	children []expressionElement
	// exposedLine reports that this node exposes a mandatory line
	// break without a delimiter of its own (hasExposedLine). A parent
	// object item or the root then wraps unary and traversal nodes in
	// parentheses (protectExposedLine).
	exposedLine bool
	// endsHeredoc reports that the node's last significant token is a
	// heredoc marker (expressionEndsHeredoc).
	endsHeredoc bool
}

// expressionElement is one child of a view: a nested view when node is set,
// otherwise a token.
type expressionElement struct {
	node  *expressionView
	token expressionToken
}

// expressionToken is a view token. A source token keeps its span so comments
// and spellings are read from the original text; a synthesized delimiter has
// no source and carries its spelling in text.
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
