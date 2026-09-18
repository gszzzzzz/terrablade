package lowering

import (
	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

func object(result syntax.Result, pieces []piece, inSequence bool) document.Doc {
	edge := line
	if !inSequence {
		// Source-only vertical layout is subordinate to template flattening.
		// Comment and heredoc hard lines remain mandatory in either context.
		for _, token := range pieces[1].before {
			if token.Kind() == syntax.Newline {
				edge = hard
				break
			}
		}
	}
	// Object newlines can replace commas in source. Materialize those separators
	// before sharing the list layout, leaving entry trivia in its original gap.
	withCommas := make([]piece, 0, len(pieces)*2)
	for i, part := range pieces {
		if i > 1 && !part.token && !pieces[i-1].token && !pieces[i-1].child.endsHeredoc {
			withCommas = append(withCommas, piece{doc: document.Text(","), token: true, kind: syntax.Comma})
		}
		withCommas = append(withCommas, part)
	}
	return delimited(result, withCommas, 0, true, edge)
}

func assignment(result syntax.Result, pieces []piece, objectItem bool) document.Doc {
	before, separator := commentGap(result, pieces[1].before, gapStyle{empty: space, beforeComment: space, afterComment: space})
	gap, start := commentGap(result, pieces[2].before, gapStyle{empty: space, beforeComment: space, afterComment: space})
	tail := document.Concat(separator, pieces[1].doc, gap, start, pieces[2].doc)
	aligned := document.Cell(0, tail)
	if objectItem {
		// A flat object shares its enclosing expression's row. Only entries
		// in a broken object establish assignment columns of their own.
		aligned = document.IfBreak(aligned, tail)
	}
	return document.Concat(pieces[0].doc, before, aligned)
}

func spacedSequence(result syntax.Result, pieces []piece) document.Doc {
	moveCommaTrivia(pieces)
	parts := make([]document.Doc, 0, len(pieces)*3)
	for i, part := range pieces {
		style := gapStyle{beforeComment: space, afterComment: space}
		style.requiredLine = i > 0 && pieces[i-1].child.endsHeredoc
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

func forExpression(result syntax.Result, pieces []piece) document.Doc {
	colon, condition := 0, len(pieces)-1
	for i, part := range pieces {
		if part.token && part.kind == syntax.Colon {
			colon = i
		}
		// After the colon, the only direct Identifier token is contextual if.
		if colon > 0 && part.token && part.kind == syntax.Identifier {
			condition = i
		}
	}
	edge := soft
	if pieces[0].kind == syntax.OpenBrace {
		edge = line
	}
	leading, start := commentGap(result, pieces[1].before, gapStyle{empty: edge, beforeComment: edge, afterComment: space})
	header := append([]piece(nil), pieces[1:colon+1]...)
	header[0].before = nil
	parts := []document.Doc{leading, start, spacedSequence(result, header)}
	projection := append([]piece(nil), pieces[colon+1:condition]...)
	gap, next := commentGap(result, projection[0].before, gapStyle{empty: line, beforeComment: space, afterComment: line})
	projection[0].before = nil
	parts = append(parts, gap, next, forProjection(result, projection))
	if condition < len(pieces)-1 {
		clause := append([]piece(nil), pieces[condition:len(pieces)-1]...)
		gap, next = commentGap(result, clause[0].before, gapStyle{empty: line, beforeComment: space, afterComment: line, requiredLine: projection[len(projection)-1].child.endsHeredoc})
		clause[0].before = nil
		parts = append(parts, gap, next, spacedSequence(result, clause))
	}
	close := pieces[len(pieces)-1]
	gap, end := commentGap(result, close.before, gapStyle{empty: edge, beforeComment: space, afterComment: edge, requiredLine: pieces[len(pieces)-2].child.endsHeredoc})
	parts = append(parts, gap)
	return document.Group(document.Concat(pieces[0].doc, document.Indent(document.Concat(parts...)), end, close.doc))
}

func forProjection(result syntax.Result, pieces []piece) document.Doc {
	if len(pieces) < 3 || !pieces[1].token || pieces[1].kind != syntax.Arrow {
		return spacedSequence(result, pieces)
	}
	gap, end := commentGap(result, pieces[1].before, gapStyle{empty: line, beforeComment: space, afterComment: line, requiredLine: pieces[0].child.endsHeredoc})
	tail := append([]piece(nil), pieces[1:]...)
	tail[0].before = nil
	return document.Group(document.Concat(pieces[0].doc,
		document.Indent(document.Concat(gap, end, spacedSequence(result, tail)))))
}
