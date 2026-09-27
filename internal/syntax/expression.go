package syntax

import "strings"

// Binding powers, from weakest to strongest.
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

// operand parses an expression binding at least as tightly as minimum into b,
// or reports a missing one and appends an empty ErrorNode, leaving the trivia.
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

// expression parses an expression whose operators bind at least as tightly
// as minimum. Binary operators associate left and conditionals right.
func (p *parser) expression(minimum int, context newlineContext) Node {
	if p.depth == maxRecursiveExpressionDepth {
		span := p.current().span
		b := p.begin()
		// Consume the offending token so the ErrorNode is not empty.
		p.consumeLookahead(&b, context)
		p.haltAtLimit(span)
		return b.finish(ErrorNode)
	}
	p.depth++
	defer func() { p.depth-- }()

	left := p.prefix(context)
	for {
		kind := p.peek(context)
		// Both arms are parsed at lowestPower, so conditionals associate right.
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
		if power == lowestPower || power < minimum {
			break
		}
		b := p.beginAt(left.Span().Start)
		b.node(left)
		p.consumeLookahead(&b, context)
		// power+1 leaves an operator of equal power to this loop, so binary
		// operators associate left.
		p.operand(&b, power+1, context)
		left = b.finish(BinaryExpression)
	}
	return left
}

// Operator precedence levels. Higher binds tighter.
const (
	// ConditionalPrecedence is the level of a ? b : c, the loosest.
	ConditionalPrecedence = lowestPower
	// UnaryPrecedence binds tighter than every binary operator.
	UnaryPrecedence = unaryPower
)

// BinaryPrecedence returns the binding power of a binary operator token, or
// ConditionalPrecedence for any other token.
func BinaryPrecedence(kind TokenKind) int { return binaryPower(kind) }

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

// prefix parses a primary or unary expression and any traversal steps after
// it.
func (p *parser) prefix(context newlineContext) Node {
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
		p.expectCloser(&b, CloseParen, ExpectedClosingParen)
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

	if p.peek(context) == Dot || p.peek(context) == OpenBracket {
		b = p.beginAt(left.Span().Start)
		b.node(left)
		p.steps(&b, context, allTraversalSteps)
		left = b.finish(TraversalExpression)
	}
	return left
}

// number consumes and validates a numeric literal. A legacy index, as in
// a.0, must not contain a decimal point.
func (p *parser) number(b *nodeBuilder, context newlineContext, legacy bool) {
	span := p.tokens[p.look(context)].span
	text := p.source[span.Start:span.End]
	if legacy && strings.Contains(text, ".") {
		p.report(InvalidLegacyIndex, span)
	} else if !numberRepresentable(text) {
		p.report(InvalidNumber, span)
	}
	p.consumeLookahead(b, context)
}

// expect consumes the next token if it has the given kind, and otherwise
// reports diagnostic at it and leaves it for the enclosing production.
func (p *parser) expect(b *nodeBuilder, kind TokenKind, diagnostic DiagnosticKind, context newlineContext) bool {
	if p.peek(context) == kind {
		p.consumeLookahead(b, context)
		return true
	}
	p.report(diagnostic, p.tokens[p.look(context)].span)
	return false
}

// expectCloser is expect for a closer in newline-transparent contents. On a
// mismatch it recovers to the closer and consumes it if found.
func (p *parser) expectCloser(b *nodeBuilder, closer TokenKind, diagnostic DiagnosticKind) {
	if p.expect(b, closer, diagnostic, newlineTransparent) {
		return
	}
	p.recoverUntil(b, newlineTransparent, expressionBoundaries)
	if p.peek(newlineTransparent) == closer {
		p.consumeLookahead(b, newlineTransparent)
	}
}

// call parses the rest of a function call after its first name: any
// "::"-separated parts, then the arguments.
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

	for {
		// Newlines are trivia among the arguments. Checking for ')' first allows
		// an empty list and a trailing comma.
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
			// Only ')' may follow an expanded argument.
			p.consumeLookahead(b, newlineTransparent)
			p.expectCloser(b, CloseParen, ExpectedClosingParen)
			return
		case kind == Comma:
			p.consumeLookahead(b, newlineTransparent)
		default:
			// Recover to the next comma; anything else ends the call.
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
