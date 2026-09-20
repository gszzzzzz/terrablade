package syntax

// forExpression parses a tuple or object for expression whose opener is at the
// cursor. It preserves the concrete tuple/object form instead of evaluating its
// projection. Bindings and contextual keywords remain Identifier tokens. The
// key/value arrow and the grouping ellipsis are parsed in both forms and
// diagnosed in the tuple form, so the tree keeps their bytes either way.
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

	p.operand(b, lowestPower, delimitedExpression)
	if p.peek(delimitedExpression) == Arrow {
		if open == OpenBracket {
			p.report(UnexpectedForKey, p.tokens[p.look(delimitedExpression)].span)
		}
		p.consumeLookahead(b, delimitedExpression)
		p.operand(b, lowestPower, delimitedExpression)
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
		p.operand(b, lowestPower, delimitedExpression)
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

// forIntroduction parses the bindings, "in", and collection that follow a for
// keyword. The same grammar introduces template for directives. Callers consume
// `for` and supply their own following ':' or template closer.
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
	p.operand(b, lowestPower, delimitedExpression)
	return true
}
