package lowering

import (
	"strings"

	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

type spacing uint8

const (
	tight spacing = iota
	space
	soft
	line
	hard
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
	default:
		return document.Doc{}
	}
}

type gapStyle struct {
	empty, beforeComment, afterComment spacing
	blankLine                          bool
	requiredLine                       bool // A preceding heredoc marker must end its line.
}

// The final separator is returned separately so a closing delimiter can resume
// its parent's indentation, while preceding comments stay inside the contents.
// Comment prefix and suffix spacing differ: an inline comment follows a space,
// but a closer may follow it tightly. Source line breaks locate comments; only
// entry gaps opt into preserving one blank line when their group breaks.
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
			commentDoc := document.Concat(commentSeparator(newlines, lineComment || requiredLine, prefix, style.blankLine), literal(result.Text(token.Span())))
			if token.Kind() == syntax.LineComment && newlines == 0 && !lineComment && !requiredLine {
				commentDoc = document.Cell(1, commentDoc)
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

func commentSeparator(newlines int, lineComment bool, fallback spacing, blankLine bool) document.Doc {
	if blankLine && newlines >= 2 {
		return document.Concat(document.HardLine(), document.HardLine())
	}
	if lineComment || newlines > 0 {
		return document.HardLine()
	}
	return fallback.doc()
}

// Literal newlines suppress automatic indentation. CRLF is one newline; a
// standalone CR remains literal content rather than becoming a line break.
// Preserve a CRLF preceded by another CR: removing its CR would fuse the literal
// CR with LF and lose that content on a subsequent parse/format pass.
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
