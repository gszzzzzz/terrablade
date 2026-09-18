package syntax

// forExpression preserves the concrete tuple/object form instead of evaluating
// its projection. Bindings and contextual keywords remain Identifier tokens.
func (p *parser) forExpression(b *nodeBuilder) {
	open := p.current().kind
	close, missingClose := CloseBracket, ExpectedClosingBracket
	if open == OpenBrace {
		close, missingClose = CloseBrace, ExpectedClosingBrace
	}
	p.consumeLookahead(b, delimitedExpression) // opening bracket or brace
	p.consumeLookahead(b, delimitedExpression) // contextual for
	if !p.forIntroduction(b) || !p.expect(b, Colon, ExpectedForColon, delimitedExpression) {
		p.recoverUntil(b, delimitedExpression, expressionBoundaries)
		p.expect(b, close, missingClose, delimitedExpression)
		return
	}

	p.operand(b, 0, delimitedExpression)
	if p.peek(delimitedExpression) == Arrow {
		if open == OpenBracket {
			p.report(UnexpectedForKey, p.tokens[p.look(delimitedExpression)].span)
		}
		p.consumeLookahead(b, delimitedExpression)
		p.operand(b, 0, delimitedExpression)
	} else if open == OpenBrace {
		p.report(ExpectedForArrow, p.tokens[p.look(delimitedExpression)].span)
	}
	if p.peek(delimitedExpression) == Ellipsis {
		if open == OpenBracket {
			p.report(UnexpectedForGrouping, p.tokens[p.look(delimitedExpression)].span)
		}
		p.consumeLookahead(b, delimitedExpression)
	}
	if p.keyword("if", delimitedExpression) {
		p.consumeLookahead(b, delimitedExpression)
		p.operand(b, 0, delimitedExpression)
	}
	if !p.expect(b, close, missingClose, delimitedExpression) {
		// A for-expression has no item separators. Recover its remaining tail as
		// one region rather than interpreting a stray comma as a new projection.
		p.recoverUntil(b, delimitedExpression, expressionBoundaries)
		if p.peek(delimitedExpression) == close {
			p.consumeLookahead(b, delimitedExpression)
		}
	}
}

// The same bindings/in/collection grammar introduces template for directives.
// Callers consume `for` and supply their own following ':' or template closer.
func (p *parser) forIntroduction(b *nodeBuilder) bool {
	if !p.expect(b, Identifier, ExpectedForVariable, delimitedExpression) {
		return false
	}
	if p.peek(delimitedExpression) == Comma {
		p.consumeLookahead(b, delimitedExpression)
		if !p.expect(b, Identifier, ExpectedForVariable, delimitedExpression) {
			return false
		}
	}
	if !p.keyword("in", delimitedExpression) {
		p.report(ExpectedForIn, p.tokens[p.look(delimitedExpression)].span)
		return false
	}
	p.consumeLookahead(b, delimitedExpression)
	p.operand(b, 0, delimitedExpression)
	return true
}
