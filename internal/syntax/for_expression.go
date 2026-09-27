package syntax

// forExpression parses a tuple or object for expression. The key arrow and
// grouping ellipsis are parsed in both forms, and reported in a tuple.
func (p *parser) forExpression(b *nodeBuilder) {
	open := p.current().kind
	closer, missingCloser := CloseBracket, ExpectedClosingBracket
	if open == OpenBrace {
		closer, missingCloser = CloseBrace, ExpectedClosingBrace
	}
	p.consumeLookahead(b, newlineTransparent) // opening bracket or brace
	p.consumeLookahead(b, newlineTransparent) // contextual for
	if !p.forIntroduction(b) || !p.expect(b, Colon, ExpectedForColon, newlineTransparent) {
		p.recoverUntil(b, newlineTransparent, expressionBoundaries)
		p.expect(b, closer, missingCloser, newlineTransparent)
		return
	}

	p.operand(b, lowestPower, newlineTransparent)
	if p.peek(newlineTransparent) == Arrow {
		if open == OpenBracket {
			p.report(UnexpectedForKey, p.tokens[p.look(newlineTransparent)].span)
		}
		p.consumeLookahead(b, newlineTransparent)
		p.operand(b, lowestPower, newlineTransparent)
	} else if open == OpenBrace {
		p.report(ExpectedForArrow, p.tokens[p.look(newlineTransparent)].span)
	}
	if p.peek(newlineTransparent) == Ellipsis {
		if open == OpenBracket {
			p.report(UnexpectedForGrouping, p.tokens[p.look(newlineTransparent)].span)
		}
		p.consumeLookahead(b, newlineTransparent)
	}
	if p.keyword("if", newlineTransparent) {
		p.consumeLookahead(b, newlineTransparent)
		p.operand(b, lowestPower, newlineTransparent)
	}

	p.expectCloser(b, closer, missingCloser)
}

// forIntroduction parses the variables, "in", and collection after the "for"
// of a for expression or directive.
func (p *parser) forIntroduction(b *nodeBuilder) bool {
	if !p.expect(b, Identifier, ExpectedForVariable, newlineTransparent) {
		return false
	}
	if p.peek(newlineTransparent) == Comma {
		p.consumeLookahead(b, newlineTransparent)
		if !p.expect(b, Identifier, ExpectedForVariable, newlineTransparent) {
			return false
		}
	}

	if !p.keyword("in", newlineTransparent) {
		p.report(ExpectedForIn, p.tokens[p.look(newlineTransparent)].span)
		return false
	}
	p.consumeLookahead(b, newlineTransparent)
	p.operand(b, lowestPower, newlineTransparent)
	return true
}
