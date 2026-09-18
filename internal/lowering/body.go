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
		node       syntax.SyntaxNode
		next       int
		nestedBody bool
	}
	stack := []frame{{node: result.Root()}}
	docs := make(map[syntax.SyntaxNode]bodyLayout)
	for len(stack) != 0 {
		current := &stack[len(stack)-1]
		if current.node.Kind() != syntax.Attribute && current.next < current.node.ChildCount() {
			element := current.node.Child(current.next)
			current.next++
			if child, ok := element.Node(); ok {
				stack = append(stack, frame{node: child, nestedBody: current.node.Kind() == syntax.Block && child.Kind() == syntax.Body})
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
			lowered = body(result, current.node, current.nestedBody, docs)
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
			lowered, err = attribute(result, current.node)
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
	doc, end    document.Doc
	endsHeredoc bool
}

func attribute(result syntax.Result, node syntax.SyntaxNode) (bodyLayout, error) {
	var parts []piece
	var trivia []syntax.SyntaxToken
	for i := range node.ChildCount() {
		element := node.Child(i)
		if child, ok := element.Node(); ok {
			value, err := lowerExpression(result, child)
			if err != nil {
				return bodyLayout{}, err
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
	return bodyLayout{doc: assignment(result, parts, false), endsHeredoc: parts[len(parts)-1].child.endsHeredoc}, nil
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
			// Upstream drops comments before labels when rebuilding the header.
			// Carry them past all labels to the brace's stable trivia position.
			header = append(header, piece{doc: docs[child].doc})
			continue
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
	endsHeredoc := false
	nonempty := false
	for i := range node.ChildCount() {
		element := node.Child(i)
		if token, ok := element.Token(); ok {
			trivia = append(trivia, token)
			continue
		}
		child, _ := element.Node()
		parts = append(parts, bodyGap(result, trivia, previous, child.Kind(), nested, endsHeredoc), docs[child].doc)
		trivia = nil
		previous = child.Kind()
		endsHeredoc = docs[child].endsHeredoc
		nonempty = true
	}
	for _, token := range trivia {
		if token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment {
			nonempty = true
		}
	}
	parts = append(parts, bodyGap(result, trivia, previous, syntax.InvalidNode, nested, endsHeredoc))
	var end document.Doc
	if nonempty {
		end = document.HardLine()
	}
	return bodyLayout{doc: document.Concat(parts...), end: end}
}

// Body gaps own both item separators and comments. Classify each source gap
// before selecting its separator; comment placement and section policy are
// independent of document construction.
func bodyGap(result syntax.Result, trivia []syntax.SyntaxToken, previous, next syntax.NodeKind, nested, afterHeredoc bool) document.Doc {
	var parts []document.Doc
	gap := bodyGapClass{before: bodyGapSide{kind: bodyItem}, onOpener: nested && previous == syntax.InvalidNode}
	switch {
	case previous == syntax.InvalidNode && nested:
		gap.before.kind = bodyBlockStart
	case previous == syntax.InvalidNode:
		gap.before.kind = bodyFileStart
	case next == syntax.InvalidNode:
		// Trailing padding has no item boundary to preserve or insert.
	case previous == syntax.Block || next == syntax.Block:
		gap.boundary = bodyBlockBoundary
	default:
		gap.boundary = bodyAttributeBoundary
	}
	lastNewline := -1
	for i, token := range trivia {
		if token.Kind() == syntax.Newline {
			lastNewline = i
		}
	}
	for i, token := range trivia {
		if token.Kind() == syntax.Newline {
			gap.lines = min(gap.lines+1, 2)
			continue
		}
		if token.Kind() != syntax.LineComment && token.Kind() != syntax.BlockComment {
			continue
		}
		if afterHeredoc {
			// Unwrapping a template can expose a heredoc before an inline
			// attribute comment. Its end marker must occupy its own line.
			gap.lines = max(gap.lines, 1)
			afterHeredoc = false
		}
		// A run can contain several block comments on the same line. They
		// share their section status, but a prefix sharing the next item's
		// line is not an independent comment section.
		gap.after = bodyGapSide{
			kind:       bodyBlockComment,
			standalone: (gap.lines > 0 || gap.before.kind == bodyFileStart || gap.before.standalone) && (next == syntax.InvalidNode || i < lastNewline),
		}
		if token.Kind() == syntax.LineComment {
			gap.after.kind = bodyLineComment
		}
		separator := gap.separator()
		if separator == bodyLine || separator == bodyBlank {
			gap.onOpener = false
		}
		if separator == bodyBlank && gap.boundary == bodyBlockBoundary {
			// A block boundary inserts one blank line across the whole gap,
			// not another one after every intervening comment.
			gap.boundary = bodyOuterBoundary
		}
		comment := document.Concat(separator.doc(), commentLiteral(result, token))
		if !gap.after.standalone && token.Kind() == syntax.LineComment {
			comment = document.Cell(1, comment)
		}
		parts = append(parts, comment)
		gap.before = gap.after
		gap.lines = 0
	}
	if next != syntax.InvalidNode {
		gap.after = bodyGapSide{kind: bodyItem}
		parts = append(parts, gap.separator().doc())
	}
	return document.Concat(parts...)
}

type bodyBoundary uint8

const (
	bodyOuterBoundary bodyBoundary = iota
	bodyAttributeBoundary
	bodyBlockBoundary
)

type bodySideKind uint8

const (
	bodyFileStart bodySideKind = iota
	bodyBlockStart
	bodyItem
	bodyBlockComment
	bodyLineComment
)

type bodyGapSide struct {
	kind       bodySideKind
	standalone bool
}

// lines is a capped source newline count: zero means inline, one means adjacent
// lines, and two means a source blank line. Literal comment newlines stay opaque.
type bodyGapClass struct {
	lines         int
	before, after bodyGapSide
	boundary      bodyBoundary
	onOpener      bool
}

func (gap bodyGapClass) separator() bodySeparator {
	switch {
	case gap.before.kind == bodyFileStart:
		return bodyTight
	case gap.before.kind == bodyBlockStart:
		if gap.after.kind != bodyItem && gap.lines == 0 {
			return bodySpace // Keep a comment attached to the opening brace.
		}
		return bodyLine
	case gap.onOpener && gap.after.kind == bodyItem:
		// A multiline block cannot begin with an item on its opening line.
		// Block comments may stay there, but must not keep that item inline.
		return bodyLine
	case gap.boundary == bodyBlockBoundary && (gap.lines > 0 || gap.before.kind == bodyLineComment || gap.after.kind == bodyItem):
		return bodyBlank
	case gap.lines == 2 && (gap.boundary == bodyAttributeBoundary || gap.before.standalone || gap.after.standalone):
		return bodyBlank
	case gap.lines > 0 || gap.before.kind == bodyLineComment:
		return bodyLine
	case gap.before.kind == bodyItem && gap.after.kind == bodyItem:
		return bodyLine
	default:
		return bodySpace
	}
}

type bodySeparator uint8

const (
	bodyTight bodySeparator = iota
	bodySpace
	bodyLine
	bodyBlank
)

func (separator bodySeparator) doc() document.Doc {
	switch separator {
	case bodySpace:
		return document.Text(" ")
	case bodyLine:
		return document.HardLine()
	case bodyBlank:
		return document.Concat(document.HardLine(), document.HardLine())
	default:
		return document.Doc{}
	}
}

func bodyTrivia(kind syntax.TokenKind) bool {
	return kind == syntax.Whitespace || kind == syntax.Newline || kind == syntax.LineComment || kind == syntax.BlockComment
}
