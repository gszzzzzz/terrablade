package syntax

import (
	"math/big"
	"strings"
)

// Binding powers order the operators from weakest to strongest for the Pratt
// loop in expression. A production asks operand for a minimum power and
// receives everything that binds at least that tightly. lowestPower accepts
// any expression; binaryPower also returns it for tokens that are not binary
// operators, and since every operator is stronger, that ends the loop.
const (
	lowestPower = iota
	orPower
	andPower
	equalityPower
	comparisonPower
	additivePower
	multiplicativePower
	unaryPower
)

// operand commits only trivia that precedes an actual expression. A missing
// operand gets an empty ErrorNode at the cursor; no synthetic token is emitted
// and trailing trivia remains available to the enclosing structure.
func (p *parser) operand(b *nodeBuilder, minimum int, context newlineContext) {
	i := p.look(context)
	if operandTerminators.has(p.tokens[i].kind) {
		p.report(ExpectedExpression, p.tokens[i].span)
		b.node(p.begin().finish(ErrorNode))
		return
	}
	p.consumeUntil(b, i)
	b.node(p.expression(minimum, context))
}

// expression parses one expression whose operators bind at least as tightly
// as minimum. Conditionals bind at lowestPower and associate right; binary
// operators bind from orPower to multiplicativePower and associate left;
// unary operators bind at unaryPower; postfix traversal binds tightest and is
// handled inside prefix.
func (p *parser) expression(minimum int, context newlineContext) SyntaxNode {
	if p.depth == maxRecursiveExpressionDepth {
		span := p.current().span
		b := p.begin()
		// Retain the offending token in a non-empty ErrorNode before freezing the
		// cursor: this boundary makes progress, while File recovers the remainder.
		p.consumeLookahead(&b, context)
		p.haltAtLimit(span)
		return b.finish(ErrorNode)
	}
	p.depth++
	defer func() { p.depth-- }()

	left := p.prefix(context)
	for {
		kind := p.peek(context)
		// Only the weakest binding level may consume '?'. Parsing both arms at
		// lowestPower lets the false arm absorb another conditional, associating
		// right.
		if kind == Question && minimum == lowestPower {
			b := p.beginAt(left.Span().Start)
			b.node(left)
			p.consumeLookahead(&b, context)
			p.operand(&b, lowestPower, context)
			if p.expect(&b, Colon, ExpectedConditionalColon, context) {
				p.operand(&b, lowestPower, context)
			}
			left = b.finish(ConditionalExpression)
			continue
		}

		power := binaryPower(kind)
		// Weaker operators belong to the caller; lowestPower also leaves
		// delimiters and EOF untouched so the enclosing production can finish
		// or recover.
		if power == lowestPower || power < minimum {
			break
		}
		b := p.beginAt(left.Span().Start)
		b.node(left)
		p.consumeLookahead(&b, context)
		// A same-precedence operator cannot enter the RHS. This loop consumes it
		// next, wrapping the previous result on the left rather than the right.
		p.operand(&b, power+1, context)
		left = b.finish(BinaryExpression)
	}
	return left
}

// binaryPower returns the binding power of a binary operator token, or
// lowestPower for any other token.
func binaryPower(kind TokenKind) int {
	switch kind {
	case Or:
		return orPower
	case And:
		return andPower
	case EqualEqual, NotEqual:
		return equalityPower
	case Less, LessEqual, Greater, GreaterEqual:
		return comparisonPower
	case Plus, Minus:
		return additivePower
	case Star, Slash, Percent:
		return multiplicativePower
	}
	return lowestPower
}

// prefix parses the operand that begins an expression: a literal, variable,
// parenthesized expression, unary operation, collection, for expression, or
// template, followed by any traversal steps. kind starts as ErrorNode so that
// only the fallback arm, which reports the missing expression and retains the
// offending token, leaves it unchanged; every grammatical arm sets its own.
func (p *parser) prefix(context newlineContext) SyntaxNode {
	b := p.begin()
	token := p.current()
	kind := ErrorNode
	switch token.kind {
	case Number:
		p.number(&b, context, false)
		kind = LiteralExpression
	case Identifier:
		p.consumeLookahead(&b, context)
		// Keywords are contextual: true() and null::f() are function calls.
		if p.peek(context) == OpenParen || p.peek(context) == DoubleColon {
			p.call(&b, context)
			kind = FunctionCallExpression
		} else {
			switch p.source[token.span.Start:token.span.End] {
			case "true", "false", "null":
				kind = LiteralExpression
			default:
				kind = VariableExpression
			}
		}
	case OpenParen:
		p.consumeLookahead(&b, context)
		p.operand(&b, lowestPower, newlineTransparent)
		p.expect(&b, CloseParen, ExpectedClosingParen, newlineTransparent)
		kind = ParenthesizedExpression
	case Minus, Bang:
		p.consumeLookahead(&b, context)
		// Unary operands include postfix traversal but exclude every binary level.
		p.operand(&b, unaryPower, context)
		kind = UnaryExpression
	case OpenBracket, OpenBrace:
		if p.collectionFor() {
			p.forExpression(&b)
			kind = ForExpression
		} else if token.kind == OpenBracket {
			p.tuple(&b)
			kind = TupleExpression
		} else {
			p.object(&b)
			kind = ObjectExpression
		}
	case QuoteOpen, HeredocOpen:
		p.templateExpression(&b)
		kind = TemplateExpression
	default:
		p.report(ExpectedExpression, token.span)
		p.consumeLookahead(&b, context)
	}
	left := b.finish(kind)

	// Traversal steps bind tighter than every operator, so they attach here
	// before expression sees the first operator.
	if p.peek(context) == Dot || p.peek(context) == OpenBracket {
		b = p.beginAt(left.Span().Start)
		b.node(left)
		p.steps(&b, context, allTraversalSteps)
		left = b.finish(TraversalExpression)
	}
	return left
}

// number validates the lexer's numeric candidate without evaluating the
// expression. Legacy dot-index syntax additionally rejects any decimal point.
func (p *parser) number(b *nodeBuilder, context newlineContext, legacy bool) {
	span := p.tokens[p.look(context)].span
	text := p.source[span.Start:span.End]
	if legacy && strings.Contains(text, ".") {
		p.report(InvalidLegacyIndex, span)
	} else if _, _, err := big.ParseFloat(text, 10, 512, big.ToNearestEven); err != nil {
		// This is the same representability check as upstream cty.ParseNumberVal,
		// using the standard library and retaining no evaluated value in the CST.
		p.report(InvalidNumber, span)
	}
	p.consumeLookahead(b, context)
}

// expect consumes the next grammatical token when it has the given kind and
// otherwise reports diagnostic at it. A mismatch leaves the token for the
// enclosing production. Consuming it here could steal that production's closer
// or attach trailing trivia to this node.
func (p *parser) expect(b *nodeBuilder, kind TokenKind, diagnostic DiagnosticKind, context newlineContext) bool {
	if p.peek(context) == kind {
		p.consumeLookahead(b, context)
		return true
	}
	p.report(diagnostic, p.tokens[p.look(context)].span)
	return false
}

// call parses the rest of a function call after its first name: optional
// "::"-separated namespace parts, then the parenthesized argument list. The
// arguments are expressions separated by commas; a trailing comma is allowed,
// and a final "..." expands the last argument, after which only ')' may follow.
func (p *parser) call(b *nodeBuilder, context newlineContext) {
	for p.peek(context) == DoubleColon {
		p.consumeLookahead(b, context)
		if !p.expect(b, Identifier, ExpectedFunctionName, context) {
			return
		}
	}
	if !p.expect(b, OpenParen, ExpectedOpeningParen, context) {
		return
	}

	// Only the arguments ignore newlines; namespace/name and '(' use the outer
	// context. Testing ')' before an operand permits empty lists and trailing ','.
	for {
		if p.peek(newlineTransparent) == CloseParen {
			p.consumeLookahead(b, newlineTransparent)
			return
		}

		p.operand(b, lowestPower, newlineTransparent)
		kind := p.peek(newlineTransparent)
		switch {
		case expressionBoundaries.has(kind):
			p.expect(b, CloseParen, ExpectedClosingParen, newlineTransparent)
			return
		case kind == Ellipsis:
			// Expansion is final: a following comma must not reopen the argument loop.
			p.consumeLookahead(b, newlineTransparent)
			p.expect(b, CloseParen, ExpectedClosingParen, newlineTransparent)
			return
		case kind == Comma:
			p.consumeLookahead(b, newlineTransparent)
		default:
			// Report the missing comma once, then keep the material up to the
			// next comma or closer as one ErrorNode so the arguments after it
			// still parse. Anything but a comma there ends the call.
			p.report(ExpectedArgumentSeparator, p.tokens[p.look(newlineTransparent)].span)
			p.recoverUntil(b, newlineTransparent, itemBoundaries)
			if p.peek(newlineTransparent) != Comma {
				p.expect(b, CloseParen, ExpectedClosingParen, newlineTransparent)
				return
			}
			p.consumeLookahead(b, newlineTransparent)
		}
	}
}
