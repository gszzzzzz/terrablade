package syntax

import (
	"math/big"
	"strings"
)

// parseExpressionSource is an internal test seam for an attribute-style value:
// unparenthesized newlines terminate it. It is not a configuration-file parser.
// Leading and trailing trivia belong to File. Remaining non-trivia is an error.
func parseExpressionSource(source []byte) Result {
	p := newParser(source)
	root := p.begin()
	p.consumeUntil(&root, p.look(delimitedExpression))
	p.operand(&root, 0, lineExpression)
	return p.file(root)
}

// operand commits only trivia that precedes an actual expression. A missing
// operand gets an empty ErrorNode at the cursor; no synthetic token is emitted
// and trailing trivia remains available to the enclosing structure.
func (p *parser) operand(b *nodeBuilder, minimum int, context expressionContext) {
	i := p.look(context)
	if operandTerminators.has(p.tokens[i].kind) {
		p.report(ExpectedExpression, p.tokens[i].span)
		b.node(p.begin().finish(ErrorNode))
		return
	}
	p.consumeUntil(b, i)
	b.node(p.expression(minimum, context))
}

// expression uses Pratt binding powers: conditional 0 (right associative),
// binary 1..6 (left associative), unary 7, then postfix traversal.
func (p *parser) expression(minimum int, context expressionContext) SyntaxNode {
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
		// zero lets the false arm absorb another conditional, associating right.
		if kind == Question && minimum == 0 {
			b := p.beginAt(left.Span().Start)
			b.node(left)
			p.consumeLookahead(&b, context)
			p.operand(&b, 0, context)
			if p.expect(&b, Colon, ExpectedConditionalColon, context) {
				p.operand(&b, 0, context)
			}
			left = b.finish(ConditionalExpression)
			continue
		}
		power := binaryPower(kind)
		// Weaker operators belong to the caller; zero also leaves delimiters and
		// EOF untouched so the enclosing production can finish or recover.
		if power == 0 || power < minimum {
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

func binaryPower(kind TokenKind) int {
	switch kind {
	case Or:
		return 1
	case And:
		return 2
	case EqualEqual, NotEqual:
		return 3
	case Less, LessEqual, Greater, GreaterEqual:
		return 4
	case Plus, Minus:
		return 5
	case Star, Slash, Percent:
		return 6
	}
	return 0
}

func (p *parser) prefix(context expressionContext) SyntaxNode {
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
		p.operand(&b, 0, delimitedExpression)
		p.expect(&b, CloseParen, ExpectedClosingParen, delimitedExpression)
		kind = ParenthesizedExpression
	case Minus, Bang:
		p.consumeLookahead(&b, context)
		// Unary operands include postfix traversal but exclude every binary level.
		p.operand(&b, 7, context)
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

// number validates the lexer's numeric candidate without evaluating the
// expression. Legacy dot-index syntax additionally rejects any decimal point.
func (p *parser) number(b *nodeBuilder, context expressionContext, legacy bool) {
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

// A mismatch leaves the token for the enclosing production. Consuming it here
// could steal that production's closer or attach trailing trivia to this node.
func (p *parser) expect(b *nodeBuilder, kind TokenKind, diagnostic DiagnosticKind, context expressionContext) bool {
	if p.peek(context) == kind {
		p.consumeLookahead(b, context)
		return true
	}
	p.report(diagnostic, p.tokens[p.look(context)].span)
	return false
}

func (p *parser) call(b *nodeBuilder, context expressionContext) {
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
		if p.peek(delimitedExpression) == CloseParen {
			p.consumeLookahead(b, delimitedExpression)
			return
		}
		p.operand(b, 0, delimitedExpression)
		kind := p.peek(delimitedExpression)
		switch {
		case expressionBoundaries.has(kind):
			p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
			return
		case kind == Ellipsis:
			// Expansion is final: a following comma must not reopen the argument loop.
			p.consumeLookahead(b, delimitedExpression)
			p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
			return
		case kind == Comma:
			p.consumeLookahead(b, delimitedExpression)
		default:
			p.report(ExpectedArgumentSeparator, p.tokens[p.look(delimitedExpression)].span)
			p.recoverUntil(b, delimitedExpression, itemBoundaries)
			if p.peek(delimitedExpression) != Comma {
				p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
				return
			}
			p.consumeLookahead(b, delimitedExpression)
		}
	}
}
