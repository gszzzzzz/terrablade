package lowering

import (
	"fmt"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// lowerExpression normalizes source and lowers the resulting view.
func lowerExpression(result syntax.Result, source syntax.Node) layout {
	return postOrder(expressionWalker{result}, normalizeExpression(result, source), grammarContext{})
}

// expressionWalker lowers a normalized expression view.
type expressionWalker struct{ result syntax.Result }

func (expressionWalker) expand(node *expressionView, context grammarContext, children []visit[*expressionView, grammarContext]) []visit[*expressionView, grammarContext] {
	switch node.Kind() {
	case syntax.ObjectItem:
		context.newlinesAllowed = false // Object keys and values are newline-sensitive.
	case syntax.TemplateInterpolation, syntax.TemplateDirective:
		context = grammarContext{newlinesAllowed: true, inSequence: true}
	case syntax.ParenthesizedExpression, syntax.FunctionCallExpression,
		syntax.TupleExpression, syntax.IndexAccess, syntax.ForExpression,
		syntax.BinaryExpression, syntax.ConditionalExpression, syntax.TraversalExpression:
		// These forms delimit their contents or add break parentheses.
		context.newlinesAllowed = true
	}
	for _, element := range node.children {
		if element.node != nil {
			children = append(children, visit[*expressionView, grammarContext]{element.node, context})
		}
	}
	return children
}

func (w expressionWalker) lower(node *expressionView, context grammarContext, children []layout) layout {
	return lowerNode(w.result, node, context, children)
}

// grammarContext is what an expression node inherits from its ancestors.
type grammarContext struct {
	newlinesAllowed bool // The grammar permits newlines here.
	inSequence      bool // Inside a template interpolation or directive.
}

// piece is one significant child of a node together with the trivia before
// it. For a node piece, kind is syntax.Invalid; for a token piece, child is
// zero.
type piece struct {
	doc    document.Doc
	token  bool
	kind   syntax.TokenKind
	before []syntax.Token
	child  layout
}

// pieceList names pieces by their grammatical position.
type pieceList []piece

func (p pieceList) opener() piece { return p[0] }

func (p pieceList) closer() piece { return p[len(p)-1] }

// inner is the piece after the opening delimiter; for an empty form, the
// closer.
func (p pieceList) inner() piece { return p[1] }

func (p pieceList) beforeCloser() piece { return p[len(p)-2] }

func (p pieceList) contents(open int) pieceList { return p[open+1 : len(p)-1] }

// detachLeading returns a copy of pieces without the first piece's leading
// trivia, and that trivia, for a caller that lays it out itself. The copy
// keeps the comments from being emitted twice.
func detachLeading(pieces []piece) ([]piece, []syntax.Token) {
	detached := append([]piece(nil), pieces...)
	leading := detached[0].before
	detached[0].before = nil
	return detached, leading
}

// layout is the lowered form of one expression node, with the facts about
// its edges that a parent needs to choose spacing.
type layout struct {
	doc document.Doc
	// For an operation (binary, conditional, or traversal), body is doc
	// without break parentheses, head is the first operand, and continuation
	// is the rest. Explicit parentheses reuse body, so both spellings share
	// one group and a second pass reproduces the first.
	body               document.Doc
	head, continuation document.Doc
	operation          bool
	// power is a binary operator's precedence, for joining same-precedence
	// chains.
	power int
	// endsNumber, startsDot, and fusesNumber decide whether a traversal
	// keeps a space so a step is not scanned as part of a number.
	endsNumber  bool
	startsDot   bool
	fusesNumber bool
	// endsHeredoc reports that the node ends in a heredoc end marker, so the
	// following gap must supply a newline.
	endsHeredoc bool
	// startsBrace and endsBrace report object braces at the node's edges,
	// which a template sequence keeps a space away from.
	startsBrace, endsBrace bool
}

// lowerNode lowers one view node given its child nodes' layouts in order.
func lowerNode(result syntax.Result, node *expressionView, context grammarContext, children []layout) layout {
	switch node.Kind() {
	case syntax.TemplateExpression, syntax.TemplateIf, syntax.TemplateFor:
		return templateParts(result, node, children)
	}
	if !lowerableKind(node.Kind()) {
		// A diagnostic-free parse produces only lowerable kinds.
		panic(fmt.Sprintf("lowering: internal invariant: unsupported expression form %s", node.Kind()))
	}

	pieces := collectPieces(result, node, children)
	lowered := boundaryFlags(pieces)
	switch node.Kind() {
	case syntax.BinaryExpression, syntax.ConditionalExpression, syntax.TraversalExpression:
		lowered = lowerOperation(result, node.Kind(), pieces, lowered, context.newlinesAllowed)
	case syntax.AttributeAccess, syntax.LegacyIndexAccess:
		lowered.doc = sequence(result, pieces)
		lowered.fusesNumber = stepFusesNumber(result, node, pieces)
	default:
		lowered.doc = lowerForm(result, node, pieces, context.inSequence)
	}
	return lowered
}

// lowerableKind reports whether lowerNode has a layout for a non-template
// kind.
func lowerableKind(kind syntax.NodeKind) bool {
	switch kind {
	case syntax.LiteralExpression, syntax.VariableExpression,
		syntax.UnaryExpression, syntax.ParenthesizedExpression,
		syntax.FunctionCallExpression, syntax.TupleExpression,
		syntax.BinaryExpression, syntax.ConditionalExpression,
		syntax.TraversalExpression, syntax.AttributeAccess, syntax.IndexAccess,
		syntax.LegacyIndexAccess, syntax.AttributeSplat, syntax.FullSplat,
		syntax.ObjectExpression, syntax.ObjectItem, syntax.ForExpression,
		syntax.TemplateInterpolation, syntax.TemplateDirective:
		return true
	}
	return false
}

// collectPieces splits a node's children into significant pieces.
func collectPieces(result syntax.Result, node *expressionView, children []layout) []piece {
	pieces := make([]piece, 0, node.ChildCount())
	var trivia []syntax.Token
	for i := 0; i < node.ChildCount(); i++ {
		element := node.Child(i)
		if token, ok := element.Token(); ok {
			if token.Kind().IsTrivia() {
				trivia = append(trivia, token.source)
				continue
			}
			pieces = append(pieces, piece{doc: document.Text(token.spelling(result)), token: true, kind: token.Kind(), before: trivia})
		} else {
			pieces = append(pieces, piece{doc: children[0].doc, child: children[0], before: trivia})
			children = children[1:]
		}
		trivia = nil
	}
	return pieces
}

// boundaryFlags derives a node's edge flags from its first and last pieces.
func boundaryFlags(pieces []piece) layout {
	first, last := pieces[0], pieces[len(pieces)-1]
	return layout{
		endsNumber:  last.token && last.kind == syntax.Number || !last.token && last.child.endsNumber,
		startsDot:   first.token && first.kind == syntax.Dot,
		endsHeredoc: !last.token && last.child.endsHeredoc,
		startsBrace: first.token && first.kind == syntax.OpenBrace || !first.token && first.child.startsBrace,
		endsBrace:   last.token && last.kind == syntax.CloseBrace || !last.token && last.child.endsBrace,
	}
}

// lowerForm lays out every form other than operations and steps.
func lowerForm(result syntax.Result, node *expressionView, pieces pieceList, inSequence bool) document.Doc {
	switch node.Kind() {
	case syntax.LiteralExpression, syntax.VariableExpression, syntax.UnaryExpression:
		return sequence(result, pieces)
	case syntax.ParenthesizedExpression:
		return parenthesized(result, pieces)
	case syntax.TupleExpression:
		return delimited(result, pieces, 0, true, soft)
	case syntax.ObjectExpression:
		return lowerObject(result, pieces, inSequence)
	case syntax.ObjectItem:
		// Spell a : separator as =, so every entry aligns on one column.
		const separator = 1
		pieces[separator].doc = document.Text("=")
		return lowerAssignment(result, pieces, true)
	case syntax.ForExpression:
		return lowerForExpression(result, pieces)
	case syntax.TemplateInterpolation, syntax.TemplateDirective:
		return templateSequence(result, pieces)
	case syntax.IndexAccess:
		return lowerIndex(result, node, pieces)
	case syntax.AttributeSplat, syntax.FullSplat:
		// The prefix is .* (two tokens) or [*] (three).
		prefix := 2
		if node.Kind() == syntax.FullSplat {
			prefix = 3
		}
		return document.Concat(sequence(result, pieces[:prefix]), traversalSequence(result, pieces[prefix:], false, false))
	default: // FunctionCallExpression, including namespace prefixes.
		for i, part := range pieces {
			if part.kind == syntax.OpenParen {
				return delimited(result, pieces, i, false, soft)
			}
		}
		panic("lowering: valid call has no opening parenthesis")
	}
}

// lowerOperation lays out a binary, conditional, or traversal expression as
// a head followed by continuation lines that begin with the operator or step.
// Where the grammar forbids newlines, a binary or conditional operation adds
// break parentheses; a traversal never breaks between steps for width.
func lowerOperation(result syntax.Result, kind syntax.NodeKind, pieces []piece, lowered layout, newlinesAllowed bool) layout {
	head := pieces[0]
	lowered.operation = true
	lowered.head = head.doc
	switch kind {
	case syntax.BinaryExpression:
		lowered.power = syntax.BinaryPrecedence(pieces[1].kind)
		lowered.continuation = operationContinuation(result, pieces[1:], head.child.endsHeredoc)
		if head.child.power == lowered.power {
			// A same-precedence chain shares one group.
			lowered.head = head.child.head
			lowered.continuation = document.Concat(head.child.continuation, lowered.continuation)
		}
	case syntax.ConditionalExpression:
		lowered.continuation = operationContinuation(result, pieces[1:], head.child.endsHeredoc)
	case syntax.TraversalExpression:
		lowered.continuation = document.Group(traversalSequence(result, pieces[1:], head.child.endsNumber, head.child.endsHeredoc))
	}

	lowered.body = document.Concat(lowered.head, lowered.continuation)
	lowered.doc = document.Group(lowered.body)
	if !newlinesAllowed && kind != syntax.TraversalExpression {
		lowered.doc = breakParentheses(lowered.body)
		// The node now ends in the closing parenthesis.
		lowered.endsHeredoc = false
	}
	return lowered
}

// stepFusesNumber reports whether an attribute or legacy index step would
// scan as part of a number directly before it, as .0 or .e2 would, so that a
// space must separate them. OpenTofu 1.12.6 removes that space and cannot
// parse its own output.
func stepFusesNumber(result syntax.Result, node *expressionView, pieces []piece) bool {
	name, _ := node.Child(node.ChildCount() - 1).Token()
	if !syntax.ContinuesNumber(name.spelling(result)) {
		return false
	}
	step := pieces[1]
	for _, token := range step.before {
		if token.Kind().IsComment() {
			return false
		}
	}
	return true
}

// sequence joins pieces that render on one line. A comment inside brackets
// or after a dot hugs its token, keeping the step visibly one unit.
func sequence(result syntax.Result, pieces []piece) document.Doc {
	parts := make([]document.Doc, 0, len(pieces)*2)
	for i, part := range pieces {
		style := spacedGap(tight)
		style.requiredLine = i > 0 && pieces[i-1].child.endsHeredoc
		if i > 0 && pieces[i-1].kind == syntax.OpenBracket {
			style.beforeComment = soft
		}
		if i > 0 && pieces[i-1].kind == syntax.Dot {
			style.beforeComment = tight
		}
		if part.kind == syntax.CloseBracket {
			style.afterComment = tight
		}

		gap, end := commentGap(result, part.before, style)
		parts = append(parts, gap, end, part.doc)
	}
	return document.Concat(parts...)
}

// spacedSequence joins pieces with single spaces, as in a block header or a
// for clause. Commas and ellipses attach to the piece before them.
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

// parenthesized lays out explicit parentheses. They break only with an
// enclosed operation, whose group they share: break parentheses read back as
// explicit ones, so sharing the group keeps formatting idempotent.
func parenthesized(result syntax.Result, pieces pieceList) document.Doc {
	opener, inner, closer := pieces.opener(), pieces.inner(), pieces.closer()
	if inner.child.operation {
		leading, start := commentGap(result, inner.before, openingGap(soft))
		gap, end := commentGap(result, closer.before, breakingGap(soft, inner.child.endsHeredoc))
		return document.Group(document.Concat(opener.doc, document.Indent(document.Concat(leading, start, inner.child.body, gap)), end, closer.doc))
	}

	openerComment := tight
	for _, token := range inner.before {
		if token.Kind() == syntax.LineComment {
			// Upstream gives an inline line comment a visible opener boundary.
			openerComment = space
			break
		}
		if token.Kind() == syntax.BlockComment {
			break
		}
	}
	leading, start := commentGap(result, inner.before, gapStyle{beforeComment: openerComment, afterComment: space})
	gap, end := commentGap(result, closer.before, gapStyle{beforeComment: space, requiredLine: inner.child.endsHeredoc})
	return document.Concat(opener.doc, document.Indent(document.Concat(leading, start, inner.doc, gap)), end, closer.doc)
}

// delimited lays out a tuple, call arguments, or object entries. pieces[open]
// is the opening delimiter, and edge is the separator just inside the
// delimiters. preserveBlank keeps one source blank line between broken
// entries. delimited modifies the trivia of pieces.
//
// A broken list ends in a trailing comma and a flat one does not, whatever
// the source had, except after an expanded argument (the grammar forbids
// both) or a final heredoc.
func delimited(result syntax.Result, pieces pieceList, open int, preserveBlank bool, edge spacing) document.Doc {
	moveCommaTrivia(pieces)
	// Drop a trailing comma after a heredoc, giving its trivia to the closer.
	if trailing := len(pieces) - 2; trailing > open && pieces[trailing].kind == syntax.Comma && pieces[trailing-1].child.endsHeredoc {
		pieces[trailing+1].before = append(pieces[trailing].before, pieces[trailing+1].before...)
		pieces = append(pieces[:trailing], pieces[trailing+1:]...)
	}

	head := sequence(result, pieces[:open+1])
	closer := pieces.closer()
	content := pieces.contents(open)
	parts := make([]document.Doc, 0, len(content)*3+3)
	for i, part := range content {
		style := spacedGap(tight)
		style.requiredLine = i > 0 && content[i-1].child.endsHeredoc
		if i == 0 {
			style.empty, style.beforeComment = edge, edge
		} else if content[i-1].kind == syntax.Comma || content[i-1].child.endsHeredoc {
			// A heredoc's newline also separates entries.
			style.empty, style.afterComment = line, line
			style.blankLine = preserveBlank
		}
		if part.kind == syntax.Comma || part.kind == syntax.Ellipsis {
			style.afterComment = tight
		}

		gap, end := commentGap(result, part.before, style)
		value := part.doc
		if i == len(content)-1 && part.kind == syntax.Comma {
			value = document.IfBreak(value, document.Doc{})
		}
		parts = append(parts, gap, end, value)
	}
	if len(content) > 0 {
		last := content[len(content)-1].kind
		if last != syntax.Comma && last != syntax.Ellipsis && !content[len(content)-1].child.endsHeredoc {
			parts = append(parts, document.IfBreak(document.Text(","), document.Doc{}))
		}
	}

	style := breakingGap(edge, len(content) > 0 && content[len(content)-1].child.endsHeredoc)
	if len(content) == 0 {
		// Empty delimiters stay compact unless the object was broken in the
		// source.
		style.empty, style.beforeComment, style.afterComment = tight, soft, soft
		if edge == hard {
			style.empty, style.beforeComment, style.afterComment = hard, hard, hard
		}
	}
	gap, end := commentGap(result, closer.before, style)
	parts = append(parts, gap)
	return document.Group(document.Concat(head, document.Indent(document.Concat(parts...)), end, closer.doc))
}

// moveCommaTrivia moves each comma's leading trivia to the piece after it,
// so a comma always follows its value directly.
func moveCommaTrivia(pieces []piece) {
	for i := 0; i < len(pieces)-1; i++ {
		// A comma after a heredoc must stay on the line after its marker.
		if i > 0 && pieces[i-1].child.endsHeredoc {
			continue
		}
		if pieces[i].token && pieces[i].kind == syntax.Comma && len(pieces[i].before) > 0 {
			pieces[i+1].before = append(pieces[i].before, pieces[i+1].before...)
			pieces[i].before = nil
		}
	}
}
