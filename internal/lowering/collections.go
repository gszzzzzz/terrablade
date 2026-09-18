package lowering

import (
	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

func object(result syntax.Result, pieces []piece) document.Doc {
	// Object newlines can replace commas in source. Materialize those separators
	// before sharing the list layout, leaving entry trivia in its original gap.
	withCommas := make([]piece, 0, len(pieces)*2)
	for i, part := range pieces {
		if i > 1 && !part.token && !pieces[i-1].token {
			withCommas = append(withCommas, piece{doc: document.Text(","), token: true, kind: syntax.Comma})
		}
		withCommas = append(withCommas, part)
	}
	return delimited(result, withCommas, 0, true, line)
}

func spacedSequence(result syntax.Result, pieces []piece) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	for i, part := range pieces {
		style := gapStyle{beforeComment: space, afterComment: space}
		if i > 0 {
			style.empty = space
		}
		if part.token && (part.kind == syntax.Comma || part.kind == syntax.Ellipsis) {
			style.empty, style.afterComment = tight, tight
		}
		gap, end := commentGap(result, part.before, style)
		parts = append(parts, gap, end, part.doc)
	}
	return document.Concat(parts...)
}
