package syntax

type traversalMode uint8

const (
	allTraversalSteps traversalMode = iota
	attributeTraversalSteps
)

// steps keeps traversal steps flat except where splat semantics group suffixes.
// Attribute splats own only following dot accesses; a full splat owns every
// following traversal, including nested splats. This follows upstream HCL's
// parseExpressionTraversals without introducing evaluated or synthetic nodes.
func (p *parser) steps(parent *nodeBuilder, context expressionContext, mode traversalMode) {
	if kind := p.peek(context); kind != Dot && (kind != OpenBracket || mode == attributeTraversalSteps) {
		return
	}
	if p.depth == maxExpressionDepth {
		p.haltAtLimit(p.tokens[p.look(context)].Span)
		return
	}
	p.depth++
	defer func() { p.depth-- }()
	for {
		kind := p.peek(context)
		if kind != Dot && (kind != OpenBracket || mode == attributeTraversalSteps) {
			return
		}
		p.consumeUntil(parent, p.look(context))
		b := p.begin()
		p.consumeLookahead(&b, context)
		if kind == Dot {
			switch p.peek(context) {
			case Identifier:
				p.consumeLookahead(&b, context)
				parent.node(p.finish(AttributeAccess, b))
			case Number:
				p.number(&b, context, true)
				parent.node(p.finish(LegacyIndexAccess, b))
			case Star:
				if mode == attributeTraversalSteps {
					p.report(NestedAttributeSplat, p.tokens[p.look(context)].Span)
					p.consumeLookahead(&b, context)
					parent.node(p.finish(Error, b))
					continue
				}
				p.consumeLookahead(&b, context)
				p.steps(&b, context, attributeTraversalSteps)
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
			p.consumeLookahead(&b, context)
			if p.expect(&b, CloseBracket, ExpectedClosingBracket, context) {
				p.steps(&b, context, allTraversalSteps)
			}
			parent.node(p.finish(FullSplat, b))
		} else {
			p.operand(&b, 0, delimitedExpression)
			p.expect(&b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
			parent.node(p.finish(IndexAccess, b))
		}
	}
}
