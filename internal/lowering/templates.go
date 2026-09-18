package lowering

import (
	"strings"

	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

// Quoted templates, heredocs, and directive bodies share this literal/sequence
// composition. No synthesized whitespace escapes a sequence into literal text.
func templateParts(result syntax.Result, node syntax.SyntaxNode, docs map[syntax.SyntaxNode]layout) layout {
	parts := make([]document.Doc, 0, node.ChildCount())
	heredoc := false
	for i := 0; i < node.ChildCount(); i++ {
		child := node.Child(i)
		if token, ok := child.Token(); ok {
			text := result.Text(token.Span())
			parts = append(parts, literal(text))
			if token.Kind() == syntax.HeredocOpen {
				heredoc = true
			}
			if token.Kind() == syntax.HeredocEndMarker && strings.HasSuffix(text, "\r") && strings.HasPrefix(result.Source()[token.Span().End:], "\r\n") {
				// As with literal CR runs, retain the line-ending CR when removing
				// it would fuse the preceding literal CR with the enclosing LF.
				parts = append(parts, document.Text("\r"))
			}
		} else {
			nested, _ := child.Node()
			parts = append(parts, docs[nested].doc)
		}
	}
	// The marker's terminating newline belongs to the enclosing gap (or body).
	return layout{doc: document.Concat(parts...), endsHeredoc: heredoc}
}

func templateSequence(result syntax.Result, pieces []piece) document.Doc {
	start, end := 1, len(pieces)-1
	if pieces[start].token && pieces[start].kind == syntax.StripMarker {
		start++
	}
	if pieces[end-1].token && pieces[end-1].kind == syntax.StripMarker {
		end--
	}
	opener := sequence(result, pieces[:start])
	content := append([]piece(nil), pieces[start:end]...)
	leadingEdge, trailingEdge := tight, tight
	// A brace adjacent to a sequence boundary gets a visible separator, also
	// with strip markers. Comments supply their own token boundary instead.
	if content[0].child.startsBrace {
		leadingEdge = space
	}
	if content[len(content)-1].child.endsBrace {
		trailingEdge = space
	}
	leading, first := commentGap(result, content[0].before, gapStyle{empty: leadingEdge, beforeComment: soft, afterComment: space})
	content[0].before = nil
	trailing, last := commentGap(result, pieces[end].before, gapStyle{empty: trailingEdge, beforeComment: space, afterComment: soft, requiredLine: content[len(content)-1].child.endsHeredoc})
	closer := append([]piece(nil), pieces[end:]...)
	closer[0].before = nil
	// Width-driven newlines in sequences can change template indentation
	// semantics. Keep all nested groups flat, retaining only mandatory lines.
	return document.ForceFlat(document.Concat(opener,
		document.Indent(document.Concat(leading, first, spacedSequence(result, content), trailing)),
		last, sequence(result, closer)))
}
