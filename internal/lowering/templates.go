package lowering

import (
	"strings"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// templateParts lowers a quoted template, heredoc, or directive body: literal
// chunks alternate with already-lowered sequences. Quoted templates,
// heredocs, and directive bodies share this composition, and no synthesized
// whitespace escapes a sequence into literal text (doc.go: Templates).
func templateParts(result syntax.Result, node *expressionView, layouts map[*expressionView]layout) layout {
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
				// As with literal CR runs, retain the line-ending CR when
				// removing it would fuse the preceding literal CR with the
				// enclosing LF.
				parts = append(parts, document.Text("\r"))
			}
		} else {
			nested, _ := child.Node()
			parts = append(parts, layouts[nested].doc)
		}
	}

	// The marker's terminating newline belongs to the enclosing gap (or body).
	return layout{doc: document.Concat(parts...), endsHeredoc: heredoc}
}

// templateSequence lays out one ${ } interpolation or %{ } directive. Strip
// markers belong to the opener and closer so their spelling is preserved. The
// contents are forced flat, keeping only mandatory comment and heredoc lines
// (doc.go: Templates). An object brace adjacent to a boundary gets a space,
// as in ${ { key = value } }; a comment at the boundary already separates the
// tokens.
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
	// A brace adjacent to a sequence boundary gets a visible separator, also
	// with strip markers. Comments supply their own token boundary instead.
	if content[0].child.startsBrace {
		leadingEdge = space
	}
	if content[len(content)-1].child.endsBrace {
		trailingEdge = space
	}
	// Both boundary gaps break softly around a comment rather than taking
	// the brace space, since the comment itself already separates the tokens.
	leading, first := commentGap(result, contentTrivia, gapStyle{empty: leadingEdge, beforeComment: soft, afterComment: space})
	closer, closerTrivia := detachLeading(pieces[end:])
	trailing, last := commentGap(result, closerTrivia, gapStyle{empty: trailingEdge, beforeComment: space, afterComment: soft, requiredLine: content[len(content)-1].child.endsHeredoc})

	// Width-driven newlines in sequences can change template indentation
	// semantics. Keep all nested groups flat, retaining only mandatory lines.
	return document.ForceFlat(document.Concat(opener,
		document.Indent(document.Concat(leading, first, spacedSequence(result, content), trailing)),
		last, sequence(result, closer)))
}
