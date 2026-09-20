package lowering

import (
	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func syntheticParentheses(body document.Doc) document.Doc {
	return document.Group(document.Concat(
		document.IfBreak(document.Text("("), document.Doc{}),
		document.Indent(document.Concat(document.SoftLine(), body)),
		document.SoftLine(), document.IfBreak(document.Text(")"), document.Doc{}),
	))
}

// Operators start continuation lines at the surrounding delimiter's indentation,
// matching upstream. A same-precedence chain still shares one fit decision.
func operationContinuation(result syntax.Result, pieces []piece, endsHeredoc bool) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	minus, forcedMinus := false, false
	for _, part := range pieces {
		style := gapStyle{empty: space, beforeComment: space, afterComment: space, requiredLine: endsHeredoc}
		if part.token {
			style.empty, style.afterComment = line, line
		}
		if minus {
			// Upstream treats a line-leading minus as a unary token for spacing,
			// even though the parser still sees this as binary subtraction.
			style.empty = flatSpace
			if forcedMinus {
				style.empty = tight
			}
			for _, token := range part.before {
				if token.Kind() == syntax.LineComment {
					break
				}
				if token.Kind() == syntax.BlockComment {
					style.beforeComment = style.empty
					break
				}
			}
		}
		gap, end := commentGap(result, part.before, style)
		parts = append(parts, gap, end, part.doc)
		minus = part.token && part.kind == syntax.Minus
		forcedMinus = style.requiredLine || operationGapHasLine(part.before)
		endsHeredoc = part.child.endsHeredoc
	}
	return document.Concat(parts...)
}

func operationGapHasLine(trivia []syntax.SyntaxToken) bool {
	comment, newline := false, false
	for _, token := range trivia {
		switch token.Kind() {
		case syntax.LineComment:
			return true
		case syntax.BlockComment:
			comment = true
		case syntax.Newline:
			newline = true
		}
	}
	return comment && newline
}

func traversalSequence(result syntax.Result, pieces []piece, endsNumber, endsHeredoc bool) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	for _, part := range pieces {
		separator := tight
		if part.child.startsDot && endsNumber && part.child.fusesNumber {
			separator = space
		}
		// Dot and bracket steps never introduce a width-driven line break.
		// A retained comment supplies a numeric boundary and may require a line.
		gap, end := commentGap(result, part.before, gapStyle{empty: separator, beforeComment: space, afterComment: tight, requiredLine: endsHeredoc})
		parts = append(parts, gap, end, part.doc)
		endsNumber = part.child.endsNumber
		endsHeredoc = part.child.endsHeredoc
	}
	return document.Concat(parts...)
}

// syntax's number scanner crosses a dot only if it later consumes a digit or
// a complete exponent prefix. Ordinary names and splats cannot extend a number;
// legacy numeric indices and names beginning e/E[+-]?[0-9] can. Test the prefix,
// not the entire name: e2suffix would still swallow e2 into the numeric token.
// A minus can occur in an attribute name (e-2). A plus is a separate operator
// token in the CST: .e + 2 supplies only "e" here and needs no boundary space.
// The caller supplies a diagnostic-free attribute/index token, never empty.
func numberContinuesAcrossDot(next string) bool {
	if next[0] == 'e' || next[0] == 'E' {
		next = next[1:]
		if next != "" && (next[0] == '+' || next[0] == '-') {
			next = next[1:]
		}
	}
	return next != "" && next[0] >= '0' && next[0] <= '9'
}

func index(result syntax.Result, node *expressionView, pieces []piece) document.Doc {
	inner, close := pieces[1], pieces[2]
	atomic := false
	for i := range node.ChildCount() {
		if child, ok := node.Child(i).Node(); ok {
			atomic = child.Kind() == syntax.LiteralExpression || child.Kind() == syntax.VariableExpression
		}
	}
	for _, part := range pieces[1:] {
		for _, token := range part.before {
			if token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment {
				atomic = false
			}
		}
	}
	if atomic {
		// A long following traversal must not peel an indivisible index onto
		// a line of its own. There is no useful break inside [name] or [0].
		return document.Concat(pieces[0].doc, inner.doc, close.doc)
	}
	leading, start := commentGap(result, inner.before, gapStyle{empty: soft, beforeComment: soft, afterComment: space})
	trailing, end := commentGap(result, close.before, gapStyle{empty: soft, beforeComment: space, afterComment: soft, requiredLine: inner.child.endsHeredoc})
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
