package syntax

// collectionFor recognizes the contextual keyword before consuming an opener.
// Even object constructors ignore newlines for this one decision. Parentheses
// disambiguate a first element/key named for from a for-expression.
func (p *parser) collectionFor() bool {
	i := p.lookFrom(p.pos+1, delimitedExpression)
	token := p.tokens[i]
	return token.kind == Identifier && p.source[token.span.Start:token.span.End] == "for"
}

func (p *parser) tuple(b *nodeBuilder) {
	p.consumeLookahead(b, delimitedExpression)
	for {
		if expressionBoundaries.has(p.peek(delimitedExpression)) {
			p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			return
		}
		p.operand(b, 0, delimitedExpression)
		kind := p.peek(delimitedExpression)
		switch {
		case expressionBoundaries.has(kind):
			p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			return
		case kind == Comma:
			p.consumeLookahead(b, delimitedExpression)
		default:
			// Upstream requires commas even when elements occupy separate lines;
			// a newline inside a tuple only continues its current expression.
			p.report(ExpectedTupleSeparator, p.tokens[p.look(delimitedExpression)].span)
			p.recoverUntil(b, delimitedExpression, itemBoundaries)
			if p.peek(delimitedExpression) != Comma {
				p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
				return
			}
			p.consumeLookahead(b, delimitedExpression)
		}
	}
}

func (p *parser) object(b *nodeBuilder) {
	p.consumeLookahead(b, delimitedExpression)
	for {
		// Between items, newlines are separators. Leave them uncommitted until
		// another item or the closer appears, preserving outer trivia at EOF.
		if expressionBoundaries.has(p.peek(delimitedExpression)) {
			p.expect(b, CloseBrace, ExpectedClosingBrace, delimitedExpression)
			return
		}
		p.consumeUntil(b, p.look(delimitedExpression))
		separated := p.objectItem(b)
		kind := p.peek(lineExpression)
		switch {
		case expressionBoundaries.has(kind):
			p.expect(b, CloseBrace, ExpectedClosingBrace, lineExpression)
			return
		case kind == Comma:
			p.consumeLookahead(b, lineExpression)
		case lineSeparators.has(kind):
			// The next loop commits these to ObjectExpression, never ObjectItem.
		default:
			// A missing key/value separator already diagnosed this position.
			if separated {
				p.report(ExpectedObjectItemSeparator, p.tokens[p.look(lineExpression)].span)
			}
			p.recoverUntil(b, lineExpression, itemBoundaries)
			kind = p.peek(lineExpression)
			switch {
			case kind == Comma:
				p.consumeLookahead(b, lineExpression)
			case lineSeparators.has(kind):
			default:
				p.expect(b, CloseBrace, ExpectedClosingBrace, lineExpression)
				return
			}
		}
	}
}

// Object keys retain their concrete expression, including any parentheses.
// ObjectItem provides the context in which a naked identifier names a literal
// key; the CST neither evaluates it nor inserts a semantic wrapper around it.
func (p *parser) objectItem(parent *nodeBuilder) bool {
	b := p.begin()
	// Unlike tuples, a newline terminates an unparenthesized object key/value.
	// Nested delimiters select their own context through the expression parser.
	p.operand(&b, 0, lineExpression)
	separated := p.peek(lineExpression) == Equal || p.peek(lineExpression) == Colon
	if separated {
		p.consumeLookahead(&b, lineExpression)
		p.operand(&b, 0, lineExpression)
	} else {
		p.report(ExpectedObjectValueSeparator, p.tokens[p.look(lineExpression)].span)
	}
	parent.node(b.finish(ObjectItem))
	return separated
}
