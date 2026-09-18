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
		switch p.peek(delimitedExpression) {
		case CloseBracket:
			p.consumeLookahead(b, delimitedExpression)
			return
		case EOF, CloseParen, CloseBrace, TemplateSequenceEnd, QuoteClose, HeredocEndMarker, StripMarker:
			p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			return
		}
		p.operand(b, 0, delimitedExpression)
		switch p.peek(delimitedExpression) {
		case CloseBracket:
			p.consumeLookahead(b, delimitedExpression)
			return
		case Comma:
			p.consumeLookahead(b, delimitedExpression)
		case EOF, CloseParen, CloseBrace, TemplateSequenceEnd, QuoteClose, HeredocEndMarker, StripMarker:
			p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			return
		default:
			// Upstream requires commas even when elements occupy separate lines;
			// a newline inside a tuple only continues its current expression.
			p.report(ExpectedTupleSeparator, p.tokens[p.look(delimitedExpression)].span)
			p.recoverCollection(b, delimitedExpression)
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
		switch p.peek(delimitedExpression) {
		case CloseBrace:
			p.consumeLookahead(b, delimitedExpression)
			return
		case EOF, CloseParen, CloseBracket, TemplateSequenceEnd, QuoteClose, HeredocEndMarker, StripMarker:
			p.expect(b, CloseBrace, ExpectedClosingBrace, delimitedExpression)
			return
		}
		p.consumeUntil(b, p.look(delimitedExpression))
		separated := p.objectItem(b)
		switch p.peek(lineExpression) {
		case CloseBrace:
			p.consumeLookahead(b, lineExpression)
			return
		case Comma:
			p.consumeLookahead(b, lineExpression)
		case Newline, LineComment:
			// The next loop commits these to ObjectExpression, never ObjectItem.
		case EOF, CloseParen, CloseBracket, TemplateSequenceEnd, QuoteClose, HeredocEndMarker, StripMarker:
			p.expect(b, CloseBrace, ExpectedClosingBrace, lineExpression)
			return
		default:
			// A missing key/value separator already diagnosed this position.
			if separated {
				p.report(ExpectedObjectItemSeparator, p.tokens[p.look(lineExpression)].span)
			}
			p.recoverCollection(b, lineExpression)
			switch p.peek(lineExpression) {
			case Comma:
				p.consumeLookahead(b, lineExpression)
			case Newline, LineComment:
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

// recoverCollection keeps the next separator/closer for the collection loop.
// Balanced nested constructs are skipped together so their commas and newlines
// cannot accidentally restart the outer collection. Trivia outside the malformed
// region stays with its parent, as it does around ordinary expression nodes.
func (p *parser) recoverCollection(parent *nodeBuilder, context expressionContext) {
	p.consumeUntil(parent, p.look(context))
	b := p.begin()
	for {
		switch p.peek(context) {
		case EOF, Comma, CloseParen, CloseBracket, CloseBrace, Newline, LineComment,
			TemplateSequenceEnd, QuoteClose, HeredocEndMarker, StripMarker:
			if len(p.pending) > b.mark {
				parent.node(b.finish(Error))
			}
			return
		}
		p.consumeUntil(&b, p.look(context))
		if closing(p.current().kind) != Invalid {
			p.skipConstruct(&b)
		} else {
			p.consumeLookahead(&b, context)
		}
	}
}
