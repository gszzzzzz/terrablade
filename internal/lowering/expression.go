package lowering

import (
	"errors"
	"fmt"

	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

// Expression lowers node from result to a layout. The caller must supply a node
// from this same Result, just as with Result.Text. Any diagnostics anywhere in
// result, a zero/non-expression node, or an unsupported expression form returns
// an error and an empty Doc. It never formats a recovered partial expression.
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

// File validates the whole result once, then lowers each attribute expression
// through this same path without repeatedly copying the diagnostics slice.
func lowerExpression(result syntax.Result, node syntax.SyntaxNode) (layout, error) {
	type frame struct {
		node       syntax.SyntaxNode
		next       int
		safe       bool // The surrounding grammar permits expression newlines.
		inSequence bool // Templates flatten source-only object layout choices.
	}
	stack := []frame{{node: node}}
	docs := make(map[syntax.SyntaxNode]layout)
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
					safe, inSequence = true, true
				case syntax.ParenthesizedExpression, syntax.FunctionCallExpression,
					syntax.TupleExpression, syntax.IndexAccess, syntax.ForExpression,
					syntax.BinaryExpression, syntax.ConditionalExpression, syntax.TraversalExpression:
					// Operations enclose themselves when their caller is not safe;
					// their descendants can share that pair of parentheses.
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

// A piece is one significant child and its preceding trivia. Delimiter and
// separator policy is local to the parent; children expose only their layout.
type piece struct {
	doc    document.Doc
	token  bool
	kind   syntax.TokenKind
	before []syntax.SyntaxToken
	child  layout
}

// Operations keep their head and continuation separate. Safe contexts indent
// the continuation once; parentheses indent the whole ungrouped body instead.
// Same-precedence chains concatenate continuations without rescanning a prefix.
type layout struct {
	doc, body, head, continuation document.Doc
	operation                     bool
	power                         int
	endsNumber                    bool
	startsDot                     bool
	fusesNumber                   bool
	endsHeredoc                   bool
}

func lowerNode(result syntax.Result, node syntax.SyntaxNode, safe, inSequence bool, docs map[syntax.SyntaxNode]layout) (layout, error) {
	switch node.Kind() {
	case syntax.TemplateExpression, syntax.TemplateIf, syntax.TemplateFor:
		return templateParts(result, node, docs), nil
	}
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression,
		syntax.UnaryExpression, syntax.ParenthesizedExpression,
		syntax.FunctionCallExpression, syntax.TupleExpression,
		syntax.BinaryExpression, syntax.ConditionalExpression,
		syntax.TraversalExpression, syntax.AttributeAccess, syntax.IndexAccess,
		syntax.LegacyIndexAccess, syntax.AttributeSplat, syntax.FullSplat:
	case syntax.ObjectExpression, syntax.ObjectItem, syntax.ForExpression:
	case syntax.TemplateInterpolation, syntax.TemplateDirective:
	default:
		return layout{}, fmt.Errorf("lowering: unsupported expression form %s", node.Kind())
	}
	pieces := make([]piece, 0, node.ChildCount())
	var trivia []syntax.SyntaxToken
	for i := 0; i < node.ChildCount(); i++ {
		element := node.Child(i)
		if token, ok := element.Token(); ok {
			switch token.Kind() {
			case syntax.Whitespace, syntax.Newline, syntax.LineComment, syntax.BlockComment:
				trivia = append(trivia, token)
				continue
			}
			pieces = append(pieces, piece{doc: document.Text(result.Text(token.Span())), token: true, kind: token.Kind(), before: trivia})
		} else {
			child, _ := element.Node()
			pieces = append(pieces, piece{doc: docs[child].doc, child: docs[child], before: trivia})
		}
		trivia = nil
	}
	last := pieces[len(pieces)-1]
	lowered := layout{
		endsNumber:  last.token && last.kind == syntax.Number || !last.token && last.child.endsNumber,
		startsDot:   pieces[0].token && pieces[0].kind == syntax.Dot,
		endsHeredoc: !last.token && last.child.endsHeredoc,
	}
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression, syntax.UnaryExpression:
		lowered.doc = sequence(result, pieces)
	case syntax.ParenthesizedExpression:
		lowered.doc = parenthesized(result, pieces)
	case syntax.TupleExpression:
		lowered.doc = delimited(result, pieces, 0, true, soft)
	case syntax.ObjectExpression:
		lowered.doc = object(result, pieces, inSequence)
	case syntax.ObjectItem:
		pieces[1].doc = document.Text("=")
		lowered.doc = assignment(result, pieces, true)
	case syntax.ForExpression:
		lowered.doc = forExpression(result, pieces)
	case syntax.TemplateInterpolation, syntax.TemplateDirective:
		lowered.doc = templateSequence(result, pieces)
	case syntax.BinaryExpression:
		lowered.power = binaryPower(pieces[1].kind)
		lowered.head = pieces[0].doc
		lowered.continuation = operationContinuation(result, pieces[1:], pieces[0].child.endsHeredoc)
		if pieces[0].child.power == lowered.power {
			lowered.head = pieces[0].child.head
			lowered.continuation = document.Concat(pieces[0].child.continuation, lowered.continuation)
		}
		lowered.operation = true
	case syntax.ConditionalExpression:
		lowered.head = pieces[0].doc
		lowered.continuation = operationContinuation(result, pieces[1:], pieces[0].child.endsHeredoc)
		lowered.operation = true
	case syntax.TraversalExpression:
		lowered.head = pieces[0].doc
		lowered.continuation = document.Group(traversalSequence(result, pieces[1:], pieces[0].child.endsNumber, pieces[0].child.endsHeredoc))
		lowered.operation = true
	case syntax.AttributeAccess, syntax.LegacyIndexAccess:
		lowered.doc = sequence(result, pieces)
		lowered.fusesNumber = numberContinuesAcrossDot(result.Text(node.Child(node.ChildCount() - 1).Span()))
		for _, token := range pieces[1].before {
			if token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment {
				// A retained comment between dot and name already stops scanning.
				lowered.fusesNumber = false
			}
		}
	case syntax.IndexAccess:
		lowered.doc = index(result, pieces)
	case syntax.AttributeSplat, syntax.FullSplat:
		prefix := 2
		if node.Kind() == syntax.FullSplat {
			prefix = 3
		}
		lowered.doc = document.Concat(sequence(result, pieces[:prefix]), traversalSequence(result, pieces[prefix:], false, false))
	default: // FunctionCallExpression, including namespace prefixes.
		for i, part := range pieces {
			if part.kind == syntax.OpenParen {
				lowered.doc = delimited(result, pieces, i, false, soft)
				return lowered, nil
			}
		}
		panic("lowering: valid call has no opening parenthesis")
	}
	if lowered.operation {
		lowered.body = document.Concat(lowered.head, lowered.continuation)
		lowered.doc = document.Group(document.Concat(lowered.head, document.Indent(lowered.continuation)))
		if !safe {
			lowered.doc = syntheticParentheses(lowered.body)
			lowered.endsHeredoc = false
		}
	}
	return lowered, nil
}

func sequence(result syntax.Result, pieces []piece) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*2)
	for i, part := range pieces {
		style := gapStyle{beforeComment: space, afterComment: space}
		style.requiredLine = i > 0 && pieces[i-1].child.endsHeredoc
		if i > 0 && pieces[i-1].kind == syntax.OpenBracket {
			style.beforeComment = soft
		}
		if part.kind == syntax.CloseBracket {
			style.afterComment = tight
		}
		gap, end := commentGap(result, part.before, style)
		parts = append(parts, gap, end, part.doc)
	}
	return document.Concat(parts...)
}

func parenthesized(result syntax.Result, pieces []piece) document.Doc {
	inner := pieces[1]
	close := pieces[len(pieces)-1]
	if inner.child.operation {
		// Synthetic parentheses become explicit on the next parse. Both paths
		// share the inner operation's group, so formatting remains idempotent.
		leading, start := commentGap(result, inner.before, gapStyle{empty: soft, beforeComment: soft, afterComment: space})
		gap, end := commentGap(result, close.before, gapStyle{empty: soft, beforeComment: space, afterComment: soft, requiredLine: inner.child.endsHeredoc})
		return document.Group(document.Concat(pieces[0].doc, document.Indent(document.Concat(leading, start, inner.child.body, gap)), end, close.doc))
	}
	leading, start := commentGap(result, inner.before, gapStyle{afterComment: space})
	gap, end := commentGap(result, close.before, gapStyle{beforeComment: space, requiredLine: inner.child.endsHeredoc})
	return document.Concat(pieces[0].doc, document.Indent(document.Concat(leading, start, inner.doc, gap)), end, close.doc)
}

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
		style := gapStyle{beforeComment: space, afterComment: space}
		style.requiredLine = i > 0 && content[i-1].child.endsHeredoc
		if i == 0 {
			style.empty, style.beforeComment = edge, edge
		} else if content[i-1].kind == syntax.Comma || content[i-1].child.endsHeredoc {
			style.empty, style.afterComment = line, line
			style.blankLine = preserveBlank
		}
		if part.kind == syntax.Comma || part.kind == syntax.Ellipsis {
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
	style := gapStyle{empty: edge, beforeComment: space, afterComment: edge}
	style.requiredLine = len(content) > 0 && content[len(content)-1].child.endsHeredoc
	if len(content) == 0 {
		style.empty, style.beforeComment, style.afterComment = tight, soft, soft
		if edge == hard {
			style.empty, style.beforeComment, style.afterComment = hard, hard, hard
		}
	}
	gap, end := commentGap(result, close.before, style)
	parts = append(parts, gap)
	return document.Group(document.Concat(head, document.Indent(document.Concat(parts...)), end, close.doc))
}

// Commas are canonical separators rather than comment anchors. This also
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
