package lowering

import (
	"strings"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// spacing is a separator a gap emits between two pieces.
type spacing uint8

const (
	tight spacing = iota // Nothing, as in foo.bar.
	space                // One space, as around a binary operator.
	soft                 // Nothing when flat; a newline when broken.
	line                 // A space when flat; a newline when broken.
	hard                 // Always a newline.
	// flatSpace is a space when flat and nothing when broken, where the
	// enclosing operation supplies the line break. It keeps a minus that
	// starts a line tight against its operand.
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

// gapStyle selects the separators commentGap emits for the trivia between
// two significant pieces.
type gapStyle struct {
	empty         spacing // Separator for a gap with no comment.
	beforeComment spacing // Before the first comment.
	afterComment  spacing // After the last comment, unless a line break is forced.
	// blankLine keeps one source blank line when the group breaks, as
	// between tuple and object entries.
	blankLine bool
	// requiredLine reports that the preceding piece ends in a heredoc, whose
	// end marker must be followed by a newline. It overrides empty and
	// beforeComment.
	requiredLine bool
}

// spacedGap surrounds any comment with spaces.
func spacedGap(empty spacing) gapStyle {
	return gapStyle{empty: empty, beforeComment: space, afterComment: space}
}

// openingGap follows an opening delimiter or clause keyword. A comment there
// hugs the delimiter when flat and starts the first line when broken.
func openingGap(edge spacing) gapStyle {
	return gapStyle{empty: edge, beforeComment: edge, afterComment: space}
}

// breakingGap precedes a closing delimiter or starts a new clause. After a
// comment, edge returns the closer or clause to the enclosing indentation.
func breakingGap(edge spacing, requiredLine bool) gapStyle {
	return gapStyle{empty: edge, beforeComment: space, afterComment: edge, requiredLine: requiredLine}
}

// Alignment columns for document.Cell: equals signs of consecutive
// assignments, and the line comments that end those rows.
const (
	assignmentColumn      uint8 = 0
	trailingCommentColumn uint8 = 1
)

// commentGap lays out the trivia between two significant pieces. It returns
// the comments and the final separator separately, so that a closing
// delimiter can outdent while the comments stay indented. A comment preceded
// by a source newline starts its own line; a line comment that stays on the
// line of the preceding content joins the trailing-comment column.
func commentGap(result syntax.Result, trivia []syntax.Token, style gapStyle) (document.Doc, document.Doc) {
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
			} else if token.Kind() == syntax.LineComment && newlines == 0 && (prefix == soft || prefix == line || prefix == hard) {
				// Keep a line comment on the opener's line, one space away.
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
			end = document.IfBreak(document.Concat(document.HardLine(), document.HardLine()), end)
		}
		return document.Doc{}, end
	}
	return document.Concat(parts...), commentSeparator(newlines, lineComment, style.afterComment, style.blankLine)
}

// commentLiteral renders one comment token. A line comment ending in a
// literal CR gains another CR, since the newline that follows would otherwise
// turn its CR into a CRLF line ending on the next parse.
func commentLiteral(result syntax.Result, token syntax.Token) document.Doc {
	text := result.Text(token.Span())
	comment := literal(text)
	if token.Kind() == syntax.LineComment && strings.HasSuffix(text, "\r") {
		comment = document.Concat(comment, document.Text("\r"))
	}
	return comment
}

// commentSeparator chooses the separator before a comment or after a comment
// run.
func commentSeparator(newlines int, lineComment bool, fallback spacing, blankLine bool) document.Doc {
	if blankLine && newlines >= 2 {
		return document.Concat(document.HardLine(), document.HardLine())
	}
	if lineComment || newlines > 0 {
		return document.HardLine()
	}
	return fallback.doc()
}

// literal renders multi-line text such as a comment or template chunk
// without reindenting it. CRLF becomes LF except after another CR, where the
// remaining CR would fuse with LF and be lost on the next pass.
func literal(text string) document.Doc {
	var parts []document.Doc
	for {
		newline := strings.IndexByte(text, '\n')
		if newline < 0 {
			return document.Concat(append(parts, document.Text(text))...)
		}

		chunk := text[:newline]
		if !strings.HasSuffix(chunk, "\r\r") {
			chunk = strings.TrimSuffix(chunk, "\r")
		}
		parts = append(parts, document.Text(chunk), document.LiteralLine())
		text = text[newline+1:]
	}
}
