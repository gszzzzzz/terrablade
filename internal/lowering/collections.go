package lowering

import (
	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// lowerObject lays out an object constructor through delimited. A source
// newline after the opening brace keeps the object vertical even when it
// would fit, except inside a template sequence, which flattens every
// source-only choice (doc.go: Objects). Braces keep inner spaces while flat,
// so the edge is line rather than the soft edge brackets use.
func lowerObject(result syntax.Result, pieces pieceList, inSequence bool) document.Doc {
	edge := line
	if !inSequence {
		// Source-only vertical layout is subordinate to template flattening.
		// Comment and heredoc hard lines remain mandatory in either context.
		// An empty object has no first entry, so inner is then the closing
		// brace, whose leading trivia is the same gap.
		for _, token := range pieces.inner().before {
			if token.Kind() == syntax.Newline {
				edge = hard
				break
			}
		}
	}

	// Object newlines can replace commas in source. Materialize those
	// separators before sharing the list layout, leaving entry trivia in its
	// original gap. A heredoc marker's newline is already a separator, and a
	// comma after it would be invalid (doc.go: Objects).
	withCommas := make([]piece, 0, len(pieces)*2)
	for i, part := range pieces {
		if i > 1 && !part.token && !pieces[i-1].token && !pieces[i-1].child.endsHeredoc {
			withCommas = append(withCommas, piece{doc: document.Text(","), token: true, kind: syntax.Comma})
		}
		withCommas = append(withCommas, part)
	}
	return delimited(result, withCommas, 0, true, edge)
}

// lowerAssignment lays out key = value for a body attribute or an object
// item from its three pieces. The separator and value form one alignment
// cell so that consecutive rows pad their equals signs to a shared column
// (doc.go: Alignment). objectItem makes the cell conditional on the object
// breaking: a flat object shares its enclosing expression's row and must not
// align with its neighbors (TestObjectLayouts: flat entries do not align).
func lowerAssignment(result syntax.Result, pieces pieceList, objectItem bool) document.Doc {
	// An assignment is exactly three pieces, in this order.
	name, equals, value := pieces[0], pieces[1], pieces[2]
	afterName, beforeEquals := commentGap(result, equals.before, spacedGap(space))
	afterEquals, beforeValue := commentGap(result, value.before, spacedGap(space))
	tail := document.Concat(beforeEquals, equals.doc, afterEquals, beforeValue, value.doc)

	aligned := document.Cell(assignmentColumn, tail)
	if objectItem {
		// A flat object shares its enclosing expression's row. Only entries
		// in a broken object establish assignment columns of their own.
		aligned = document.IfBreak(aligned, tail)
	}
	// Comments between the name and the separator stay outside the cell, so
	// they cannot pad the shared assignment column.
	return document.Concat(name.doc, afterName, aligned)
}

// spacedSequence joins pieces with single spaces, as in a block header or a
// for clause. Commas and ellipses attach to the piece before them, and their
// leading trivia moves after them first so a comment never separates a value
// from its comma.
func spacedSequence(result syntax.Result, pieces []piece) document.Doc {
	moveCommaTrivia(pieces)
	parts := make([]document.Doc, 0, len(pieces)*3)
	for i, part := range pieces {
		style := spacedGap(tight)
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

// lowerForExpression lays out a tuple or object for expression. The header (for
// bindings in collection :), the projection, and the optional if clause each
// start a continuation line once the group breaks (doc.go: For expressions).
// The last direct colon ends the header, because a conditional collection's
// own colon sits inside a child node, and after that colon the only direct
// Identifier token is the contextual if keyword. An object for uses the line
// edge so its braces keep inner spaces while flat, as object entries do; a
// tuple for uses the soft edge of brackets.
func lowerForExpression(result syntax.Result, pieces pieceList) document.Doc {
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

// lowerForProjection lays out the projection: a single value, or key => value
// for an object for. The arrow may start a continuation line at the same
// indent (doc.go: For expressions), so its gap is laid out here as a clause
// break and the arrow then heads one spaced sequence with the value.
func lowerForProjection(result syntax.Result, pieces []piece) document.Doc {
	// An object projection is key, arrow, value; anything else is one value.
	if len(pieces) < 3 || !pieces[1].token || pieces[1].kind != syntax.Arrow {
		return spacedSequence(result, pieces)
	}

	key := pieces[0]
	tail, arrowTrivia := detachLeading(pieces[1:])
	gap, end := commentGap(result, arrowTrivia, breakingGap(line, key.child.endsHeredoc))
	return document.Group(document.Concat(key.doc, gap, end, spacedSequence(result, tail)))
}
