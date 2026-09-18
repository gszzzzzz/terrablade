package lowering

import (
	"strings"

	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

type spacing uint8

const (
	tight spacing = iota
	soft
	line
)

func (s spacing) doc() document.Doc {
	switch s {
	case soft:
		return document.SoftLine()
	case line:
		return document.Line()
	default:
		return document.Doc{}
	}
}

// The final separator is returned separately so a closing delimiter can resume
// its parent's indentation, while preceding comments stay inside the contents.
func commentGap(result syntax.Result, trivia []syntax.SyntaxToken, fallback spacing) (document.Doc, document.Doc) {
	var parts []document.Doc
	newline, comment := false, false
	for _, token := range trivia {
		switch token.Kind() {
		case syntax.Newline:
			newline = true
		case syntax.LineComment, syntax.BlockComment:
			parts = append(parts, commentSeparator(newline, fallback), literal(result.Text(token.Span())))
			newline = token.Kind() == syntax.LineComment
			comment = true
		}
	}
	if !comment {
		return document.Doc{}, fallback.doc()
	}
	return document.Concat(parts...), commentSeparator(newline, fallback)
}

func commentSeparator(newline bool, fallback spacing) document.Doc {
	if newline {
		return document.HardLine()
	}
	if fallback != tight {
		return document.Line()
	}
	return document.Text(" ")
}

// Literal newlines suppress automatic indentation. CRLF is one newline; a
// standalone CR remains literal content rather than becoming a line break.
func literal(text string) document.Doc {
	var parts []document.Doc
	for {
		index := strings.IndexByte(text, '\n')
		if index < 0 {
			return document.Concat(append(parts, document.Text(text))...)
		}
		line := text[:index]
		line = strings.TrimSuffix(line, "\r")
		parts = append(parts, document.Text(line), document.LiteralLine())
		text = text[index+1:]
	}
}
