package syntax

// templateExpression parses a quoted template or heredoc.
func (p *parser) templateExpression(b *nodeBuilder) {
	closer := QuoteClose
	if p.current().kind == HeredocOpen {
		closer = HeredocEndMarker
	}
	p.consumeUntil(b, p.pos+1)

	// Copy b so that it does not escape to the heap.
	nesting := templateNesting{root: *b, lastIf: -1, lastFor: -1}
	for {
		body := nesting.current()
		switch p.current().kind {
		case closer:
			nesting.closeMissing(0, p.current().span)
			p.consumeUntil(b, p.pos+1)
			return
		// The lexer reports the unterminated template.
		case EOF:
			nesting.closeMissing(0, p.current().span)
			return
		case TemplateText, HeredocMarker:
			p.consumeUntil(body, p.pos+1)
		case InterpolationOpen:
			p.interpolation(body)
		case DirectiveOpen:
			header, name := p.templateDirective()
			nesting.directive(header, name)
		case Whitespace, Newline, LineComment, BlockComment:
			// Trivia at EOF, left by a malformed sequence, stays outside the
			// template.
			next := p.look(newlineTransparent)
			if p.tokens[next].kind == EOF {
				nesting.closeMissing(0, p.tokens[next].span)
				return
			}
			p.consumeUntil(body, next)
		default:
			// The lexer should not produce other tokens here.
			p.report(UnexpectedToken, p.current().span)
			part := p.begin()
			p.consumeUntil(&part, p.pos+1)
			body.node(part.finish(ErrorNode))
		}
	}
}

// interpolation parses one ${...} sequence whose opener is at the cursor.
func (p *parser) interpolation(parent *nodeBuilder) {
	b := p.begin()
	p.templateSequenceOpen(&b)
	// Newlines are allowed even in a quoted template's interpolation.
	p.operand(&b, lowestPower, newlineTransparent)
	p.templateSequenceEnd(&b)
	parent.node(b.finish(TemplateInterpolation))
}

// templateSequenceOpen consumes the opener and a strip marker directly after
// it.
func (p *parser) templateSequenceOpen(b *nodeBuilder) {
	p.consumeUntil(b, p.pos+1)
	if p.current().kind == StripMarker {
		p.consumeUntil(b, p.pos+1)
	}
}

// templateSequenceEnd consumes a closing strip marker and brace, recovering
// from anything before them without passing the template's own closer.
func (p *parser) templateSequenceEnd(b *nodeBuilder) {
	kind := p.peek(newlineTransparent)
	if kind != TemplateSequenceEnd && kind != StripMarker && kind != EOF {
		p.report(ExpectedTemplateSequenceEnd, p.tokens[p.look(newlineTransparent)].span)
		p.recoverUntil(b, newlineTransparent, templateBoundaries)
	}

	if p.peek(newlineTransparent) == StripMarker {
		p.consumeLookahead(b, newlineTransparent)
	}
	if p.peek(newlineTransparent) == TemplateSequenceEnd {
		p.consumeLookahead(b, newlineTransparent)
	}
}
