package syntax

// collectionFor recognizes the contextual keyword before consuming an opener.
// Even object constructors ignore newlines for this one decision. Parentheses
// disambiguate a first element/key named for from a for-expression.
func (p *parser) collectionFor() bool {
	return p.keywordAt("for", p.lookFrom(p.pos+1, delimitedExpression))
}

// tuple parses "[...]" after collectionFor ruled out a for expression. Its
// elements are separated by commas with an optional trailing comma. Upstream
// requires the commas even when elements occupy separate lines, so a newline
// inside a tuple only continues its current expression.
func (p *parser) tuple(b *nodeBuilder) {
	p.consumeLookahead(b, delimitedExpression)
	for {
		// Testing the closer before an operand permits empty tuples and a
		// trailing comma.
		if expressionBoundaries.has(p.peek(delimitedExpression)) {
			p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			return
		}

		p.operand(b, lowestPower, delimitedExpression)
		kind := p.peek(delimitedExpression)
		switch {
		case expressionBoundaries.has(kind):
			p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			return
		case kind == Comma:
			p.consumeLookahead(b, delimitedExpression)
		default:
			// Report the missing comma once, then keep the material up to the
			// next comma or closer as one ErrorNode so later elements still
			// parse. Anything but a comma there ends the tuple.
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

// object parses "{...}" after collectionFor ruled out a for expression. Items
// are separated by commas or newlines, so the loop looks ahead under two
// contexts: between items, delimitedExpression skips the separating newlines
// to find the next item or the closer; after an item, lineExpression stops at
// the newline that ends it.
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
			// Keep the material up to the next separator or closer as one
			// ErrorNode so later items still parse.
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

// objectItem parses one key, separator, and value, and reports whether the
// separator was present. Object keys retain their concrete expression,
// including any parentheses. ObjectItem provides the context in which a naked
// identifier names a literal key; the CST neither evaluates it nor inserts a
// semantic wrapper around it.
func (p *parser) objectItem(parent *nodeBuilder) bool {
	b := p.begin()
	// Unlike tuples, a newline terminates an unparenthesized object key/value.
	// Nested delimiters select their own context through the expression parser.
	p.operand(&b, lowestPower, lineExpression)

	separated := p.peek(lineExpression) == Equal || p.peek(lineExpression) == Colon
	if separated {
		p.consumeLookahead(&b, lineExpression)
		p.operand(&b, lowestPower, lineExpression)
	} else {
		p.report(ExpectedObjectValueSeparator, p.tokens[p.look(lineExpression)].span)
	}
	parent.node(b.finish(ObjectItem))
	return separated
}
