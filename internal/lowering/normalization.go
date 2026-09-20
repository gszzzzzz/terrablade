package lowering

import "github.com/gszzzzzz/terrablade/internal/syntax"

// expressionView is a private, immutable syntax overlay. Original tokens keep
// their source spans; only canonical delimiters are synthesized. The lossless
// CST and the public lowering interface never expose these rewrite decisions.
// Its accessors mirror syntax.SyntaxNode so the layout code reads a view the
// same way it would read the CST.
type expressionView struct {
	kind     syntax.NodeKind
	children []expressionElement
	// needsNewlineContext reports that this node exposes a mandatory line
	// break without a delimiter of its own (exposedExpressionLines). A parent
	// object item or the root then wraps unary and traversal nodes in
	// parentheses (protectExpressionLines).
	needsNewlineContext bool
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

// normalizeExpression rewrites the expression rooted at root into a view that
// spells quoted interpolation-only wrappers as their inner expression and
// legacy numeric indices as bracket indices (doc.go: Quoted wrappers, Numeric
// indices). It visits each source element once in the post-order walk
// described at File. An alias carries its removed wrapper's grammar role to
// the immediate parent, which can then protect precedence without rescanning
// a deep expression chain: rewrite.unwrapped tells the parent that the child
// lost the "${ }" delimiters that made it an atom.
func normalizeExpression(result syntax.Result, root syntax.SyntaxNode) *expressionView {
	type rewrite struct {
		node      *expressionView
		unwrapped bool
	}
	type frame struct {
		node syntax.SyntaxNode
		next int
		// attributeProjection marks a direct child of an attribute splat:
		// a step in the projection, where brackets would change meaning.
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
					rewritten.node = permanentParentheses(rewritten.node, nil, nil)
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
				// Delimiter comments need a stable home after both template
				// edges disappear. Parentheses also make every line comment
				// newline legal.
				rewritten = rewrite{node: permanentParentheses(inner, before, after)}
			}
		} else if node.Kind() == syntax.LegacyIndexAccess && !current.attributeProjection {
			// Brackets inside .* would index the projected tuple instead of
			// each element. Keep those dot steps until splats can be
			// modernized together.
			view.kind = syntax.IndexAccess
			view.children[0] = delimiter(syntax.OpenBracket, "[")
			last := len(view.children) - 1
			view.children[last] = expressionElement{node: &expressionView{kind: syntax.LiteralExpression, children: []expressionElement{view.children[last]}}}
			view.children = append(view.children, delimiter(syntax.CloseBracket, "]"))
			// The brackets now delimit the index, so nothing is exposed.
			view.needsNewlineContext = false
		}
		views[node] = rewritten
		stack = stack[:len(stack)-1]
	}
	return protectExpressionLines(views[root].node)
}

// protectExpressionLines wraps a unary or traversal node that exposes a
// mandatory line break in permanent parentheses. Removing an interpolation
// also removes its newline-safe grammar context. Binary and conditional
// layouts already enclose mandatory continuation lines; unary and traversal
// expressions need a permanent delimiter when exposed at an attribute value
// or object key/value. Delimited descendants own their lines.
func protectExpressionLines(node *expressionView) *expressionView {
	if node.needsNewlineContext && (node.kind == syntax.UnaryExpression || node.kind == syntax.TraversalExpression) {
		return permanentParentheses(node, nil, nil)
	}
	return node
}

// exposedExpressionLines answers whether node contains a mandatory line
// break that none of its own tokens delimit: a line comment, a block comment
// followed by a source newline, a heredoc marker followed by more of the
// node, or a child that already exposes one. Only undelimited forms can
// expose a line; delimited forms and the delimited descendants of these forms
// own their lines already, so the answer stops at them.
//
// The answer matters once a quoted wrapper is removed: the "${ }" that made
// newlines legal is gone, and an attribute value or an object key or value
// ends where the grammar forbids an unparenthesized newline (doc.go:
// Expression context). Binary and conditional layouts add break parentheses
// when they break, but unary and traversal layouts have no group of their
// own, so protectExpressionLines gives them permanent parentheses when this
// reports true.
//
// comment and newline describe the trivia run since the last significant
// element, because a block comment keeps a source newline after it while a
// bare newline is canonicalized away. heredoc records that the previous child
// ended in a marker, whose newline becomes exposed as soon as anything but
// trivia follows within this node.
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

// expressionEndsHeredoc reports whether node's last significant element is a
// heredoc marker, whose terminating newline the enclosing gap or body must
// supply (doc.go: Heredocs). A template ends in a marker exactly when it
// opened as a heredoc. Any other node defers to its last child; trailing
// trivia is skipped because a comment there does not change which token the
// following gap must separate from, and any other token ends the node with
// something that is not a marker.
func expressionEndsHeredoc(node *expressionView) bool {
	if node.kind == syntax.TemplateExpression {
		return node.children[0].token.kind == syntax.HeredocOpen
	}
	for i := len(node.children) - 1; i >= 0; i-- {
		child := node.children[i]
		if child.node != nil {
			return child.node.endsHeredoc
		}
		if !isTrivia(child.token.kind) {
			return false
		}
	}
	return false
}

// quotedWrapper recognizes a quoted template that holds exactly one
// interpolation and nothing else, and returns its inner expression with the
// trivia on either side of it. Such a wrapper returns its inner value without
// string conversion, so removing it preserves meaning (doc.go: Quoted
// wrappers). Literal text (even whitespace), directives, and heredocs are not
// wrappers.
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
		} else if isTrivia(element.token.kind) {
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

// permanentParentheses wraps inner in parentheses that appear in every
// layout, keeping any wrapper-edge trivia inside them so those comments
// retain their position. Unlike operations.breakParentheses, which appears
// only when a group breaks, these parentheses are part of the view itself:
// the grammar or the operand's precedence needs them at every width.
func permanentParentheses(inner *expressionView, before, after []expressionElement) *expressionView {
	children := make([]expressionElement, 0, len(before)+len(after)+3)
	children = append(children, delimiter(syntax.OpenParen, "("))
	children = append(children, before...)
	children = append(children, expressionElement{node: inner})
	children = append(children, after...)
	children = append(children, delimiter(syntax.CloseParen, ")"))
	return &expressionView{kind: syntax.ParenthesizedExpression, children: children}
}

// needsGrouping decides whether an unwrapped child of parent must gain
// parentheses to keep the meaning its "${ }" wrapper gave it (doc.go: Grammar
// and content). position counts only node children, so it names the child's
// grammar role: the key of an object item, the left or right operand of a
// binary operator, the condition of a conditional, the root of a traversal,
// or the first element of a tuple. Each case names the misreading it
// prevents; an already parenthesized child needs nothing.
func needsGrouping(result syntax.Result, parent syntax.SyntaxNode, position int, child *expressionView) bool {
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
		// A looser operand would bind to this operator instead, and an
		// equal-power right operand would reassociate the left-associative
		// chain: a - (b - c) is not a - b - c. The operator is the first
		// direct token that is not trivia.
		power := 0
		for i := range parent.ChildCount() {
			if token, ok := parent.Child(i).Token(); ok && !isTrivia(token.Kind()) {
				power = binaryPower(token.Kind())
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
// directive, whose value is fixed text and therefore unambiguous as a key.
func literalQuotedTemplate(node *expressionView) bool {
	if node.kind != syntax.TemplateExpression || node.children[0].token.kind != syntax.QuoteOpen {
		return false
	}
	for _, child := range node.children {
		if child.node != nil {
			return false // Interpolations and directives are evaluated key content.
		}
	}
	return true
}

// expressionPower maps an expression form to its binding power. A binary
// node takes its operator's power; delimited and literal forms are atomic.
func expressionPower(node *expressionView) int {
	switch node.kind {
	case syntax.ConditionalExpression:
		return conditionalPower
	case syntax.BinaryExpression:
		for _, element := range node.children {
			if element.node == nil && !isTrivia(element.token.kind) {
				return binaryPower(element.token.kind)
			}
		}
	case syntax.UnaryExpression:
		return unaryPower
	case syntax.TraversalExpression:
		return traversalPower
	}
	return atomicPower
}
