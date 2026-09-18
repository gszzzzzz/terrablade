package lowering

import "terrablade/internal/syntax"

// expressionView is a private, immutable syntax overlay. Original tokens keep
// their source spans; only canonical delimiters are synthesized. The lossless
// CST and the public lowering interface never expose these rewrite decisions.
type expressionView struct {
	kind                syntax.NodeKind
	children            []expressionElement
	needsNewlineContext bool
	endsHeredoc         bool
}

type expressionElement struct {
	node  *expressionView
	token expressionToken
}

type expressionToken struct {
	source syntax.SyntaxToken
	kind   syntax.TokenKind
	text   string // Nonempty only for a synthesized delimiter.
}

func (n *expressionView) Kind() syntax.NodeKind            { return n.kind }
func (n *expressionView) ChildCount() int                  { return len(n.children) }
func (n *expressionView) Child(i int) expressionElement    { return n.children[i] }
func (e expressionElement) Node() (*expressionView, bool)  { return e.node, e.node != nil }
func (e expressionElement) Token() (expressionToken, bool) { return e.token, e.node == nil }
func (t expressionToken) Kind() syntax.TokenKind           { return t.kind }
func (t expressionToken) Span() syntax.Span                { return t.source.Span() }
func (t expressionToken) spelling(result syntax.Result) string {
	if t.text != "" {
		return t.text
	}
	return result.Text(t.source.Span())
}

func delimiter(kind syntax.TokenKind, text string) expressionElement {
	return expressionElement{token: expressionToken{kind: kind, text: text}}
}

// normalizeExpression visits each source element once in postorder. An alias
// carries its removed wrapper's grammar role to the immediate parent, which can
// then protect precedence without rescanning a deep expression chain.
func normalizeExpression(result syntax.Result, root syntax.SyntaxNode) *expressionView {
	type rewrite struct {
		node      *expressionView
		unwrapped bool
	}
	type frame struct {
		node                syntax.SyntaxNode
		next                int
		attributeProjection bool
	}
	stack := []frame{{node: root}}
	views := make(map[syntax.SyntaxNode]rewrite)
	for len(stack) != 0 {
		current := &stack[len(stack)-1]
		if current.next < current.node.ChildCount() {
			element := current.node.Child(current.next)
			current.next++
			if child, ok := element.Node(); ok {
				stack = append(stack, frame{node: child, attributeProjection: current.node.Kind() == syntax.AttributeSplat})
			}
			continue
		}
		node := current.node
		view := &expressionView{kind: node.Kind(), children: make([]expressionElement, 0, node.ChildCount())}
		position := 0
		for i := range node.ChildCount() {
			if child, ok := node.Child(i).Node(); ok {
				rewritten := views[child]
				if rewritten.unwrapped && needsGrouping(result, node, position, rewritten.node) {
					rewritten.node = groupedExpression(rewritten.node, nil, nil)
				}
				if node.Kind() == syntax.ObjectItem {
					rewritten.node = protectExpressionLines(rewritten.node)
				}
				view.children = append(view.children, expressionElement{node: rewritten.node})
				position++
			} else if token, ok := node.Child(i).Token(); ok {
				view.children = append(view.children, expressionElement{token: expressionToken{source: token, kind: token.Kind()}})
			}
		}
		view.needsNewlineContext = exposedExpressionLines(view)
		view.endsHeredoc = expressionEndsHeredoc(view)
		rewritten := rewrite{node: view}
		if inner, before, after := quotedWrapper(view); inner != nil {
			rewritten = rewrite{node: inner, unwrapped: true}
			if hasComments(before) || hasComments(after) {
				// Delimiter comments need a stable home after both template edges
				// disappear. Parentheses also make every line comment newline legal.
				rewritten = rewrite{node: groupedExpression(inner, before, after)}
			}
		} else if node.Kind() == syntax.LegacyIndexAccess && !current.attributeProjection {
			// Brackets inside .* would index the projected tuple instead of each
			// element. Keep those dot steps until splats can be modernized together.
			view.kind = syntax.IndexAccess
			view.children[0] = delimiter(syntax.OpenBracket, "[")
			last := len(view.children) - 1
			view.children[last] = expressionElement{node: &expressionView{kind: syntax.LiteralExpression, children: []expressionElement{view.children[last]}}}
			view.children = append(view.children, delimiter(syntax.CloseBracket, "]"))
			view.needsNewlineContext = false
		}
		views[node] = rewritten
		stack = stack[:len(stack)-1]
	}
	return protectExpressionLines(views[root].node)
}

// Removing an interpolation also removes its newline-safe grammar context.
// Binary and conditional layouts already enclose mandatory continuation lines;
// unary and traversal expressions need a permanent delimiter when exposed at an
// attribute value or object key/value. Delimited descendants own their lines.
func protectExpressionLines(node *expressionView) *expressionView {
	if node.needsNewlineContext && (node.kind == syntax.UnaryExpression || node.kind == syntax.TraversalExpression) {
		return groupedExpression(node, nil, nil)
	}
	return node
}

func exposedExpressionLines(node *expressionView) bool {
	switch node.kind {
	case syntax.UnaryExpression, syntax.BinaryExpression, syntax.ConditionalExpression,
		syntax.TraversalExpression, syntax.AttributeAccess, syntax.LegacyIndexAccess,
		syntax.AttributeSplat, syntax.FullSplat:
	default:
		return false
	}
	comment, newline, heredoc := false, false, false
	for _, element := range node.children {
		if element.node != nil {
			if heredoc || element.node.needsNewlineContext {
				return true
			}
			heredoc = element.node.endsHeredoc
			comment, newline = false, false
			continue
		}
		switch element.token.kind {
		case syntax.LineComment:
			return true
		case syntax.BlockComment:
			comment = true
		case syntax.Newline:
			newline = true
		case syntax.Whitespace:
		default:
			if heredoc {
				return true
			}
			comment, newline = false, false
		}
		if comment && newline {
			return true
		}
	}
	return false
}

func expressionEndsHeredoc(node *expressionView) bool {
	if node.kind == syntax.TemplateExpression {
		return node.children[0].token.kind == syntax.HeredocOpen
	}
	for i := len(node.children) - 1; i >= 0; i-- {
		child := node.children[i]
		if child.node != nil {
			return child.node.endsHeredoc
		}
		if !bodyTrivia(child.token.kind) {
			return false
		}
	}
	return false
}

// A quoted TemplateWrapExpr returns its inner value without string conversion.
// Literal text (even whitespace), directives, and heredocs are not wrappers.
func quotedWrapper(node *expressionView) (inner *expressionView, before, after []expressionElement) {
	if node.kind != syntax.TemplateExpression || len(node.children) != 3 ||
		node.children[0].token.kind != syntax.QuoteOpen || node.children[2].token.kind != syntax.QuoteClose {
		return nil, nil, nil
	}
	sequence := node.children[1].node
	if sequence == nil || sequence.kind != syntax.TemplateInterpolation {
		return nil, nil, nil
	}
	for _, element := range sequence.children {
		if element.node != nil {
			inner = element.node
		} else if bodyTrivia(element.token.kind) {
			if inner == nil {
				before = append(before, element)
			} else {
				after = append(after, element)
			}
		}
	}
	return inner, before, after
}

func hasComments(elements []expressionElement) bool {
	for _, element := range elements {
		if element.token.kind == syntax.LineComment || element.token.kind == syntax.BlockComment {
			return true
		}
	}
	return false
}

func groupedExpression(inner *expressionView, before, after []expressionElement) *expressionView {
	children := make([]expressionElement, 0, len(before)+len(after)+3)
	children = append(children, delimiter(syntax.OpenParen, "("))
	children = append(children, before...)
	children = append(children, expressionElement{node: inner})
	children = append(children, after...)
	children = append(children, delimiter(syntax.CloseParen, ")"))
	return &expressionView{kind: syntax.ParenthesizedExpression, children: children}
}

func needsGrouping(result syntax.Result, parent syntax.SyntaxNode, position int, child *expressionView) bool {
	if child.kind == syntax.ParenthesizedExpression {
		return false
	}
	switch parent.Kind() {
	case syntax.ObjectItem:
		// Bare identifiers (including true/null) are literal keys in HCL.
		return position == 0
	case syntax.UnaryExpression:
		return expressionPower(child) < 7
	case syntax.BinaryExpression:
		power := 0
		for i := range parent.ChildCount() {
			if token, ok := parent.Child(i).Token(); ok && !bodyTrivia(token.Kind()) {
				power = binaryPower(token.Kind())
				break
			}
		}
		return expressionPower(child) < power || position == 1 && expressionPower(child) == power
	case syntax.ConditionalExpression:
		return position == 0 && child.kind == syntax.ConditionalExpression
	case syntax.TraversalExpression:
		if position != 0 {
			return false
		}
		if expressionPower(child) < 8 {
			return true
		}
		if child.kind == syntax.TraversalExpression {
			// Appending a step to an exposed splat would extend its projection.
			last := child.children[len(child.children)-1].node
			return last.kind == syntax.FullSplat || last.kind == syntax.AttributeSplat
		}
	case syntax.TupleExpression:
		// The first bare `for` selects comprehension grammar, not a variable.
		return position == 0 && child.kind == syntax.VariableExpression && child.children[0].token.spelling(result) == "for"
	}
	return false
}

func expressionPower(node *expressionView) int {
	switch node.kind {
	case syntax.ConditionalExpression:
		return 0
	case syntax.BinaryExpression:
		for _, element := range node.children {
			if element.node == nil && !bodyTrivia(element.token.kind) {
				return binaryPower(element.token.kind)
			}
		}
	case syntax.UnaryExpression:
		return 7
	case syntax.TraversalExpression:
		return 8
	}
	return 9
}
