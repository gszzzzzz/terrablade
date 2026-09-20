package lowering

import (
	"strings"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// spacing names the separator a gap emits at one position: when it holds no
// comment, before its first comment, or after its last one. Each value maps
// onto one document primitive; gapStyle combines three of them per gap.
type spacing uint8

const (
	// tight emits nothing, so the neighboring tokens touch, as in foo.bar.
	tight spacing = iota
	// space emits one space in every layout, as around binary operators.
	space
	// soft emits nothing while the enclosing group fits and a newline once
	// it breaks, as directly inside a delimiter pair.
	soft
	// line emits a space while the enclosing group fits and a newline once
	// it breaks, as between the entries of a delimited list.
	line
	// hard always emits a newline, as after a line comment or heredoc marker.
	hard
	// flatSpace emits a space only while the enclosing operation stays on
	// one line and nothing once it breaks. The operation layout supplies the
	// line break itself, so removing the space is what makes a line-leading
	// minus sit tight against its operand.
	flatSpace
)

func (s spacing) doc() document.Doc {
	switch s {
	case space:
		return document.Text(" ")
	case soft:
		return document.SoftLine()
	case line:
		return document.Line()
	case hard:
		return document.HardLine()
	case flatSpace:
		return document.IfBreak(document.Doc{}, document.Text(" "))
	default:
		return document.Doc{}
	}
}

// gapStyle selects the separators commentGap emits for one source gap, the
// trivia between two significant pieces. Every layout function builds one
// per gap; the presets below cover the recurring shapes, and one-off shapes
// stay inline where they are used.
type gapStyle struct {
	// empty is the separator for a gap with no comment. It is the common
	// case and decides the gap's flat and broken behavior.
	empty spacing
	// beforeComment precedes the first comment in the gap. Later comments in
	// the same run always follow one space.
	beforeComment spacing
	// afterComment follows the last comment in the gap unless a source
	// newline or a line comment already forces a line break there.
	afterComment spacing
	// blankLine preserves one source blank line as an output blank line when
	// the enclosing group breaks. Only broken tuple and object entry gaps opt
	// in; calls and other gaps collapse blank lines (doc.go: Blank lines).
	blankLine bool
	// requiredLine reports that the piece before the gap ends in a heredoc,
	// whose end marker must be followed by a newline before any other token.
	// It replaces empty and beforeComment with a hard line.
	requiredLine bool
}

// spacedGap surrounds any comment with spaces and emits empty otherwise. It
// serves tokens that share one line with their neighbors: operands, names,
// and the pieces of an assignment.
func spacedGap(empty spacing) gapStyle {
	return gapStyle{empty: empty, beforeComment: space, afterComment: space}
}

// openingGap follows an opening delimiter or clause keyword. A comment there
// takes the same edge separator as empty content, so it hugs the delimiter
// while flat and starts the first content line once broken, and one space
// separates it from the content that follows.
func openingGap(edge spacing) gapStyle {
	return gapStyle{empty: edge, beforeComment: edge, afterComment: space}
}

// breakingGap precedes a closing delimiter or starts a new clause. A comment
// there follows one space, and the edge separator resumes after it so the
// closer or clause returns to the enclosing indentation.
func breakingGap(edge spacing, requiredLine bool) gapStyle {
	return gapStyle{empty: edge, beforeComment: space, afterComment: edge, requiredLine: requiredLine}
}

// Alignment columns passed to document.Cell. Consecutive rows sharing a column
// pad to a common width (doc.go: Alignment): assignmentColumn aligns the
// equals signs of adjacent assignments and trailingCommentColumn aligns the
// line comments that end those rows. Body attributes and object entries use
// the same pair. A run ends at a row with no cell in that column and at a cell
// that spans more than one row; a line break between rows keeps it going.
const (
	assignmentColumn      uint8 = 0
	trailingCommentColumn uint8 = 1
)

// commentGap lays out the trivia between two significant pieces. It returns
// the comment run and the final separator separately so a closing delimiter
// can resume its parent's indentation while the comments stay inside the
// indented contents.
//
// The loop carries three facts about the run so far. newlines counts source
// newlines since the previous comment or the gap start; a comment preceded by
// one must start its own line, because source line breaks locate comments
// (doc.go: Comments). lineComment records that the previous comment ended its
// line, which forces a hard line before whatever follows. comment records
// that a run has begun, after which every further comment follows one space
// regardless of style.beforeComment. requiredLine is consumed by the first
// separator emitted: the heredoc marker before the gap needs exactly one
// newline, not one per comment.
//
// A line comment joins the trailing-comment alignment column only when it
// renders on the same line as the content before the gap: no source newline
// precedes it, no earlier line comment already ended the line, and no heredoc
// newline is pending. Any other line comment is standalone, and padding it to
// the column would misalign the rows around it (doc.go: Alignment).
func commentGap(result syntax.Result, trivia []syntax.SyntaxToken, style gapStyle) (document.Doc, document.Doc) {
	var parts []document.Doc
	newlines := 0
	comment, lineComment := false, false
	requiredLine := style.requiredLine

	for _, token := range trivia {
		switch token.Kind() {
		case syntax.Newline:
			newlines++
		case syntax.LineComment, syntax.BlockComment:
			prefix := style.beforeComment
			if comment {
				prefix = space
			}
			commentDoc := document.Concat(commentSeparator(newlines, lineComment || requiredLine, prefix, style.blankLine), commentLiteral(result, token))
			if token.Kind() == syntax.LineComment && newlines == 0 && !lineComment && !requiredLine {
				commentDoc = document.Cell(trailingCommentColumn, commentDoc)
			}
			parts = append(parts, commentDoc)

			newlines = 0
			requiredLine = false
			lineComment = token.Kind() == syntax.LineComment
			comment = true
		}
	}

	if !comment {
		end := style.empty.doc()
		if requiredLine {
			end = document.HardLine()
		}
		if style.blankLine && newlines >= 2 {
			// A flat list collapses source blank lines; only the broken
			// layout has entry lines for a blank line to separate.
			end = document.IfBreak(document.Concat(document.HardLine(), document.HardLine()), end)
		}
		return document.Doc{}, end
	}
	return document.Concat(parts...), commentSeparator(newlines, lineComment, style.afterComment, style.blankLine)
}

// commentLiteral renders one comment token. Every line comment is followed by
// a newline supplied by its enclosing gap or body. Protect a final literal CR
// from becoming part of that line ending: the lexer excludes the CR of a CRLF
// from the comment but retains every earlier CR, so without the extra CR a
// second pass would read the comment as one CR shorter. This also covers an
// EOF comment that did not originally have a line ending.
func commentLiteral(result syntax.Result, token syntax.SyntaxToken) document.Doc {
	text := result.Text(token.Span())
	comment := literal(text)
	if token.Kind() == syntax.LineComment && strings.HasSuffix(text, "\r") {
		comment = document.Concat(comment, document.Text("\r"))
	}
	return comment
}

// commentSeparator chooses the separator before a comment or after a comment
// run. A source newline or a preceding line comment makes the line break
// mandatory; otherwise the caller's fallback spacing applies.
func commentSeparator(newlines int, lineComment bool, fallback spacing, blankLine bool) document.Doc {
	if blankLine && newlines >= 2 {
		return document.Concat(document.HardLine(), document.HardLine())
	}
	if lineComment || newlines > 0 {
		return document.HardLine()
	}
	return fallback.doc()
}

// literal renders opaque multi-line text such as a comment or template chunk.
// Literal newlines suppress automatic indentation. CRLF is one newline; a
// standalone CR remains literal content rather than becoming a line break.
// Preserve a CRLF preceded by another CR: removing its CR would fuse the
// literal CR with LF and lose that content on a subsequent parse/format pass.
func literal(text string) document.Doc {
	var parts []document.Doc
	for {
		index := strings.IndexByte(text, '\n')
		if index < 0 {
			return document.Concat(append(parts, document.Text(text))...)
		}

		line := text[:index]
		if !strings.HasSuffix(line, "\r\r") {
			line = strings.TrimSuffix(line, "\r")
		}
		parts = append(parts, document.Text(line), document.LiteralLine())
		text = text[index+1:]
	}
}
