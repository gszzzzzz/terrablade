package syntax

// collectionFor recognizes the contextual keyword before consuming an opener.
// Even object constructors ignore newlines for this one decision. Parentheses
// disambiguate a first element/key named for from a for-expression.
func (p *parser) collectionFor() bool {
	return p.keywordAt("for", p.lookFrom(p.pos+1, newlineTransparent))
}

// tuple parses "[...]" after collectionFor ruled out a for expression. Its
// elements are separated by commas with an optional trailing comma. Upstream
// requires the commas even when elements occupy separate lines, so a newline
// inside a tuple only continues its current expression.
func (p *parser) tuple(b *nodeBuilder) {
	p.consumeLookahead(b, newlineTransparent)
	for {
		// Testing the closer before an operand permits empty tuples and a
		// trailing comma.
		if expressionBoundaries.has(p.peek(newlineTransparent)) {
			p.expect(b, CloseBracket, ExpectedClosingBracket, newlineTransparent)
			return
		}

		p.operand(b, lowestPower, newlineTransparent)
		kind := p.peek(newlineTransparent)
		switch {
		case expressionBoundaries.has(kind):
			p.expect(b, CloseBracket, ExpectedClosingBracket, newlineTransparent)
			return
		case kind == Comma:
			p.consumeLookahead(b, newlineTransparent)
		default:
			// Report the missing comma once, then keep the material up to the
			// next comma or closer as one ErrorNode so later elements still
			// parse. Anything but a comma there ends the tuple.
			p.report(ExpectedTupleSeparator, p.tokens[p.look(newlineTransparent)].span)
			p.recoverUntil(b, newlineTransparent, itemBoundaries)
			if p.peek(newlineTransparent) != Comma {
				p.expect(b, CloseBracket, ExpectedClosingBracket, newlineTransparent)
				return
			}
			p.consumeLookahead(b, newlineTransparent)
		}
	}
}

// object parses "{...}" after collectionFor ruled out a for expression. Items
// are separated by commas or newlines, so the loop looks ahead under two
// contexts: between items, newlineTransparent skips the separating newlines to
// find the next item or the closer; after an item, newlineTerminates stops at
// the newline that ends it.
func (p *parser) object(b *nodeBuilder) {
	p.consumeLookahead(b, newlineTransparent)
	for {
		// Between items, newlines are separators. Leave them uncommitted until
		// another item or the closer appears, preserving outer trivia at EOF.
		if expressionBoundaries.has(p.peek(newlineTransparent)) {
			p.expect(b, CloseBrace, ExpectedClosingBrace, newlineTransparent)
			return
		}
		p.consumeUntil(b, p.look(newlineTransparent))
		separated := p.objectItem(b)

		kind := p.peek(newlineTerminates)
		switch {
		case expressionBoundaries.has(kind):
			p.expect(b, CloseBrace, ExpectedClosingBrace, newlineTerminates)
			return
		case kind == Comma:
			p.consumeLookahead(b, newlineTerminates)
		case lineSeparators.has(kind):
			// The next loop commits these to ObjectExpression, never ObjectItem.
		default:
			// A missing key/value separator already diagnosed this position.
			if separated {
				p.report(ExpectedObjectItemSeparator, p.tokens[p.look(newlineTerminates)].span)
			}
			// Keep the material up to the next separator or closer as one
			// ErrorNode so later items still parse.
			p.recoverUntil(b, newlineTerminates, itemBoundaries)
			kind = p.peek(newlineTerminates)
			switch {
			case kind == Comma:
				p.consumeLookahead(b, newlineTerminates)
			case lineSeparators.has(kind):
			default:
				p.expect(b, CloseBrace, ExpectedClosingBrace, newlineTerminates)
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
	p.operand(&b, lowestPower, newlineTerminates)

	separated := p.peek(newlineTerminates) == Equal || p.peek(newlineTerminates) == Colon
	if separated {
		p.consumeLookahead(&b, newlineTerminates)
		p.operand(&b, lowestPower, newlineTerminates)
	} else {
		p.report(ExpectedObjectValueSeparator, p.tokens[p.look(newlineTerminates)].span)
	}
	parent.node(b.finish(ObjectItem))
	return separated
}
