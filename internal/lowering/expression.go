package lowering

import (
	"errors"
	"fmt"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// Expression lowers node from result to a layout. The caller must supply a node
// from this same Result, just as with Result.Text. Any diagnostics anywhere in
// result, a zero/non-expression node, or an unsupported expression form returns
// an error and an empty Doc. It never formats a recovered partial expression.
// Quoted interpolation-only wrappers and legacy numeric indices are normalized
// without changing result. Indices inside attribute splats retain their syntax
// because bracket indexing would change the projection's scope.
// Layout width and indentation are selected later by document.Render.
func Expression(result syntax.Result, node syntax.SyntaxNode) (document.Doc, error) {
	if len(result.Diagnostics()) != 0 {
		return document.Doc{}, errors.New("lowering: cannot format a result with diagnostics")
	}
	if !expressionKind(node.Kind()) {
		return document.Doc{}, errors.New("lowering: expected an expression node")
	}

	lowered, err := lowerExpression(result, node)
	return lowered.doc, err
}

// lowerExpression normalizes source and lowers the resulting view. It does not
// check diagnostics: Expression checks them for one node and File once for the
// whole file, and Result.Diagnostics clones its slice on every call, so this
// shared path must not repeat the check per attribute.
//
// The walk is the explicit post-order stack described at File, because deep
// unary and operator chains must not recurse. Each frame also records the
// grammar context its children inherit: safe, whether the surrounding grammar
// permits expression newlines; and inSequence, whether a template sequence
// encloses the node and therefore flattens source-only layout choices.
func lowerExpression(result syntax.Result, source syntax.SyntaxNode) (layout, error) {
	node := normalizeExpression(result, source)

	type frame struct {
		node       *expressionView
		next       int
		safe       bool // The surrounding grammar permits expression newlines.
		inSequence bool // Templates flatten source-only object layout choices.
	}
	stack := []frame{{node: node}}
	docs := make(map[*expressionView]layout)

	for len(stack) != 0 {
		current := &stack[len(stack)-1]
		if current.next < current.node.ChildCount() {
			element := current.node.Child(current.next)
			current.next++
			if child, ok := element.Node(); ok {
				safe, inSequence := current.safe, current.inSequence
				switch current.node.Kind() {
				case syntax.ObjectItem:
					safe = false // Object keys and values are newline-sensitive.
				case syntax.TemplateInterpolation, syntax.TemplateDirective:
					// ${ } and %{ } delimit their contents, and templates
					// flatten what they enclose (doc.go: Templates).
					safe, inSequence = true, true
				case syntax.ParenthesizedExpression, syntax.FunctionCallExpression,
					syntax.TupleExpression, syntax.IndexAccess, syntax.ForExpression,
					syntax.BinaryExpression, syntax.ConditionalExpression, syntax.TraversalExpression:
					// Operations enclose themselves when their caller is not
					// safe; their descendants can share that pair of
					// parentheses.
					safe = true
				}
				stack = append(stack, frame{node: child, safe: safe, inSequence: inSequence})
			}
			continue
		}

		doc, err := lowerNode(result, current.node, current.safe, current.inSequence, docs)
		if err != nil {
			return layout{}, err
		}
		docs[current.node] = doc
		stack = stack[:len(stack)-1]
	}
	return docs[node], nil
}

// expressionKind reports whether kind is a complete expression that the
// Expression entry point accepts. Traversal steps, object items, and template
// parts are lowered only as children of these forms.
func expressionKind(kind syntax.NodeKind) bool {
	switch kind {
	case syntax.LiteralExpression, syntax.VariableExpression,
		syntax.ParenthesizedExpression, syntax.UnaryExpression,
		syntax.BinaryExpression, syntax.ConditionalExpression,
		syntax.FunctionCallExpression, syntax.TraversalExpression,
		syntax.TupleExpression, syntax.ObjectExpression, syntax.ForExpression,
		syntax.TemplateExpression:
		return true
	}
	return false
}

// piece is one significant child of a node together with the trivia before
// it. Delimiter and separator policy is local to the parent; children expose
// only their layout. Exactly one of token and child is meaningful.
type piece struct {
	// doc is the child's rendered form: a token's spelling or a child
	// layout's doc. A parent may replace it, as object items do for their
	// separator.
	doc document.Doc
	// token reports a token piece. A child piece leaves it false and leaves
	// kind at syntax.Invalid, the zero TokenKind, so comparing kind against a
	// specific token kind needs no token guard.
	token bool
	kind  syntax.TokenKind
	// before is the trivia between the previous significant child and this
	// one. Comma pieces normally hand theirs to the next piece
	// (moveCommaTrivia), and clause helpers take it away (detachLeading) when
	// they lay it out through a commentGap of their own.
	before []syntax.SyntaxToken
	// child is the lowered layout of a node piece. A token piece leaves it
	// zero, whose false flags read as "no such boundary".
	child layout
}

// detachLeading copies pieces and clears the first copy's leading trivia,
// returning that trivia. Clause and sequence helpers call it when they lay
// out the first gap through a commentGap of their own, so the shared sequence
// helpers see a first piece without trivia and cannot emit the same comments
// twice. The copy leaves the parent's pieces untouched.
func detachLeading(pieces []piece) ([]piece, []syntax.SyntaxToken) {
	detached := append([]piece(nil), pieces...)
	leading := detached[0].before
	detached[0].before = nil
	return detached, leading
}

// layout is the lowered form of one expression node. Operations keep their
// head and continuation separate so an enclosing parenthesized expression can
// share their group, and the boundary flags let a parent choose spacing at
// this node's edges without inspecting its tokens.
type layout struct {
	// doc is the complete rendered expression; every node sets it.
	doc document.Doc
	// body is an operation's content without synthetic parentheses. Explicit
	// parentheses reuse it so that both spellings share one group and a
	// second pass reproduces the first. Set only when operation is true.
	body document.Doc
	// head is the first operand and continuation the operator/operand tail.
	// Enclosing delimiters supply indentation; continuations do not add
	// another level. A same-precedence binary chain concatenates its child's
	// continuation with its own instead of rescanning a prefix. Set only when
	// operation is true.
	head, continuation document.Doc
	// operation marks binary, conditional, and traversal nodes, whose broken
	// layout is the head followed by continuation lines.
	operation bool
	// power is the binding power of a binary node's operator, letting a
	// parent binary node recognize a same-precedence chain. Zero otherwise.
	power int
	// endsNumber reports a trailing Number token and startsDot a leading dot
	// step. With fusesNumber they decide whether a traversal must keep a
	// space before a step so the number scanner cannot swallow it.
	endsNumber bool
	startsDot  bool
	// fusesNumber reports that this attribute or legacy index step, placed
	// directly after a number, would scan as part of that number
	// (numberContinuesAcrossDot). Set only for those two step kinds.
	fusesNumber bool
	// endsHeredoc reports that the node's last significant token is a heredoc
	// marker whose newline the following gap must supply (doc.go: Heredocs).
	endsHeredoc bool
	// startsBrace and endsBrace report object braces at the node's edges,
	// which template sequence boundaries keep a space away from.
	startsBrace, endsBrace bool
}

// lowerNode lowers one view node whose children are already in docs. safe
// and inSequence are the grammar context recorded by lowerExpression's frame.
// Templates compose literal chunks and are handled apart; every other form
// is split into pieces, given its boundary flags, and then laid out by the
// helper that owns its delimiter and separator policy.
func lowerNode(result syntax.Result, node *expressionView, safe, inSequence bool, docs map[*expressionView]layout) (layout, error) {
	switch node.Kind() {
	case syntax.TemplateExpression, syntax.TemplateIf, syntax.TemplateFor:
		return templateParts(result, node, docs), nil
	}
	if !lowerableKind(node.Kind()) {
		return layout{}, fmt.Errorf("lowering: unsupported expression form %s", node.Kind())
	}

	pieces := collectPieces(result, node, docs)
	lowered := boundaryFlags(pieces)
	switch node.Kind() {
	case syntax.BinaryExpression, syntax.ConditionalExpression, syntax.TraversalExpression:
		lowered = lowerOperation(result, node.Kind(), pieces, lowered, safe)
	case syntax.AttributeAccess, syntax.LegacyIndexAccess:
		lowered.doc = sequence(result, pieces)
		lowered.fusesNumber = stepFusesNumber(result, node, pieces)
	default:
		lowered.doc = formDoc(result, node, pieces, inSequence)
	}
	return lowered, nil
}

// lowerableKind reports whether lowerNode has a layout for kind: every
// non-template form a diagnostic-free parse can produce. expressionKind is
// the narrower set accepted at the Expression entry point.
func lowerableKind(kind syntax.NodeKind) bool {
	switch kind {
	case syntax.LiteralExpression, syntax.VariableExpression,
		syntax.UnaryExpression, syntax.ParenthesizedExpression,
		syntax.FunctionCallExpression, syntax.TupleExpression,
		syntax.BinaryExpression, syntax.ConditionalExpression,
		syntax.TraversalExpression, syntax.AttributeAccess, syntax.IndexAccess,
		syntax.LegacyIndexAccess, syntax.AttributeSplat, syntax.FullSplat,
		syntax.ObjectExpression, syntax.ObjectItem, syntax.ForExpression,
		syntax.TemplateInterpolation, syntax.TemplateDirective:
		return true
	}
	return false
}

// collectPieces splits a node's children into significant pieces, each
// carrying the trivia that preceded it. Token pieces keep their spelling,
// synthesized or from source; node pieces carry the child's layout.
func collectPieces(result syntax.Result, node *expressionView, docs map[*expressionView]layout) []piece {
	pieces := make([]piece, 0, node.ChildCount())
	var trivia []syntax.SyntaxToken
	for i := 0; i < node.ChildCount(); i++ {
		element := node.Child(i)
		if token, ok := element.Token(); ok {
			switch token.Kind() {
			case syntax.Whitespace, syntax.Newline, syntax.LineComment, syntax.BlockComment:
				trivia = append(trivia, token.source)
				continue
			}
			pieces = append(pieces, piece{doc: document.Text(token.spelling(result)), token: true, kind: token.Kind(), before: trivia})
		} else {
			child, _ := element.Node()
			pieces = append(pieces, piece{doc: docs[child].doc, child: docs[child], before: trivia})
		}
		trivia = nil
	}
	return pieces
}

// boundaryFlags derives the layout flags describing a node's first and last
// significant pieces. A token piece answers from its kind and a child piece
// from its own flags; the other half of each disjunction is false because a
// token piece has a zero child and a child piece has the Invalid kind.
func boundaryFlags(pieces []piece) layout {
	first, last := pieces[0], pieces[len(pieces)-1]
	return layout{
		endsNumber:  last.token && last.kind == syntax.Number || !last.token && last.child.endsNumber,
		startsDot:   first.token && first.kind == syntax.Dot,
		endsHeredoc: !last.token && last.child.endsHeredoc,
		startsBrace: first.token && first.kind == syntax.OpenBrace || !first.token && first.child.startsBrace,
		endsBrace:   last.token && last.kind == syntax.CloseBrace || !last.token && last.child.endsBrace,
	}
}

// formDoc builds the doc for every non-operation, non-step form by delegating
// to the helper that owns that form's delimiter and separator policy.
func formDoc(result syntax.Result, node *expressionView, pieces []piece, inSequence bool) document.Doc {
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression, syntax.UnaryExpression:
		return sequence(result, pieces)
	case syntax.ParenthesizedExpression:
		return parenthesized(result, pieces)
	case syntax.TupleExpression:
		return delimited(result, pieces, 0, true, soft)
	case syntax.ObjectExpression:
		return object(result, pieces, inSequence)
	case syntax.ObjectItem:
		// The parser accepts either = or : between an object key and its
		// value with the same meaning. Output spells it = (doc.go: Objects),
		// so entries share one assignment column and a second pass sees the
		// spelling it produced.
		pieces[1].doc = document.Text("=")
		return assignment(result, pieces, true)
	case syntax.ForExpression:
		return forExpression(result, pieces)
	case syntax.TemplateInterpolation, syntax.TemplateDirective:
		return templateSequence(result, pieces)
	case syntax.IndexAccess:
		return index(result, node, pieces)
	case syntax.AttributeSplat, syntax.FullSplat:
		// The projection prefix is .* (two tokens) or [*] (three). The steps
		// after it are the projection and lay out as a traversal tail.
		prefix := 2
		if node.Kind() == syntax.FullSplat {
			prefix = 3
		}
		return document.Concat(sequence(result, pieces[:prefix]), traversalSequence(result, pieces[prefix:], false, false))
	default: // FunctionCallExpression, including namespace prefixes.
		for i, part := range pieces {
			if part.kind == syntax.OpenParen {
				return delimited(result, pieces, i, false, soft)
			}
		}
		panic("lowering: valid call has no opening parenthesis")
	}
}

// lowerOperation lays out a binary, conditional, or traversal expression as
// a head operand followed by continuation lines that begin with the operator
// or step, all in one group (doc.go: Operators, Traversals). Where the grammar
// forbids expression newlines, a broken binary or conditional operation adds
// synthetic parentheses. Traversals never do: width alone never breaks their
// steps, and their calls and indices carry their own delimiters.
func lowerOperation(result syntax.Result, kind syntax.NodeKind, pieces []piece, lowered layout, safe bool) layout {
	head := pieces[0]
	lowered.operation = true
	lowered.head = head.doc
	switch kind {
	case syntax.BinaryExpression:
		lowered.power = binaryPower(pieces[1].kind)
		lowered.continuation = operationContinuation(result, pieces[1:], head.child.endsHeredoc)
		if head.child.power == lowered.power {
			// A same-precedence chain shares one group and one run of
			// continuation lines instead of nesting a group per operator
			// (doc.go: Operators).
			lowered.head = head.child.head
			lowered.continuation = document.Concat(head.child.continuation, lowered.continuation)
		}
	case syntax.ConditionalExpression:
		lowered.continuation = operationContinuation(result, pieces[1:], head.child.endsHeredoc)
	case syntax.TraversalExpression:
		lowered.continuation = document.Group(traversalSequence(result, pieces[1:], head.child.endsNumber, head.child.endsHeredoc))
	}

	lowered.body = document.Concat(lowered.head, lowered.continuation)
	lowered.doc = document.Group(lowered.body)
	if !safe && kind != syntax.TraversalExpression {
		lowered.doc = syntheticParentheses(lowered.body)
		// A heredoc marker forces the group to break, so the node now ends in
		// its closing parenthesis and the following gap owes no newline.
		lowered.endsHeredoc = false
	}
	return lowered
}

// stepFusesNumber reports whether an attribute or legacy index step, placed
// directly after a Number token, would scan as part of that number. A
// retained comment between the dot and the name already separates the two
// tokens, so no boundary space is needed then.
func stepFusesNumber(result syntax.Result, node *expressionView, pieces []piece) bool {
	name, _ := node.Child(node.ChildCount() - 1).Token()
	if !numberContinuesAcrossDot(name.spelling(result)) {
		return false
	}
	for _, token := range pieces[1].before {
		if token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment {
			return false
		}
	}
	return true
}

// sequence joins pieces that render on one line, spacing any comments. Inside
// a splat or index a comment hugs its bracket, and a comment after a dot step
// hugs the dot, so the step's tokens stay recognizable as one unit.
func sequence(result syntax.Result, pieces []piece) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*2)
	for i, part := range pieces {
		style := spacedGap(tight)
		style.requiredLine = i > 0 && pieces[i-1].child.endsHeredoc
		if i > 0 && pieces[i-1].kind == syntax.OpenBracket {
			style.beforeComment = soft
		}
		if i > 0 && pieces[i-1].kind == syntax.Dot {
			style.beforeComment = tight
		}
		if part.kind == syntax.CloseBracket {
			style.afterComment = tight
		}

		gap, end := commentGap(result, part.before, style)
		parts = append(parts, gap, end, part.doc)
	}
	return document.Concat(parts...)
}

// parenthesized lays out explicit parentheses. They introduce no width-driven
// break of their own (doc.go: Parentheses): an enclosed operation supplies
// the group, and any other content simply carries its comments.
func parenthesized(result syntax.Result, pieces []piece) document.Doc {
	inner := pieces[1]
	close := pieces[len(pieces)-1]
	if inner.child.operation {
		// Synthetic parentheses become explicit on the next parse. Both paths
		// share the inner operation's group, so formatting remains idempotent.
		leading, start := commentGap(result, inner.before, openingGap(soft))
		gap, end := commentGap(result, close.before, breakingGap(soft, inner.child.endsHeredoc))
		return document.Group(document.Concat(pieces[0].doc, document.Indent(document.Concat(leading, start, inner.child.body, gap)), end, close.doc))
	}

	openerComment := tight
	for _, token := range inner.before {
		if token.Kind() == syntax.LineComment {
			// Upstream gives an inline line comment a visible opener boundary.
			openerComment = space
			break
		}
		if token.Kind() == syntax.BlockComment {
			break
		}
	}
	// Non-operation content never breaks at the parentheses, so the opener
	// and closer gaps have no empty separator: (x) stays compact.
	leading, start := commentGap(result, inner.before, gapStyle{beforeComment: openerComment, afterComment: space})
	gap, end := commentGap(result, close.before, gapStyle{beforeComment: space, requiredLine: inner.child.endsHeredoc})
	return document.Concat(pieces[0].doc, document.Indent(document.Concat(leading, start, inner.doc, gap)), end, close.doc)
}

// delimited lays out a bracketed list: a tuple, a call's arguments, or an
// object's entries once object has materialized their commas. pieces[open] is
// the opening delimiter (a call's name precedes it), the last piece is the
// closer, and the content between alternates values with comma and ellipsis
// tokens. preserveBlank keeps one source blank line between broken entries
// (tuples and objects, not calls). edge is the separator directly inside the
// delimiters: soft for brackets and parentheses, line for objects, and hard
// for a source-vertical object.
//
// A broken layout ends in a trailing comma and a flat layout in none, so the
// output does not depend on whether the source had one and a second pass
// reproduces it. The comma is omitted after an expanded call argument, since
// the grammar allows a trailing comma or an ellipsis but not both, and after
// a final heredoc, whose marker newline already separates it from the closer
// (doc.go: Calls and tuples).
func delimited(result syntax.Result, pieces []piece, open int, preserveBlank bool, edge spacing) document.Doc {
	moveCommaTrivia(pieces)
	// A heredoc's mandatory marker newline is enough before the closer. Drop
	// a source trailing comma as well as avoiding a synthesized one. Its trivia
	// has already moved to the closer, so no comments or blank lines are lost.
	if last := len(pieces) - 2; last > open && pieces[last].kind == syntax.Comma && pieces[last-1].child.endsHeredoc {
		pieces[last+1].before = append(pieces[last].before, pieces[last+1].before...)
		pieces = append(pieces[:last], pieces[last+1:]...)
	}

	head := sequence(result, pieces[:open+1])
	close := pieces[len(pieces)-1]
	content := pieces[open+1 : len(pieces)-1]
	parts := make([]document.Doc, 0, len(content)*3+3)
	for i, part := range content {
		style := spacedGap(tight)
		style.requiredLine = i > 0 && content[i-1].child.endsHeredoc
		if i == 0 {
			// The first entry sits directly inside the opener.
			style.empty, style.beforeComment = edge, edge
		} else if content[i-1].kind == syntax.Comma || content[i-1].child.endsHeredoc {
			// An entry after a separator starts its own line once the list
			// breaks; a heredoc marker serves as that separator.
			style.empty, style.afterComment = line, line
			style.blankLine = preserveBlank
		}
		if part.kind == syntax.Comma || part.kind == syntax.Ellipsis {
			// A comma or ellipsis is a suffix of the value before it, so it
			// follows a comment tightly rather than after a space.
			style.afterComment = tight
		}

		gap, end := commentGap(result, part.before, style)
		value := part.doc
		if i == len(content)-1 && part.kind == syntax.Comma {
			value = document.IfBreak(value, document.Doc{})
		}
		parts = append(parts, gap, end, value)
	}
	if len(content) > 0 {
		last := content[len(content)-1].kind
		if last != syntax.Comma && last != syntax.Ellipsis && !content[len(content)-1].child.endsHeredoc {
			parts = append(parts, document.IfBreak(document.Text(","), document.Doc{}))
		}
	}

	style := breakingGap(edge, len(content) > 0 && content[len(content)-1].child.endsHeredoc)
	if len(content) == 0 {
		// Empty delimiters stay compact: the closer hugs the opener, and a
		// comment inside only forces a soft break. A source-vertical object
		// keeps its hard line even when empty (doc.go: Objects).
		style.empty, style.beforeComment, style.afterComment = tight, soft, soft
		if edge == hard {
			style.empty, style.beforeComment, style.afterComment = hard, hard, hard
		}
	}
	gap, end := commentGap(result, close.before, style)
	parts = append(parts, gap)
	return document.Group(document.Concat(head, document.Indent(document.Concat(parts...)), end, close.doc))
}

// moveCommaTrivia hands each comma's leading trivia to the piece after it.
// Commas are canonical separators rather than comment anchors, so a comment
// written before a comma renders after it (doc.go: Comments). This also
// applies to for bindings and template directive headers, not only lists.
func moveCommaTrivia(pieces []piece) {
	for i := 0; i < len(pieces)-1; i++ {
		// A required comma after a heredoc cannot cross the marker newline.
		// Keeping that gap separate also avoids inventing a blank line later.
		if i > 0 && pieces[i-1].child.endsHeredoc {
			continue
		}
		if pieces[i].token && pieces[i].kind == syntax.Comma && len(pieces[i].before) > 0 {
			pieces[i+1].before = append(pieces[i].before, pieces[i+1].before...)
			pieces[i].before = nil
		}
	}
}
