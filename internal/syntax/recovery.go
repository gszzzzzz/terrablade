package syntax

// recoverUntil retains malformed material in one ErrorNode until a boundary
// chosen by its caller. Leading trivia stays on the parent; trailing trivia is
// left uncommitted for the next production. If already at a boundary, even the
// leading trivia must stay untouched, especially at EOF or a template closer.
func (p *parser) recoverUntil(parent *nodeBuilder, context expressionContext, stops tokenSet) {
	if stops.has(p.peek(context)) {
		return
	}
	p.consumeUntil(parent, p.look(context))
	b := p.begin()
	for !stops.has(p.peek(context)) {
		p.consumeUntil(&b, p.look(context))
		if closing(p.current().kind) != Invalid {
			// Inner commas/newlines are not separators for the outer production.
			p.skipConstruct(&b)
		} else {
			p.consumeLookahead(&b, context)
		}
	}
	parent.node(b.finish(ErrorNode))
}

// skipConstruct retains one balanced construct as raw tokens without recursion
// during error recovery. Nested template/interpolation delimiters remain visible
// in the lexical stream.
func (p *parser) skipConstruct(b *nodeBuilder) {
	if p.halted {
		return
	}
	// Raw tokens preserve template whitespace and delimiters in the ErrorNode subtree;
	// expression lookahead would interpret trivia in the wrong sub-language.
	// An unterminated construct still ends at its last non-trivia token, so the
	// trailing trivia of the file stays with the parent like any other node.
	var ends []TokenKind
	end := p.pos
	for i := p.pos; p.tokens[i].kind != EOF; i++ {
		kind := p.tokens[i].kind
		if close := closing(kind); close != Invalid {
			ends = append(ends, close)
		} else if len(ends) > 0 && ends[len(ends)-1] == kind {
			ends = ends[:len(ends)-1]
		} else if closingDelimiters.has(kind) {
			// A mismatched closer can belong to an enclosing expression or
			// template. Leave it available instead of swallowing the outer tail.
			break
		}
		if !isTrivia(kind) {
			end = i + 1
		}
		if len(ends) == 0 {
			break
		}
	}
	p.consumeUntil(b, end)
}

func closing(kind TokenKind) TokenKind {
	switch kind {
	case OpenParen:
		return CloseParen
	case OpenBracket:
		return CloseBracket
	case OpenBrace:
		return CloseBrace
	case QuoteOpen:
		return QuoteClose
	case HeredocOpen:
		return HeredocEndMarker
	case InterpolationOpen, DirectiveOpen:
		return TemplateSequenceEnd
	}
	return Invalid
}
