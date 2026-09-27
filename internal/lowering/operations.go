package lowering

import (
	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// breakParentheses wraps an operation in parentheses that print only when it
// breaks, for a position where the grammar forbids newlines.
func breakParentheses(body document.Doc) document.Doc {
	return document.Group(document.Concat(
		document.IfBreak(document.Text("("), document.Doc{}),
		document.Indent(document.Concat(document.SoftLine(), body)),
		document.SoftLine(), document.IfBreak(document.Text(")"), document.Doc{}),
	))
}

// operationContinuation lays out the operators and operands after the head of
// a binary or conditional expression. Each operator starts a continuation
// line when the group breaks.
//
// Upstream spaces a minus that starts a line as a unary minus, tight against
// its operand. A minus forced onto a new line by a heredoc or comment is
// tight even in a flat layout.
func operationContinuation(result syntax.Result, pieces []piece, endsHeredoc bool) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	minus, forcedMinus := false, false
	for _, part := range pieces {
		style := spacedGap(space)
		style.requiredLine = endsHeredoc
		if part.token {
			style.empty, style.afterComment = line, line
		}
		if minus {
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

// operationGapHasLine reports whether trivia forces a line break: it holds a
// line comment, or a block comment and a newline.
func operationGapHasLine(trivia []syntax.Token) bool {
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

// traversalSequence lays out traversal steps after their root. Only a
// comment or heredoc puts a step on a new line. endsNumber and endsHeredoc
// describe the piece before the first step.
func traversalSequence(result syntax.Result, pieces []piece, endsNumber, endsHeredoc bool) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	for _, part := range pieces {
		separator := tight
		if part.child.startsDot && endsNumber && part.child.fusesNumber {
			// 1.e2 is one number but 1 .e2 is a traversal.
			separator = space
		}
		gap, end := commentGap(result, part.before, gapStyle{empty: separator, beforeComment: space, afterComment: tight, requiredLine: endsHeredoc})
		parts = append(parts, gap, end, part.doc)

		endsNumber = part.child.endsNumber
		endsHeredoc = part.child.endsHeredoc
	}
	return document.Concat(parts...)
}

// lowerIndex lays out one bracketed index step. An index holding only a
// literal or name never breaks: there is no useful break in [0] or [name].
func lowerIndex(result syntax.Result, node *expressionView, pieces pieceList) document.Doc {
	opener, inner, closer := pieces.opener(), pieces.inner(), pieces.closer()
	atomic := false
	for i := range node.ChildCount() {
		if child, ok := node.Child(i).Node(); ok {
			atomic = child.Kind() == syntax.LiteralExpression || child.Kind() == syntax.VariableExpression
		}
	}
	for _, part := range pieces[1:] {
		for _, token := range part.before {
			if token.Kind().IsComment() {
				atomic = false
			}
		}
	}

	if atomic {
		return document.Concat(opener.doc, inner.doc, closer.doc)
	}
	leading, start := commentGap(result, inner.before, openingGap(soft))
	trailing, end := commentGap(result, closer.before, breakingGap(soft, inner.child.endsHeredoc))
	return document.Group(document.Concat(opener.doc,
		document.Indent(document.Concat(leading, start, inner.doc, trailing)), end, closer.doc))
}

// Binding powers extend the parser's operator precedences with the forms
// that bind tighter than any operator.
const (
	conditionalPower = syntax.ConditionalPrecedence // a ? b : c
	unaryPower       = syntax.UnaryPrecedence       // - !
	traversalPower   = unaryPower + 1               // .attr [index] .* [*]
	atomicPower      = traversalPower + 1           // Literals, names, and delimited forms.
)
