package lowering

import (
	"errors"

	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

// File lowers a complete native HCL file, including body comments and its final
// newline. A zero Result or any parse diagnostic returns an error and an empty
// Doc. Layout width and indentation are selected later by document.Render.
func File(result syntax.Result) (document.Doc, error) {
	if len(result.Diagnostics()) != 0 {
		return document.Doc{}, errors.New("lowering: cannot format a result with diagnostics")
	}
	if result.Root().Kind() != syntax.File {
		return document.Doc{}, errors.New("lowering: expected a parsed file")
	}
	type frame struct {
		node syntax.SyntaxNode
		next int
	}
	stack := []frame{{node: result.Root()}}
	docs := make(map[syntax.SyntaxNode]bodyLayout)
	for len(stack) != 0 {
		current := &stack[len(stack)-1]
		if current.node.Kind() != syntax.Attribute && current.next < current.node.ChildCount() {
			element := current.node.Child(current.next)
			current.next++
			if child, ok := element.Node(); ok {
				stack = append(stack, frame{node: child})
			}
			continue
		}
		var lowered bodyLayout
		switch current.node.Kind() {
		case syntax.File:
			var parts []document.Doc
			for i := range current.node.ChildCount() {
				element := current.node.Child(i)
				if body, ok := element.Node(); ok {
					parts = append(parts, docs[body].doc, docs[body].end)
				}
			}
			lowered.doc = document.Concat(parts...)
		case syntax.Body:
			lowered = body(result, current.node, len(stack) > 2, docs)
		case syntax.Block:
			lowered.doc = block(result, current.node, docs)
		case syntax.BlockLabel:
			text := result.Text(current.node.Span())
			if token, _ := current.node.Child(0).Token(); token.Kind() == syntax.Identifier {
				// Identifier labels cannot contain quote or template punctuation.
				text = `"` + text + `"`
			}
			lowered.doc = document.Text(text)
		case syntax.Attribute:
			var err error
			lowered.doc, err = attribute(result, current.node)
			if err != nil {
				return document.Doc{}, err
			}
		}
		docs[current.node] = lowered
		stack = stack[:len(stack)-1]
	}
	return docs[result.Root()].doc, nil
}

// The closing line belongs outside Indent. The contents include an opening
// line only for nested bodies, allowing an opener's inline comment to stay put.
type bodyLayout struct {
	doc, end document.Doc
}

func attribute(result syntax.Result, node syntax.SyntaxNode) (document.Doc, error) {
	var parts []piece
	var trivia []syntax.SyntaxToken
	for i := range node.ChildCount() {
		element := node.Child(i)
		if child, ok := element.Node(); ok {
			value, err := lowerExpression(result, child)
			if err != nil {
				return document.Doc{}, err
			}
			parts = append(parts, piece{doc: value.doc, child: value, before: trivia})
		} else if token, ok := element.Token(); ok {
			if bodyTrivia(token.Kind()) {
				trivia = append(trivia, token)
				continue
			}
			parts = append(parts, piece{doc: document.Text(result.Text(token.Span())), token: true, kind: token.Kind(), before: trivia})
		}
		trivia = nil
	}
	before, separator := commentGap(result, parts[1].before, gapStyle{empty: space, beforeComment: space, afterComment: space})
	value := parts[2]
	gap, start := commentGap(result, value.before, gapStyle{empty: space, beforeComment: space, afterComment: space})
	return document.Concat(parts[0].doc, before,
		document.Cell(0, document.Concat(separator, parts[1].doc, gap, start, value.doc))), nil
}

func block(result syntax.Result, node syntax.SyntaxNode, docs map[syntax.SyntaxNode]bodyLayout) document.Doc {
	var header []piece
	var trivia []syntax.SyntaxToken
	var contents bodyLayout
	for i := range node.ChildCount() {
		element := node.Child(i)
		if child, ok := element.Node(); ok {
			if child.Kind() == syntax.Body {
				contents = docs[child]
				break
			}
			// Upstream rebuilds the label list: trivia before labels is removed,
			// while trivia between the final label and opening brace survives.
			header = append(header, piece{doc: docs[child].doc})
		} else if token, ok := element.Token(); ok {
			if bodyTrivia(token.Kind()) {
				trivia = append(trivia, token)
				continue
			}
			header = append(header, piece{doc: document.Text(result.Text(token.Span())), token: true, kind: token.Kind(), before: trivia})
		}
		trivia = nil
	}
	return document.Concat(spacedSequence(result, header), document.Indent(contents.doc), contents.end, document.Text("}"))
}

func body(result syntax.Result, node syntax.SyntaxNode, nested bool, docs map[syntax.SyntaxNode]bodyLayout) bodyLayout {
	var parts []document.Doc
	var trivia []syntax.SyntaxToken
	previous := syntax.InvalidNode
	nonempty := false
	for i := range node.ChildCount() {
		element := node.Child(i)
		if token, ok := element.Token(); ok {
			trivia = append(trivia, token)
			continue
		}
		child, _ := element.Node()
		parts = append(parts, bodyGap(result, trivia, previous, child.Kind(), nested), docs[child].doc)
		trivia = nil
		previous = child.Kind()
		nonempty = true
	}
	for _, token := range trivia {
		if token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment {
			nonempty = true
		}
	}
	parts = append(parts, bodyGap(result, trivia, previous, syntax.InvalidNode, nested))
	var end document.Doc
	if nonempty {
		end = document.HardLine()
	}
	return bodyLayout{doc: document.Concat(parts...), end: end}
}

// Body gaps own both item separators and comments. Blank lines depend on item
// kinds unless a standalone comment gives an explicit source section boundary.
func bodyGap(result syntax.Result, trivia []syntax.SyntaxToken, previous, next syntax.NodeKind, nested bool) document.Doc {
	var parts []document.Doc
	newlines := 0
	haveContent := previous != syntax.InvalidNode
	haveComment, standalone, lineComment := false, false, false
	blockBoundary := previous != syntax.InvalidNode && next != syntax.InvalidNode && (previous == syntax.Block || next == syntax.Block)
	lastNewline := -1
	for i, token := range trivia {
		if token.Kind() == syntax.Newline {
			lastNewline = i
		}
	}
	for i, token := range trivia {
		if token.Kind() == syntax.Newline {
			newlines++
			continue
		}
		if token.Kind() != syntax.LineComment && token.Kind() != syntax.BlockComment {
			continue
		}
		// A run can contain several block comments on the same line. They
		// share their section status, but a prefix sharing the next item's
		// line is not an independent comment section.
		isStandalone := (newlines > 0 || !haveContent && !nested || standalone) && (next == syntax.InvalidNode || i < lastNewline)
		separator := document.Text(" ")
		if newlines > 0 || lineComment {
			separator = document.HardLine()
			if haveContent && (newlines >= 2 && (isStandalone || standalone) || blockBoundary) {
				separator = document.Concat(separator, document.HardLine())
				blockBoundary = false
			}
		}
		if !haveContent && !nested {
			separator = document.Doc{}
		} else if !haveContent && nested && newlines > 0 {
			separator = document.HardLine()
		}
		comment := document.Concat(separator, literal(result.Text(token.Span())))
		if !isStandalone && token.Kind() == syntax.LineComment {
			comment = document.Cell(1, comment)
		}
		parts = append(parts, comment)
		haveContent, haveComment, standalone = true, true, isStandalone
		lineComment = token.Kind() == syntax.LineComment
		newlines = 0
	}
	if next != syntax.InvalidNode {
		separator := document.HardLine()
		if !haveContent && !nested {
			separator = document.Doc{}
		} else if haveComment && !lineComment && newlines == 0 {
			separator = document.Text(" ")
		}
		if blockBoundary || haveComment && standalone && newlines >= 2 {
			separator = document.Concat(document.HardLine(), document.HardLine())
		}
		parts = append(parts, separator)
	}
	return document.Concat(parts...)
}

func bodyTrivia(kind syntax.TokenKind) bool {
	return kind == syntax.Whitespace || kind == syntax.Newline || kind == syntax.LineComment || kind == syntax.BlockComment
}
