package lowering

import (
	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// breakParentheses wraps an operation that may break where the grammar
// forbids expression newlines. The parentheses appear only in the broken
// layout, so a fitting expression keeps its source spelling and a broken one
// stays parseable (doc.go: Parentheses). This is the width-conditional half
// of the pair; normalization's permanentParentheses adds the other half,
// which is present in every layout because the grammar needs it.
func breakParentheses(body document.Doc) document.Doc {
	return document.Group(document.Concat(
		document.IfBreak(document.Text("("), document.Doc{}),
		document.Indent(document.Concat(document.SoftLine(), body)),
		document.SoftLine(), document.IfBreak(document.Text(")"), document.Doc{}),
	))
}

// operationContinuation lays out the operator/operand tail of a binary or
// conditional expression. Operators start continuation lines at the
// surrounding delimiter's indentation, matching upstream; operands follow
// their operator on the same line. A same-precedence chain still shares one
// fit decision because the caller concatenates continuations.
//
// A minus that starts a continuation line is spaced as upstream spaces a
// unary minus, tight against its operand (doc.go: Operators), even though the
// parser sees binary subtraction. flatSpace gives the flat layout its usual
// space. forcedMinus covers a minus that is guaranteed to start a line by a
// heredoc marker or a mandatory comment line; inside a ForceFlat template the
// flat branch would still be chosen there, so the spacing must be tight
// unconditionally.
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
			// Upstream treats a line-leading minus as a unary token for
			// spacing, even though the parser still sees this as binary
			// subtraction.
			style.empty = flatSpace
			if forcedMinus {
				style.empty = tight
			}
			for _, token := range part.before {
				if token.Kind() == syntax.LineComment {
					break
				}
				if token.Kind() == syntax.BlockComment {
					// The block comment stays inline, so it takes the
					// operand's tight spacing and the operand follows it.
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

// operationGapHasLine reports whether trivia forces a line break on its own:
// a line comment always does, and a block comment followed by a source
// newline keeps that newline (doc.go: Comments).
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

// traversalSequence lays out traversal steps after their root. Dot and
// bracket steps never introduce a width-driven line break; only a retained
// comment or heredoc marker separates steps (doc.go: Traversals). endsNumber
// and endsHeredoc describe the piece before the first step.
func traversalSequence(result syntax.Result, pieces []piece, endsNumber, endsHeredoc bool) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*3)
	for _, part := range pieces {
		separator := tight
		if part.child.startsDot && endsNumber && part.child.fusesNumber {
			// The number scanner would swallow this step: 1.e2 is one number
			// but 1 .e2 is a traversal (doc.go: Traversals).
			separator = space
		}
		// The gap is tight, or one number-boundary space; a retained comment
		// supplies that boundary itself, may require a line, and is followed
		// tightly by its step.
		gap, end := commentGap(result, part.before, gapStyle{empty: separator, beforeComment: space, afterComment: tight, requiredLine: endsHeredoc})
		parts = append(parts, gap, end, part.doc)

		endsNumber = part.child.endsNumber
		endsHeredoc = part.child.endsHeredoc
	}
	return document.Concat(parts...)
}

// lowerIndex lays out one bracketed index step. An index holding a bare
// literal or name is atomic and never breaks; any other index may break
// inside its brackets (doc.go: Traversals).
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
		// A long following traversal must not peel an indivisible index onto
		// a line of its own. There is no useful break inside [name] or [0].
		return document.Concat(opener.doc, inner.doc, closer.doc)
	}
	leading, start := commentGap(result, inner.before, openingGap(soft))
	trailing, end := commentGap(result, closer.before, breakingGap(soft, inner.child.endsHeredoc))
	return document.Group(document.Concat(opener.doc,
		document.Indent(document.Concat(leading, start, inner.doc, trailing)), end, closer.doc))
}

// Binding powers order HCL's expression forms from loosest to tightest. An
// operand binding more loosely than its operator needs parentheses to keep its
// meaning, and equal power on the right of a binary operator would reassociate
// a left-associative chain. The parser defines the operator levels, read with
// syntax.BinaryPrecedence; traversals and atoms bind tighter than any of them.
const (
	conditionalPower = syntax.ConditionalPrecedence // a ? b : c
	unaryPower       = syntax.UnaryPrecedence       // - !
	traversalPower   = unaryPower + 1               // .attr [index] .* [*]
	atomicPower      = traversalPower + 1           // Literals, names, and delimited forms.
)
