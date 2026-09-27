package syntax

// recoverUntil consumes tokens into one ErrorNode on parent until the next
// token in stops. Leading trivia goes to parent and trailing trivia is left
// unconsumed; at a stop it consumes nothing.
func (p *parser) recoverUntil(parent *nodeBuilder, context newlineContext, stops tokenSet) {
	if stops.has(p.peek(context)) {
		return
	}

	p.consumeUntil(parent, p.look(context))
	b := p.begin()
	for !stops.has(p.peek(context)) {
		p.consumeUntil(&b, p.look(context))
		if closing(p.current().kind) != Invalid {
			// Separators inside a nested construct do not stop recovery.
			p.skipConstruct(&b)
		} else {
			p.consumeLookahead(&b, context)
		}
	}
	parent.node(b.finish(ErrorNode))
}

// skipConstruct consumes the balanced construct opened at pos as raw tokens.
func (p *parser) skipConstruct(b *nodeBuilder) {
	if p.halted {
		return
	}

	// An unterminated construct ends at its last non-trivia token, leaving
	// trailing trivia to the parent.
	var ends []TokenKind
	end := p.pos
	for i := p.pos; p.tokens[i].kind != EOF; i++ {
		kind := p.tokens[i].kind
		if closer := closing(kind); closer != Invalid {
			ends = append(ends, closer)
		} else if len(ends) > 0 && ends[len(ends)-1] == kind {
			ends = ends[:len(ends)-1]
		} else if closingDelimiters.has(kind) {
			// A mismatched closer may belong to an enclosing construct.
			break
		}
		if !kind.IsTrivia() {
			end = i + 1
		}
		if len(ends) == 0 {
			break
		}
	}
	p.consumeUntil(b, end)
}

// closing returns the closer that balances an opener, or Invalid.
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
