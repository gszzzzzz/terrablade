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
	}
	stack := []frame{{node: node}}
	docs := make(map[syntax.SyntaxNode]document.Doc)
	for len(stack) != 0 {
		current := &stack[len(stack)-1]
		if current.next < current.node.ChildCount() {
			element := current.node.Child(current.next)
			current.next++
			if child, ok := element.Node(); ok {
				stack = append(stack, frame{node: child})
			}
			continue
		}
		doc, err := lowerNode(result, current.node, docs)
		if err != nil {
			return document.Doc{}, err
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
	kind   syntax.TokenKind
	before []syntax.SyntaxToken
}

func lowerNode(result syntax.Result, node syntax.SyntaxNode, docs map[syntax.SyntaxNode]document.Doc) (document.Doc, error) {
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression,
		syntax.UnaryExpression, syntax.ParenthesizedExpression,
		syntax.FunctionCallExpression, syntax.TupleExpression:
	default:
		return document.Doc{}, fmt.Errorf("lowering: unsupported expression form %s", node.Kind())
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
			pieces = append(pieces, piece{document.Text(result.Text(token.Span())), token.Kind(), trivia})
		} else {
			child, _ := element.Node()
			pieces = append(pieces, piece{doc: docs[child], before: trivia})
		}
		trivia = nil
	}
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression, syntax.UnaryExpression:
		return sequence(result, pieces), nil
	case syntax.ParenthesizedExpression:
		return delimited(result, pieces, 0, false), nil
	case syntax.TupleExpression:
		return delimited(result, pieces, 0, true), nil
	default: // FunctionCallExpression, including namespace prefixes.
		for i, part := range pieces {
			if part.kind == syntax.OpenParen {
				return delimited(result, pieces, i, true), nil
			}
		}
		panic("lowering: valid call has no opening parenthesis")
	}
}

func sequence(result syntax.Result, pieces []piece) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*2)
	for _, part := range pieces {
		gap, end := commentGap(result, part.before, tight)
		parts = append(parts, gap, end, part.doc)
	}
	return document.Concat(parts...)
}

func delimited(result syntax.Result, pieces []piece, open int, comma bool) document.Doc {
	head := sequence(result, pieces[:open+1])
	close := pieces[len(pieces)-1]
	content := pieces[open+1 : len(pieces)-1]
	parts := make([]document.Doc, 0, len(content)*3+3)
	for i, part := range content {
		spacing := tight
		if i == 0 {
			spacing = soft
		} else if content[i-1].kind == syntax.Comma {
			spacing = line
		}
		gap, end := commentGap(result, part.before, spacing)
		value := part.doc
		if i == len(content)-1 && part.kind == syntax.Comma {
			value = document.IfBreak(value, document.Doc{})
		}
		parts = append(parts, gap, end, value)
	}
	if comma && len(content) > 0 {
		last := content[len(content)-1].kind
		if last != syntax.Comma && last != syntax.Ellipsis {
			parts = append(parts, document.IfBreak(document.Text(","), document.Doc{}))
		}
	}
	spacing := soft
	if len(content) == 0 {
		spacing = tight
	}
	gap, end := commentGap(result, close.before, spacing)
	parts = append(parts, gap)
	return document.Group(document.Concat(head, document.Indent(document.Concat(parts...)), end, close.doc))
}
