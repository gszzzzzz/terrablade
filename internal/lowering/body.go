package lowering

import (
	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// File lowers a complete native HCL file, including body comments and its final
// newline. result must come from a diagnostic-free parse: recovered input is
// never formatted, so File panics otherwise. Layout width and indentation are
// selected later by document.Render.
func File(result syntax.Result) document.Doc {
	if len(result.Diagnostics()) != 0 || result.Root().Kind() != syntax.File {
		panic("lowering: File requires a diagnostic-free parse")
	}
	return postOrder(fileWalker{result}, result.Root(), false).doc
}

// fileWalker lowers body-level nodes for File. Its context reports whether a
// Body belongs to a block rather than the file.
type fileWalker struct{ result syntax.Result }

func (fileWalker) expand(node syntax.Node, _ bool, children []visit[syntax.Node, bool]) []visit[syntax.Node, bool] {
	// lowerAttribute walks the value expression itself.
	if node.Kind() == syntax.Attribute {
		return children
	}
	for i := range node.ChildCount() {
		if child, ok := node.Child(i).Node(); ok {
			children = append(children, visit[syntax.Node, bool]{child, node.Kind() == syntax.Block})
		}
	}
	return children
}

func (w fileWalker) lower(node syntax.Node, nested bool, children []bodyLayout) bodyLayout {
	switch node.Kind() {
	case syntax.File:
		var parts []document.Doc
		for _, body := range children {
			parts = append(parts, body.doc, body.end)
		}
		return bodyLayout{doc: document.Concat(parts...)}
	case syntax.Body:
		return lowerBody(w.result, node, nested, children)
	case syntax.Block:
		return bodyLayout{doc: lowerBlock(w.result, node, children)}
	case syntax.BlockLabel:
		text := w.result.Text(node.Span())
		if token, _ := node.Child(0).Token(); token.Kind() == syntax.Identifier {
			// Identifier labels cannot contain quote or template
			// punctuation, so quoting the spelling verbatim yields a
			// valid string label.
			text = `"` + text + `"`
		}
		return bodyLayout{doc: document.Text(text)}
	default: // Attribute, the only other body-level kind
		return lowerAttribute(w.result, node)
	}
}

// bodyLayout is the lowered form of one body-level node.
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
	// inline comment. Set only for Attribute nodes and read by lowerBody.
	endsHeredoc bool
}

// lowerAttribute lowers name = value together with the trivia between its
// tokens. It is the only body-level node that holds an expression, so it is
// where the body walk hands off to lowerExpression.
func lowerAttribute(result syntax.Result, node syntax.Node) bodyLayout {
	var parts []piece
	var trivia []syntax.Token
	for i := range node.ChildCount() {
		element := node.Child(i)
		if child, ok := element.Node(); ok {
			value := lowerExpression(result, child)
			parts = append(parts, piece{doc: value.doc, child: value, before: trivia})
		} else if token, ok := element.Token(); ok {
			if token.Kind().IsTrivia() {
				trivia = append(trivia, token)
				continue
			}
			parts = append(parts, piece{doc: document.Text(result.Text(token.Span())), token: true, kind: token.Kind(), before: trivia})
		}
		trivia = nil
	}

	return bodyLayout{doc: lowerAssignment(result, parts, false), endsHeredoc: parts[len(parts)-1].child.endsHeredoc}
}

// lowerAssignment lays out key = value for a body attribute or an object
// item from its three pieces. The separator and value form one alignment
// cell so that consecutive rows pad their equals signs to a shared column
// (doc.go: Alignment). objectItem makes the cell conditional on the object
// breaking: a flat object shares its enclosing expression's row and must not
// align with its neighbors.
func lowerAssignment(result syntax.Result, pieces pieceList, objectItem bool) document.Doc {
	// An assignment is exactly three pieces, in this order.
	name, equals, value := pieces[0], pieces[1], pieces[2]
	afterName, beforeEquals := commentGap(result, equals.before, spacedGap(space))
	afterEquals, beforeValue := commentGap(result, value.before, spacedGap(space))
	tail := document.Concat(beforeEquals, equals.doc, afterEquals, beforeValue, value.doc)

	aligned := document.Cell(assignmentColumn, tail)
	if objectItem {
		// A flat object shares its enclosing expression's row. Only entries
		// in a broken object establish assignment columns of their own.
		aligned = document.IfBreak(aligned, tail)
	}
	// Comments between the name and the separator stay outside the cell, so
	// they cannot pad the shared assignment column.
	return document.Concat(name.doc, afterName, aligned)
}

// lowerBlock lowers a block header and its already-lowered body. Header
// comments are gathered into the trivia before the opening brace, and the
// body's closing brace stays outside the Indent (doc.go: Blocks).
func lowerBlock(result syntax.Result, node syntax.Node, children []bodyLayout) document.Doc {
	var header []piece
	var trivia []syntax.Token
	var contents bodyLayout
	for i := range node.ChildCount() {
		element := node.Child(i)
		if child, ok := element.Node(); ok {
			lowered := children[0]
			children = children[1:]
			if child.Kind() == syntax.Body {
				contents = lowered
				break
			}
			// Upstream drops comments before labels when rebuilding the header.
			// Carry them past all labels to the brace's stable trivia position.
			header = append(header, piece{doc: lowered.doc})
			continue
		} else if token, ok := element.Token(); ok {
			if token.Kind().IsTrivia() {
				trivia = append(trivia, token)
				continue
			}
			header = append(header, piece{doc: document.Text(result.Text(token.Span())), token: true, kind: token.Kind(), before: trivia})
		}
		trivia = nil
	}

	return document.Concat(spacedSequence(result, header), document.Indent(contents.doc), contents.end, document.Text("}"))
}

// lowerBody lowers the items of a file or block body. Each item is preceded
// by the gap that owns the trivia before it; the trailing gap after the last
// item owns any closing comments. nested distinguishes a block body, whose
// first gap starts on the opening brace line, from the file body.
func lowerBody(result syntax.Result, node syntax.Node, nested bool, children []bodyLayout) bodyLayout {
	var parts []document.Doc
	var trivia []syntax.Token
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
		lowered := children[0]
		children = children[1:]
		parts = append(parts, bodyGap(result, trivia, previous, child.Kind(), nested, endsHeredoc), lowered.doc)
		trivia = nil
		previous = child.Kind()
		endsHeredoc = lowered.endsHeredoc
		nonempty = true
	}

	for _, token := range trivia {
		if token.Kind().IsComment() {
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
func bodyGap(result syntax.Result, trivia []syntax.Token, previous, next syntax.NodeKind, nested, afterHeredoc bool) document.Doc {
	gap := newBodyGap(previous, next, nested)

	// A comment is standalone only if a newline follows it before the next
	// item; the last newline's position decides that for every comment.
	lastNewline := -1
	for i, token := range trivia {
		if token.Kind() == syntax.Newline {
			lastNewline = i
		}
	}

	var parts []document.Doc
	for i, token := range trivia {
		switch {
		case token.Kind() == syntax.Newline:
			gap.lines = min(gap.lines+1, 2)
		case token.Kind().IsComment():
			if afterHeredoc {
				// Unwrapping a template can expose a heredoc before an inline
				// attribute comment. Its end marker must occupy its own line.
				gap.lines = max(gap.lines, 1)
				afterHeredoc = false
			}
			ownsLine := next == syntax.InvalidNode || i < lastNewline
			parts = append(parts, gap.comment(result, token, ownsLine))
		}
	}

	if next != syntax.InvalidNode {
		gap.after = bodyGapSide{kind: bodyItem}
		parts = append(parts, gap.separator().doc())
	}
	return document.Concat(parts...)
}

// newBodyGap classifies the boundary between previous and next, either of
// which is InvalidNode at a body edge. A gap owes no separation until an item
// boundary is found.
func newBodyGap(previous, next syntax.NodeKind, nested bool) bodyGapState {
	gap := bodyGapState{
		before:   bodyGapSide{kind: bodyItem},
		boundary: bodyOuterBoundary,
		onOpener: nested && previous == syntax.InvalidNode,
	}
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
	return gap
}

// comment places one comment after the gap's before side and makes it the new
// before side. ownsLine reports that a newline follows the comment before the
// next item. A run can contain several block comments on the same line; they
// share their section status, but a prefix sharing the next item's line is
// not an independent comment section.
func (gap *bodyGapState) comment(result syntax.Result, token syntax.Token, ownsLine bool) document.Doc {
	gap.after = bodyGapSide{
		kind:       bodyBlockComment,
		standalone: ownsLine && (gap.lines > 0 || gap.before.kind == bodyFileStart || gap.before.standalone),
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
		gap.boundary = bodyBoundarySatisfied
	}
	comment := document.Concat(separator.doc(), commentLiteral(result, token))
	if !gap.after.standalone && token.Kind() == syntax.LineComment {
		comment = document.Cell(trailingCommentColumn, comment)
	}

	gap.before = gap.after
	gap.lines = 0
	return comment
}

// bodyBoundary classifies the items a gap separates, which fixes the minimum
// separation the gap must insert regardless of source layout.
type bodyBoundary uint8

const (
	// bodyOuterBoundary has no item boundary to honor: the gap is leading or
	// trailing body padding.
	bodyOuterBoundary bodyBoundary = iota
	// bodyAttributeBoundary separates two attributes. One source blank line
	// survives because it also splits alignment groups (doc.go: Alignment).
	bodyAttributeBoundary
	// bodyBlockBoundary separates items of which at least one is a block.
	// Exactly one blank line is inserted (doc.go: Item boundaries).
	bodyBlockBoundary
	// bodyBoundarySatisfied is a block boundary whose blank line has already
	// been emitted, so the rest of the gap has nothing left to insert.
	bodyBoundarySatisfied
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

// bodyGapState is the state bodyGap carries across one gap. It advances after
// every comment so that each separator decision sees only its two sides.
type bodyGapState struct {
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
func (gap *bodyGapState) separator() bodySeparator {
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

func (s bodySeparator) doc() document.Doc {
	switch s {
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
