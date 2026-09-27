package lowering

import (
	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// lowerObject lays out an object constructor. A source newline after the
// opening brace keeps the object broken even when it would fit, except inside
// a template sequence, which is always flat. Unlike brackets, braces keep
// inner spaces when flat: { a = 1 }.
func lowerObject(result syntax.Result, pieces pieceList, inSequence bool) document.Doc {
	edge := line
	if !inSequence {
		// For an empty object, inner is the closing brace.
		for _, token := range pieces.inner().before {
			if token.Kind() == syntax.Newline {
				edge = hard
				break
			}
		}
	}

	// Source objects may separate entries by newlines alone; supply the
	// commas. A heredoc's closing newline already separates entries, and a
	// comma after it would be invalid.
	withCommas := make([]piece, 0, len(pieces)*2)
	for i, part := range pieces {
		if i > 1 && !part.token && !pieces[i-1].token && !pieces[i-1].child.endsHeredoc {
			withCommas = append(withCommas, piece{doc: document.Text(","), token: true, kind: syntax.Comma})
		}
		withCommas = append(withCommas, part)
	}
	return delimited(result, withCommas, 0, true, edge)
}

// lowerForExpression lays out a tuple or object for expression. The header,
// the projection, and the optional if clause each start a line when the group
// breaks.
func lowerForExpression(result syntax.Result, pieces pieceList) document.Doc {
	colon, condition := 0, len(pieces)-1
	for i, part := range pieces {
		if part.token && part.kind == syntax.Colon {
			colon = i
		}
		// A conditional collection's colon is inside a child node, and after
		// the header's colon the only direct identifier is the if keyword.
		if colon > 0 && part.token && part.kind == syntax.Identifier {
			condition = i
		}
	}
	edge := soft
	if pieces.opener().kind == syntax.OpenBrace {
		edge = line
	}

	header, headerTrivia := detachLeading(pieces[1 : colon+1])
	leading, start := commentGap(result, headerTrivia, openingGap(edge))
	parts := []document.Doc{leading, start, spacedSequence(result, header)}

	projection, projectionTrivia := detachLeading(pieces[colon+1 : condition])
	gap, next := commentGap(result, projectionTrivia, breakingGap(line, false))
	parts = append(parts, gap, next, lowerForProjection(result, projection))

	if condition < len(pieces)-1 {
		clause, clauseTrivia := detachLeading(pieces[condition : len(pieces)-1])
		gap, next = commentGap(result, clauseTrivia, breakingGap(line, projection[len(projection)-1].child.endsHeredoc))
		parts = append(parts, gap, next, spacedSequence(result, clause))
	}

	closer := pieces.closer()
	gap, end := commentGap(result, closer.before, breakingGap(edge, pieces.beforeCloser().child.endsHeredoc))
	parts = append(parts, gap)
	return document.Group(document.Concat(pieces.opener().doc, document.Indent(document.Concat(parts...)), end, closer.doc))
}

// lowerForProjection lays out a value, or key => value for an object for. The
// arrow may start a continuation line.
func lowerForProjection(result syntax.Result, pieces []piece) document.Doc {
	if len(pieces) < 3 || !pieces[1].token || pieces[1].kind != syntax.Arrow {
		return spacedSequence(result, pieces)
	}

	key := pieces[0]
	tail, arrowTrivia := detachLeading(pieces[1:])
	gap, end := commentGap(result, arrowTrivia, breakingGap(line, key.child.endsHeredoc))
	return document.Group(document.Concat(key.doc, gap, end, spacedSequence(result, tail)))
}
