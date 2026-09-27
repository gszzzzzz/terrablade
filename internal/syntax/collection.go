package syntax

// collectionFor reports whether the opener at pos starts a for expression.
func (p *parser) collectionFor() bool {
	return p.keywordAt("for", p.lookFrom(p.pos+1, newlineTransparent))
}

// tuple parses "[...]". As upstream, elements need commas even on separate
// lines.
func (p *parser) tuple(b *nodeBuilder) {
	p.consumeLookahead(b, newlineTransparent)
	for {
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
			// Recover to the next comma; anything else ends the tuple.
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

// object parses "{...}", whose items are separated by commas or newlines.
func (p *parser) object(b *nodeBuilder) {
	p.consumeLookahead(b, newlineTransparent)
	for {
		// Leave separating newlines unconsumed until an item or the closer
		// follows.
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
			// Consumed into ObjectExpression by the next iteration.
		default:
			// A missing key/value separator already diagnosed this position.
			if separated {
				p.report(ExpectedObjectItemSeparator, p.tokens[p.look(newlineTerminates)].span)
			}
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
// separator was present.
func (p *parser) objectItem(parent *nodeBuilder) bool {
	b := p.begin()
	// Unlike a tuple element, an object key or value ends at a newline.
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
