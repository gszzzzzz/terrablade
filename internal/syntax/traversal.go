package syntax

// steps keeps traversal steps flat except where splat semantics group suffixes.
// Attribute splats own only following dot accesses; a full splat owns every
// following traversal, including nested splats. This follows upstream HCL's
// parseExpressionTraversals without introducing evaluated or synthetic nodes.
func (p *parser) steps(parent *nodeBuilder, context expressionContext, attributesOnly bool) {
	if kind := p.peek(context); kind != Dot && (kind != OpenBracket || attributesOnly) {
		return
	}
	if p.depth == maxExpressionDepth {
		p.limit(p.tokens[p.look(context)].Span)
		return
	}
	p.depth++
	defer func() { p.depth-- }()
	for !p.limited {
		kind := p.peek(context)
		if kind != Dot && (kind != OpenBracket || attributesOnly) {
			return
		}
		p.before(parent, p.look(context))
		b := p.begin()
		p.take(&b, context)
		if kind == Dot {
			switch p.peek(context) {
			case Identifier:
				p.take(&b, context)
				parent.node(p.finish(AttributeAccess, b))
			case Number:
				p.number(&b, context, true)
				parent.node(p.finish(LegacyIndexAccess, b))
			case Star:
				if attributesOnly {
					p.report(NestedAttributeSplat, p.tokens[p.look(context)].Span)
					p.take(&b, context)
					parent.node(p.finish(Error, b))
					continue
				}
				p.take(&b, context)
				p.steps(&b, context, true)
				parent.node(p.finish(AttributeSplat, b))
			default:
				p.report(ExpectedAttributeName, p.tokens[p.look(context)].Span)
				parent.node(p.finish(Error, b))
				return
			}
			continue
		}
		// Upstream detects [*] in the outer newline context before entering
		// ordinary index-expression mode. Thus a[\n*] is invalid outside parens,
		// while a[\n0] is valid. Preserve this distinction and all raw trivia.
		if p.peek(context) == Star {
			p.take(&b, context)
			if p.expect(&b, CloseBracket, ExpectedClosingBracket, context) {
				p.steps(&b, context, false)
			}
			parent.node(p.finish(FullSplat, b))
		} else {
			p.operand(&b, 0, delimitedExpression)
			p.expect(&b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			parent.node(p.finish(IndexAccess, b))
		}
	}
}
