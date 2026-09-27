package lowering

import "github.com/gszzzzzz/terrablade/internal/syntax"

// normalizeExpression rewrites the expression at root into a view that
// replaces quoted "${x}" wrappers by x and legacy numeric steps a.0 by a[0].
func normalizeExpression(result syntax.Result, root syntax.Node) *expressionView {
	return protectExposedLine(postOrder(normalizationWalker{result}, root, false).node)
}

// normalizationWalker rewrites source nodes into views. Its context marks a
// direct child of an attribute splat.
type normalizationWalker struct{ result syntax.Result }

func (normalizationWalker) expand(node syntax.Node, _ bool, children []visit[syntax.Node, bool]) []visit[syntax.Node, bool] {
	for i := range node.ChildCount() {
		if child, ok := node.Child(i).Node(); ok {
			children = append(children, visit[syntax.Node, bool]{child, node.Kind() == syntax.AttributeSplat})
		}
	}
	return children
}

func (w normalizationWalker) lower(node syntax.Node, attributeProjection bool, children []rewrite) rewrite {
	return rewriteView(normalizedView(w.result, node, children), attributeProjection)
}

// rewrite is one normalized node. unwrapped tells the parent that the node
// lost the "${ }" delimiters that made it an atom.
type rewrite struct {
	node      *expressionView
	unwrapped bool
}

// normalizedView copies node into a view over its normalized children,
// adding parentheses where an unwrapped child needs them.
func normalizedView(result syntax.Result, node syntax.Node, children []rewrite) *expressionView {
	view := &expressionView{kind: node.Kind(), children: make([]expressionElement, 0, node.ChildCount())}
	position := 0
	for i := range node.ChildCount() {
		if token, ok := node.Child(i).Token(); ok {
			view.children = append(view.children, expressionElement{token: expressionToken{source: token, kind: token.Kind()}})
			continue
		}
		child := children[position]
		if child.unwrapped && needsGrouping(result, node, position, child.node) {
			child.node = permanentParentheses(child.node, nil, nil)
		}
		if node.Kind() == syntax.ObjectItem {
			child.node = protectExposedLine(child.node)
		}
		view.children = append(view.children, expressionElement{node: child.node})
		position++
	}
	view.exposedLine = hasExposedLine(view)
	view.endsHeredoc = expressionEndsHeredoc(view)
	return view
}

// rewriteView removes a quoted interpolation-only wrapper or converts a legacy
// index step to brackets. Other views are returned unchanged.
func rewriteView(view *expressionView, attributeProjection bool) rewrite {
	if inner, before, after := quotedWrapper(view); inner != nil {
		if hasComments(before) || hasComments(after) {
			// Keep the comments inside parentheses, where newlines are legal.
			return rewrite{node: permanentParentheses(inner, before, after)}
		}
		return rewrite{node: inner, unwrapped: true}
	}
	if view.kind == syntax.LegacyIndexAccess && !attributeProjection {
		// Keep a.*.0: a.*[0] would index the projected tuple instead.
		view.kind = syntax.IndexAccess
		view.children[0] = delimiter(syntax.OpenBracket, "[")
		last := len(view.children) - 1
		view.children[last] = expressionElement{node: &expressionView{kind: syntax.LiteralExpression, children: []expressionElement{view.children[last]}}}
		view.children = append(view.children, delimiter(syntax.CloseBracket, "]"))
		view.exposedLine = false
	}
	return rewrite{node: view}
}

// protectExposedLine parenthesizes a unary or traversal node that exposes a
// mandatory line break. Binary and conditional layouts add their own
// parentheses when they break.
func protectExposedLine(node *expressionView) *expressionView {
	if node.exposedLine && (node.kind == syntax.UnaryExpression || node.kind == syntax.TraversalExpression) {
		return permanentParentheses(node, nil, nil)
	}
	return node
}

// hasExposedLine reports whether an undelimited node contains a mandatory
// line break outside any delimiters: a line comment, a block comment and a
// newline, or a heredoc followed by more of the node. Once its "${ }" wrapper
// is removed, such a node needs parentheses where the grammar forbids
// newlines.
func hasExposedLine(node *expressionView) bool {
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
			if heredoc || element.node.exposedLine {
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

// expressionEndsHeredoc reports whether node's last significant element is a
// heredoc end marker.
func expressionEndsHeredoc(node *expressionView) bool {
	if node.kind == syntax.TemplateExpression {
		return node.children[0].token.kind == syntax.HeredocOpen
	}
	for i := len(node.children) - 1; i >= 0; i-- {
		child := node.children[i]
		if child.node != nil {
			return child.node.endsHeredoc
		}
		if !child.token.kind.IsTrivia() {
			return false
		}
	}
	return false
}

// quotedWrapper recognizes a quoted template holding only one interpolation,
// which evaluates to its inner value unconverted, and returns that inner
// expression with the trivia on either side.
func quotedWrapper(node *expressionView) (inner *expressionView, before, after []expressionElement) {
	if node.kind != syntax.TemplateExpression || len(node.children) != 3 ||
		node.children[0].token.kind != syntax.QuoteOpen || node.children[2].token.kind != syntax.QuoteClose {
		return nil, nil, nil
	}
	interpolation := node.children[1].node
	if interpolation == nil || interpolation.kind != syntax.TemplateInterpolation {
		return nil, nil, nil
	}

	for _, element := range interpolation.children {
		if element.node != nil {
			inner = element.node
		} else if element.token.kind.IsTrivia() {
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
		if element.token.kind.IsComment() {
			return true
		}
	}
	return false
}

// permanentParentheses wraps inner, and the trivia around it, in
// parentheses.
func permanentParentheses(inner *expressionView, before, after []expressionElement) *expressionView {
	children := make([]expressionElement, 0, len(before)+len(after)+3)
	children = append(children, delimiter(syntax.OpenParen, "("))
	children = append(children, before...)
	children = append(children, expressionElement{node: inner})
	children = append(children, after...)
	children = append(children, delimiter(syntax.CloseParen, ")"))
	return &expressionView{kind: syntax.ParenthesizedExpression, children: children}
}

// needsGrouping reports whether an unwrapped child of parent needs
// parentheses to keep the meaning its "${ }" wrapper gave it. position is the
// child's index among parent's child nodes.
func needsGrouping(result syntax.Result, parent syntax.Node, position int, child *expressionView) bool {
	if child.kind == syntax.ParenthesizedExpression {
		return false
	}

	switch parent.Kind() {
	case syntax.ObjectItem:
		// Bare identifiers (including true/null) change meaning as keys, but
		// a literal quoted key already expresses its intent unambiguously.
		return position == 0 && !literalQuotedTemplate(child)
	case syntax.UnaryExpression:
		// -(a + b) would otherwise read back as (-a) + b.
		return expressionPower(child) < unaryPower
	case syntax.BinaryExpression:
		// A looser operand, or an equal right one, would reassociate:
		// a - (b - c) is not a - b - c.
		power := 0
		for i := range parent.ChildCount() {
			if token, ok := parent.Child(i).Token(); ok && !token.Kind().IsTrivia() {
				power = syntax.BinaryPrecedence(token.Kind())
				break
			}
		}
		return expressionPower(child) < power || position == 1 && expressionPower(child) == power
	case syntax.ConditionalExpression:
		// Conditionals associate right, so only a conditional in condition
		// position would be reparsed as an arm: (a ? b : c) ? d : e.
		return position == 0 && child.kind == syntax.ConditionalExpression
	case syntax.TraversalExpression:
		if position != 0 {
			return false
		}
		if expressionPower(child) < traversalPower {
			// Steps bind tighter than operators: (a + b).c is not a + b.c.
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

// literalQuotedTemplate reports a quoted template with no interpolation or
// directive.
func literalQuotedTemplate(node *expressionView) bool {
	if node.kind != syntax.TemplateExpression || node.children[0].token.kind != syntax.QuoteOpen {
		return false
	}
	for _, child := range node.children {
		if child.node != nil {
			return false
		}
	}
	return true
}

// expressionPower returns the binding power of node's form.
func expressionPower(node *expressionView) int {
	switch node.kind {
	case syntax.ConditionalExpression:
		return conditionalPower
	case syntax.BinaryExpression:
		for _, element := range node.children {
			if element.node == nil && !element.token.kind.IsTrivia() {
				return syntax.BinaryPrecedence(element.token.kind)
			}
		}
	case syntax.UnaryExpression:
		return unaryPower
	case syntax.TraversalExpression:
		return traversalPower
	}
	return atomicPower
}
