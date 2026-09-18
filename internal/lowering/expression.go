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
	type frame struct {
		node syntax.SyntaxNode
		next int
		safe bool // The surrounding grammar permits expression newlines.
	}
	stack := []frame{{node: node}}
	docs := make(map[syntax.SyntaxNode]layout)
	for len(stack) != 0 {
		current := &stack[len(stack)-1]
		if current.next < current.node.ChildCount() {
			element := current.node.Child(current.next)
			current.next++
			if child, ok := element.Node(); ok {
				safe := current.safe
				switch current.node.Kind() {
				case syntax.ParenthesizedExpression, syntax.FunctionCallExpression,
					syntax.TupleExpression, syntax.IndexAccess,
					syntax.BinaryExpression, syntax.ConditionalExpression, syntax.TraversalExpression:
					// Operations enclose themselves when their caller is not safe;
					// their descendants can share that pair of parentheses.
					safe = true
				}
				stack = append(stack, frame{node: child, safe: safe})
			}
			continue
		}
		doc, err := lowerNode(result, current.node, current.safe, docs)
		if err != nil {
			return document.Doc{}, err
		}
		docs[current.node] = doc
		stack = stack[:len(stack)-1]
	}
	return docs[node].doc, nil
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
	kind   syntax.TokenKind
	before []syntax.SyntaxToken
	child  layout
}

// body omits the operation's outer group. A parent can share that group for a
// same-precedence chain or explicit parentheses without inspecting document IR.
type layout struct {
	doc, body  document.Doc
	operation  bool
	power      int
	endsNumber bool
	startsDot  bool
}

func lowerNode(result syntax.Result, node syntax.SyntaxNode, safe bool, docs map[syntax.SyntaxNode]layout) (layout, error) {
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression,
		syntax.UnaryExpression, syntax.ParenthesizedExpression,
		syntax.FunctionCallExpression, syntax.TupleExpression,
		syntax.BinaryExpression, syntax.ConditionalExpression,
		syntax.TraversalExpression, syntax.AttributeAccess, syntax.IndexAccess,
		syntax.LegacyIndexAccess, syntax.AttributeSplat, syntax.FullSplat:
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
			pieces = append(pieces, piece{doc: document.Text(result.Text(token.Span())), kind: token.Kind(), before: trivia})
		} else {
			child, _ := element.Node()
			pieces = append(pieces, piece{doc: docs[child].doc, child: docs[child], before: trivia})
		}
		trivia = nil
	}
	last := pieces[len(pieces)-1]
	lowered := layout{endsNumber: last.kind == syntax.Number || last.child.endsNumber, startsDot: pieces[0].kind == syntax.Dot}
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression, syntax.UnaryExpression:
		lowered.doc = sequence(result, pieces)
	case syntax.ParenthesizedExpression:
		lowered.doc = parenthesized(result, pieces)
	case syntax.TupleExpression:
		lowered.doc = delimited(result, pieces, 0, true)
	case syntax.BinaryExpression:
		lowered.power = binaryPower(pieces[1].kind)
		if pieces[0].child.power == lowered.power {
			pieces[0].doc = pieces[0].child.body
		}
		lowered.body = operationSequence(result, pieces)
		lowered.operation = true
	case syntax.ConditionalExpression:
		lowered.body = operationSequence(result, pieces)
		lowered.operation = true
	case syntax.TraversalExpression:
		lowered.body = document.Concat(pieces[0].doc, document.Group(traversalSequence(result, pieces[1:], pieces[0].child.endsNumber)))
		lowered.operation = true
	case syntax.AttributeAccess, syntax.LegacyIndexAccess:
		lowered.doc = sequence(result, pieces)
	case syntax.IndexAccess:
		lowered.doc = index(result, pieces)
	case syntax.AttributeSplat, syntax.FullSplat:
		prefix := 2
		if node.Kind() == syntax.FullSplat {
			prefix = 3
		}
		lowered.doc = document.Concat(sequence(result, pieces[:prefix]), traversalSequence(result, pieces[prefix:], false))
	default: // FunctionCallExpression, including namespace prefixes.
		for i, part := range pieces {
			if part.kind == syntax.OpenParen {
				lowered.doc = delimited(result, pieces, i, false)
				return lowered, nil
			}
		}
		panic("lowering: valid call has no opening parenthesis")
	}
	if lowered.operation {
		lowered.doc = document.Group(lowered.body)
		if !safe {
			lowered.doc = syntheticParentheses(lowered.body)
		}
	}
	return lowered, nil
}

func sequence(result syntax.Result, pieces []piece) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*2)
	for i, part := range pieces {
		style := gapStyle{beforeComment: space, afterComment: space}
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
		gap, end := commentGap(result, close.before, gapStyle{empty: soft, beforeComment: space, afterComment: soft})
		return document.Group(document.Concat(pieces[0].doc, document.Indent(document.Concat(leading, start, inner.child.body, gap)), end, close.doc))
	}
	leading, start := commentGap(result, inner.before, gapStyle{afterComment: space})
	gap, end := commentGap(result, close.before, gapStyle{beforeComment: space})
	return document.Concat(pieces[0].doc, document.Indent(document.Concat(leading, start, inner.doc, gap)), end, close.doc)
}

func delimited(result syntax.Result, pieces []piece, open int, preserveBlank bool) document.Doc {
	// Commas are canonical separators rather than comment anchors. Move their
	// leading trivia to the following gap so comments cannot swallow punctuation.
	for i := open + 1; i < len(pieces)-1; i++ {
		if pieces[i].kind == syntax.Comma && len(pieces[i].before) > 0 {
			pieces[i+1].before = append(pieces[i].before, pieces[i+1].before...)
			pieces[i].before = nil
		}
	}
	head := sequence(result, pieces[:open+1])
	close := pieces[len(pieces)-1]
	content := pieces[open+1 : len(pieces)-1]
	parts := make([]document.Doc, 0, len(content)*3+3)
	for i, part := range content {
		style := gapStyle{beforeComment: space, afterComment: space}
		if i == 0 {
			style.empty, style.beforeComment = soft, soft
		} else if content[i-1].kind == syntax.Comma {
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
		if last != syntax.Comma && last != syntax.Ellipsis {
			parts = append(parts, document.IfBreak(document.Text(","), document.Doc{}))
		}
	}
	style := gapStyle{empty: soft, beforeComment: space, afterComment: soft}
	if len(content) == 0 {
		style.empty, style.beforeComment = tight, soft
	}
	gap, end := commentGap(result, close.before, style)
	parts = append(parts, gap)
	return document.Group(document.Concat(head, document.Indent(document.Concat(parts...)), end, close.doc))
}
