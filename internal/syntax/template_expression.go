package syntax

// templateExpression parses a quoted template or heredoc whose opener is at
// the cursor. It keeps literal spelling, escapes, strip markers, and heredoc
// indentation untouched. Only interpolation contents enter the ordinary
// expression grammar; literal template bytes are never expression trivia.
// Directive scopes are tracked by templateNesting rather than by recursion.
func (p *parser) templateExpression(b *nodeBuilder) {
	close := QuoteClose
	if p.current().kind == HeredocOpen {
		close = HeredocEndMarker
	}
	p.consumeUntil(b, p.pos+1)

	// Builders are cursor snapshots. Copying the root avoids making every
	// caller's expression builder escape to the heap through the scope stack.
	nesting := templateNesting{root: *b, lastIf: -1, lastFor: -1}
	for {
		body := nesting.current()
		switch p.current().kind {
		case close:
			nesting.closeMissing(0, p.current().span)
			p.consumeUntil(b, p.pos+1)
			return
		case EOF:
			// The lexer diagnoses the unclosed template at its opening delimiter.
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
			// The heredoc header owns a newline when followed by content or its
			// closer. Trivia left at EOF by malformed sequences remains outside.
			next := p.look(delimitedExpression)
			if p.tokens[next].kind == EOF {
				nesting.closeMissing(0, p.tokens[next].span)
				return
			}
			p.consumeUntil(body, next)
		default:
			// The lexer normally emits only the cases above in a template body.
			// Diagnose unexpected tokens as a lexer/parser invariant defense, and
			// still consume one so a broken assumption cannot prevent progress.
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
	// Interpolations permit newlines even inside a quoted, single-line template.
	p.operand(&b, lowestPower, delimitedExpression)
	p.templateSequenceEnd(&b)
	parent.node(b.finish(TemplateInterpolation))
}

// templateSequenceOpen consumes the opener and an opening strip marker. Only a
// strip marker immediately after the opener belongs to its opening edge. Any
// later marker is before the closing brace, even across expression trivia.
func (p *parser) templateSequenceOpen(b *nodeBuilder) {
	p.consumeUntil(b, p.pos+1)
	if p.current().kind == StripMarker {
		p.consumeUntil(b, p.pos+1)
	}
}

// templateSequenceEnd consumes a closing strip marker and the closing brace of
// an interpolation or directive header. Material the expression left before
// the closer becomes one ErrorNode bounded by the template's own closers, so a
// broken sequence cannot swallow the template's closing quote or marker.
func (p *parser) templateSequenceEnd(b *nodeBuilder) {
	kind := p.peek(delimitedExpression)
	if kind != TemplateSequenceEnd && kind != StripMarker && kind != EOF {
		p.report(ExpectedTemplateSequenceEnd, p.tokens[p.look(delimitedExpression)].span)
		p.recoverUntil(b, delimitedExpression, templateBoundaries)
	}

	if p.peek(delimitedExpression) == StripMarker {
		p.consumeLookahead(b, delimitedExpression)
	}
	if p.peek(delimitedExpression) == TemplateSequenceEnd {
		p.consumeLookahead(b, delimitedExpression)
	}
}
