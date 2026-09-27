package lowering

import (
	"strings"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// templateParts lowers a quoted template, heredoc, or directive body: literal
// chunks alternating with already-lowered sequences.
func templateParts(result syntax.Result, node *expressionView, children []layout) layout {
	parts := make([]document.Doc, 0, node.ChildCount())
	heredoc := false
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if token, ok := child.Token(); ok {
			text := token.spelling(result)
			parts = append(parts, literal(text))
			if token.Kind() == syntax.HeredocOpen {
				heredoc = true
			}
			if token.Kind() == syntax.HeredocEndMarker && strings.HasSuffix(text, "\r") && strings.HasPrefix(result.Source()[token.source.Span().End:], "\r\n") {
				// Keep the CR of a CRLF after a literal CR; see literal.
				parts = append(parts, document.Text("\r"))
			}
		} else {
			parts = append(parts, children[0].doc)
			children = children[1:]
		}
	}

	// The enclosing gap supplies the newline after the end marker.
	return layout{doc: document.Concat(parts...), endsHeredoc: heredoc}
}

// templateSequence lays out one ${ } interpolation or %{ } directive. An
// object brace next to a boundary gets a space, as in ${ { key = value } }.
func templateSequence(result syntax.Result, pieces []piece) document.Doc {
	start, end := 1, len(pieces)-1
	if pieces[start].token && pieces[start].kind == syntax.StripMarker {
		start++
	}
	if pieces[end-1].token && pieces[end-1].kind == syntax.StripMarker {
		end--
	}
	opener := sequence(result, pieces[:start])

	content, contentTrivia := detachLeading(pieces[start:end])
	leadingEdge, trailingEdge := tight, tight
	if content[0].child.startsBrace {
		leadingEdge = space
	}
	if content[len(content)-1].child.endsBrace {
		trailingEdge = space
	}
	leading, first := commentGap(result, contentTrivia, gapStyle{empty: leadingEdge, beforeComment: soft, afterComment: space})
	closer, closerTrivia := detachLeading(pieces[end:])
	trailing, last := commentGap(result, closerTrivia, gapStyle{empty: trailingEdge, beforeComment: space, afterComment: soft, requiredLine: content[len(content)-1].child.endsHeredoc})

	// A width-driven newline could change the template's indentation, so
	// keep the contents flat apart from mandatory lines.
	return document.ForceFlat(document.Concat(opener,
		document.Indent(document.Concat(leading, first, spacedSequence(result, content), trailing)),
		last, sequence(result, closer)))
}
