package lowering

import (
	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// File lowers a complete native HCL file, including its comments and final
// newline. It panics if result has diagnostics: recovered input is never
// formatted.
func File(result syntax.Result) document.Doc {
	if len(result.Diagnostics()) != 0 || result.Root().Kind() != syntax.File {
		panic("lowering: File requires a diagnostic-free parse")
	}
	return postOrder(fileWalker{result}, result.Root(), false).doc
}

// fileWalker lowers body-level nodes. Its context reports whether a Body
// belongs to a block.
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
			// An identifier needs no escaping inside quotes.
			text = `"` + text + `"`
		}
		return bodyLayout{doc: document.Text(text)}
	default: // Attribute, the only other body-level kind
		return lowerAttribute(w.result, node)
	}
}

// bodyLayout is the lowered form of one body-level node.
type bodyLayout struct {
	// doc excludes a Body's final line break, which must fall outside the
	// enclosing block's Indent. A nested body's doc starts with its first
	// gap, so a comment after the opening brace can stay on that line.
	doc document.Doc
	// end is a Body's final line break; empty for an empty nested body,
	// which renders as {}.
	end document.Doc
	// endsHeredoc reports an attribute whose value ends in a heredoc, so the
	// next gap must break the line before any inline comment.
	endsHeredoc bool
}

// lowerAttribute lowers name = value and the trivia between its tokens.
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
// item. The separator and value form one cell so that consecutive rows align
// their equals signs.
func lowerAssignment(result syntax.Result, pieces pieceList, objectItem bool) document.Doc {
	name, equals, value := pieces[0], pieces[1], pieces[2]
	afterName, beforeEquals := commentGap(result, equals.before, spacedGap(space))
	afterEquals, beforeValue := commentGap(result, value.before, spacedGap(space))
	tail := document.Concat(beforeEquals, equals.doc, afterEquals, beforeValue, value.doc)

	aligned := document.Cell(assignmentColumn, tail)
	if objectItem {
		// A flat object shares its row with other text; only a broken
		// object's entries align.
		aligned = document.IfBreak(aligned, tail)
	}
	// Comments before the equals sign stay outside the cell and so do not
	// widen the column.
	return document.Concat(name.doc, afterName, aligned)
}

// lowerBlock lowers a block header and its already-lowered body.
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
			// Upstream drops comments before labels, so move them to the
			// opening brace, where it keeps them.
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

// lowerBody lowers the items of a file or block body, each preceded by the
// gap holding the trivia before it. The last gap holds any trailing comments.
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
	// A file, even an empty one, ends in LF: Terraform turns empty input into
	// one LF, and both Terraform and OpenTofu preserve it.
	if nonempty || !nested {
		end = document.HardLine()
	}
	return bodyLayout{doc: document.Concat(parts...), end: end}
}

// bodyGap lays out the trivia between two body items, including item
// separators and comments. previous and next are the kinds of the items on
// either side, or InvalidNode at a body edge. afterHeredoc reports that
// previous ends in a heredoc.
//
// Comments are placed in source order, each separator chosen from the two
// sides known so far; the comment then becomes the before side.
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
				// Unwrapping a template can leave a heredoc before an
				// inline comment; its end marker needs its own line.
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

// newBodyGap classifies the boundary between previous and next.
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

// comment places a comment after the gap's before side and makes it the new
// before side. ownsLine reports that a newline follows it before the next
// item.
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
		// A block boundary needs one blank line per gap, not per comment.
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

// bodyBoundary classifies the items a gap separates.
type bodyBoundary uint8

const (
	bodyOuterBoundary     bodyBoundary = iota // Leading or trailing padding.
	bodyAttributeBoundary                     // Keeps at most one source blank line.
	bodyBlockBoundary                         // Adjoins a block; needs one blank line.
	bodyBoundarySatisfied                     // A block boundary already separated.
)

// bodySideKind identifies what lies on one side of a separator.
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
	// standalone marks a comment on lines of its own. Such comments keep a
	// source blank line on either side, whereas a comment that shares a line
	// with an item belongs to that item.
	standalone bool
}

// bodyGapState is the state bodyGap carries across one gap.
type bodyGapState struct {
	// lines counts source newlines since the before side, capped at 2
	// (a blank line).
	lines         int
	before, after bodyGapSide
	boundary      bodyBoundary
	// onOpener reports that the gap is still on a nested body's opening
	// brace line, where a comment may stay but an item may not.
	onOpener bool
}

// separator chooses the separator between the gap's before and after sides.
// The cases are in priority order: body edges, the blank line around blocks,
// preserved source blank lines, then line breaks forced by the source.
func (gap *bodyGapState) separator() bodySeparator {
	switch {
	case gap.before.kind == bodyFileStart:
		return bodyTight
	case gap.before.kind == bodyBlockStart:
		if gap.after.kind != bodyItem && gap.lines == 0 {
			return bodySpace // Keep a comment attached to the opening brace.
		}
		return bodyLine
	case gap.onOpener && gap.after.kind == bodyItem:
		// An item never shares the opening brace line.
		return bodyLine
	case gap.boundary == bodyBlockBoundary && (gap.lines > 0 || gap.before.kind == bodyLineComment || gap.after.kind == bodyItem):
		// Place the blank line at the first line break, so an inline
		// comment after the previous item stays on its line.
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
