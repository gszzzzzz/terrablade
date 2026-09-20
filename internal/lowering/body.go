package lowering

import (
	"errors"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// File lowers a complete native HCL file, including body comments and its final
// newline. A zero Result or any parse diagnostic returns an error and an empty
// Doc. Layout width and indentation are selected later by document.Render.
//
// Bodies nest arbitrarily deep (TestDeepAndWideBodies lowers 20000 nested
// blocks), so the traversal is an explicit post-order stack rather than
// recursion. lowerExpression and normalizeExpression share this shape: each
// frame remembers the next child to visit, a node is lowered only after all
// of its children, and the results are keyed by node so a parent can collect
// them without revisiting the subtree.
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
		// Attributes are lowered whole by attribute, which walks the
		// expression itself; descending into them would lower the value twice.
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
				// Identifier labels cannot contain quote or template
				// punctuation, so quoting the spelling verbatim yields a
				// valid string label.
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

// bodyLayout is the lowered form of one body-level node, kept in File's map
// while the node's ancestors are still being lowered.
type bodyLayout struct {
	// doc is the node's rendered content, set for every node kind. For a
	// Body it excludes the closing line, which belongs outside the enclosing
	// block's Indent, and it begins with a line break only for nested bodies
	// so an opener's inline comment can stay on the brace line.
	doc document.Doc
	// end is a Body's final line break, emitted by the enclosing block after
	// its Indent or by the file at EOF. It stays empty for an empty nested
	// body, which renders as {}. Set only for Body nodes.
	end document.Doc
	// endsHeredoc reports an attribute whose value ends in a heredoc marker,
	// so the following body gap must supply the marker's newline before any
	// inline comment. Set only for Attribute nodes and read by body.
	endsHeredoc bool
}

// attribute lowers name = value together with the trivia between its tokens.
// The expression child is lowered through lowerExpression, so the attribute
// is the only body-level node whose lowering can fail.
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

// block lowers a block header and its already-lowered body. Header comments
// are gathered into the trivia before the opening brace, and the body's
// closing brace stays outside the Indent (doc.go: Blocks).
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

// body lowers the items of a file or block body. Each item is preceded by the
// gap that owns the trivia before it; the trailing gap after the last item
// owns any closing comments. nested distinguishes a block body, whose first
// gap starts on the opening brace line, from the file body.
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
	// A complete file always owns a final LF, including an empty file. Besides
	// being a stable textual-file convention, this is the common fixed point
	// of Terraform and OpenTofu: Terraform turns empty bytes into one LF, while
	// both tools preserve that LF. Empty nested bodies still render inline
	// as {}.
	if nonempty || !nested {
		end = document.HardLine()
	}
	return bodyLayout{doc: document.Concat(parts...), end: end}
}

// bodyGap lays out the trivia between two body items, or between an item and
// the body edge. Body gaps own both item separators and comments: each source
// gap is classified before its separators are selected, so comment placement
// and section policy stay independent of document construction.
//
// previous and next are the item kinds on either side, or InvalidNode at a
// body edge. The gap walks its comments in source order, choosing a separator
// before each one from the sides known so far and then making that comment
// the new before side, so a later decision never rescans earlier comments.
// afterHeredoc reports that previous ends in a heredoc marker.
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

	// A comment is standalone only if a newline follows it before the next
	// item; the last newline's position decides that for every comment.
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
			// not another one after every intervening comment. Once emitted,
			// the rest of the gap has nothing left to insert, which is exactly
			// what bodyOuterBoundary means for edge padding.
			gap.boundary = bodyOuterBoundary
		}
		comment := document.Concat(separator.doc(), commentLiteral(result, token))
		if !gap.after.standalone && token.Kind() == syntax.LineComment {
			comment = document.Cell(trailingCommentColumn, comment)
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

// bodyBoundary classifies the items a gap separates, which fixes the minimum
// separation the gap must insert regardless of source layout.
type bodyBoundary uint8

const (
	// bodyOuterBoundary has no item boundary to honor: the gap is leading or
	// trailing body padding. bodyGap also assigns it to a block boundary once
	// that boundary's blank line has been emitted.
	bodyOuterBoundary bodyBoundary = iota
	// bodyAttributeBoundary separates two attributes. One source blank line
	// survives because it also splits alignment groups (doc.go: Alignment).
	bodyAttributeBoundary
	// bodyBlockBoundary separates items of which at least one is a block.
	// Exactly one blank line is inserted (doc.go: Item boundaries).
	bodyBlockBoundary
)

// bodySideKind identifies what lies on one side of a separator: a body edge,
// an item, or a comment already placed earlier in the same gap.
type bodySideKind uint8

const (
	bodyFileStart  bodySideKind = iota // The start of the top-level body.
	bodyBlockStart                     // The opening brace of a nested body.
	bodyItem                           // An attribute or a block.
	bodyBlockComment
	bodyLineComment
)

// bodyGapSide describes one side of a separator decision.
type bodyGapSide struct {
	kind bodySideKind
	// standalone marks a comment on lines of its own: it starts a line and no
	// item shares its last line. Standalone comments form sections that keep
	// a source blank line on either side, whereas a comment prefixing an item
	// belongs to that item. Meaningful only for the comment kinds.
	standalone bool
}

// bodyGapClass is the state bodyGap carries across one gap. It advances after
// every comment so that each separator decision sees only its two sides.
type bodyGapClass struct {
	// lines is a capped source newline count since the before side: zero
	// means inline, one means adjacent lines, and two means a source blank
	// line. Literal comment newlines stay opaque.
	lines         int
	before, after bodyGapSide
	boundary      bodyBoundary
	// onOpener reports that the gap still sits on a nested body's opening
	// brace line, where a comment may stay but an item may not. It clears
	// once a separator has broken the line.
	onOpener bool
}

// separator chooses the separator between the gap's before and after sides.
// The cases implement doc.go's item-boundary policies in priority order:
// body edges first, then the mandatory blank line around blocks, then source
// blank lines that attribute groups and comment sections preserve, and last
// the line breaks that source newlines and line comments force.
func (gap bodyGapClass) separator() bodySeparator {
	switch {
	case gap.before.kind == bodyFileStart:
		return bodyTight // Outer padding is removed (doc.go: File boundaries).
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
		// The blank line lands at the first line break in the gap, or right
		// before the item, so an inline comment after the previous item
		// stays on its line.
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

// bodySeparator is the separator a body gap emits between two sides.
type bodySeparator uint8

const (
	bodyTight bodySeparator = iota
	bodySpace
	bodyLine
	bodyBlank // One blank line: two hard line breaks.
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

// bodyTrivia reports whether a token carries no syntax of its own. Despite
// the name it applies to every token stream in the package: the expression
// view reuses it to skip the same kinds.
func bodyTrivia(kind syntax.TokenKind) bool {
	return kind == syntax.Whitespace || kind == syntax.Newline || kind == syntax.LineComment || kind == syntax.BlockComment
}
