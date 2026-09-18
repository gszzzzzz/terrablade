package lowering

import (
	"terrablade/internal/document"
	"terrablade/internal/syntax"
)

func syntheticParentheses(body document.Doc) document.Doc {
	return document.Group(document.Concat(
		document.IfBreak(document.Text("("), document.Doc{}),
		document.Indent(document.Concat(document.SoftLine(), body)),
		document.SoftLine(), document.IfBreak(document.Text(")"), document.Doc{}),
	))
}

// Operators start continuation lines. Every same-precedence binary on the left
// shares this group via its ungrouped body, avoiding a staircase or rescanning
// a growing chain. Different precedence and conditional arms group independently.
func operationSequence(result syntax.Result, pieces []piece) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	for i, part := range pieces {
		style := gapStyle{beforeComment: space, afterComment: space}
		if i > 0 {
			style.empty = space
			if part.kind != syntax.Invalid {
				style.empty, style.afterComment = line, line
			}
		}
		gap, end := commentGap(result, part.before, style)
		parts = append(parts, gap, end, part.doc)
	}
	return document.Concat(parts...)
}

func traversalSequence(result syntax.Result, pieces []piece, endsNumber bool) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	for _, part := range pieces {
		separator := soft
		if endsNumber && part.child.startsDot {
			// A numeric token followed by a dot can become another number token:
			// foo.0 .0 must not collapse into the invalid candidate foo.0.0.
			separator = line
		}
		gap, end := commentGap(result, part.before, gapStyle{empty: separator, beforeComment: space, afterComment: separator})
		parts = append(parts, gap, end, part.doc)
		endsNumber = part.child.endsNumber
	}
	return document.Concat(parts...)
}

func index(result syntax.Result, pieces []piece) document.Doc {
	inner, close := pieces[1], pieces[2]
	leading, start := commentGap(result, inner.before, gapStyle{empty: soft, beforeComment: soft, afterComment: space})
	trailing, end := commentGap(result, close.before, gapStyle{empty: soft, beforeComment: space, afterComment: soft})
	return document.Group(document.Concat(pieces[0].doc,
		document.Indent(document.Concat(leading, start, inner.doc, trailing)), end, close.doc))
}

func binaryPower(kind syntax.TokenKind) int {
	switch kind {
	case syntax.Or:
		return 1
	case syntax.And:
		return 2
	case syntax.EqualEqual, syntax.NotEqual:
		return 3
	case syntax.Less, syntax.LessEqual, syntax.Greater, syntax.GreaterEqual:
		return 4
	case syntax.Plus, syntax.Minus:
		return 5
	default: // Star, Slash, Percent: the highest binary precedence.
		return 6
	}
}
