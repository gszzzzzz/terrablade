package syntax

// traversalMode selects which steps a steps call may consume.
type traversalMode uint8

const (
	allTraversalSteps traversalMode = iota
	// attributeTraversalSteps accepts only dot steps, for the suffix of an
	// attribute splat.
	attributeTraversalSteps
)

func continuesTraversal(kind TokenKind, mode traversalMode) bool {
	return kind == Dot || (kind == OpenBracket && mode != attributeTraversalSteps)
}

// steps parses traversal steps into parent. As in upstream HCL, an attribute
// splat contains the dot steps after it, and a full splat all steps after it.
func (p *parser) steps(parent *nodeBuilder, context newlineContext, mode traversalMode) {
	// Take a depth level only when a step follows.
	if !continuesTraversal(p.peek(context), mode) {
		return
	}
	if p.depth == maxRecursiveExpressionDepth {
		p.haltAtLimit(p.tokens[p.look(context)].span)
		return
	}
	p.depth++
	defer func() { p.depth-- }()

	for {
		kind := p.peek(context)
		if !continuesTraversal(kind, mode) {
			return
		}
		// Trivia between steps belongs to the traversal.
		p.consumeUntil(parent, p.look(context))
		b := p.begin()
		p.consumeLookahead(&b, context)

		if kind == Dot {
			if !p.dotStep(parent, &b, context, mode) {
				return
			}
			continue
		}
		p.bracketStep(parent, &b, context)
	}
}

// dotStep completes a step whose '.' is in b. It reports false for a stray
// dot, which ends the traversal.
func (p *parser) dotStep(parent, b *nodeBuilder, context newlineContext, mode traversalMode) bool {
	switch p.peek(context) {
	case Identifier:
		p.consumeLookahead(b, context)
		parent.node(b.finish(AttributeAccess))
	case Number:
		p.number(b, context, true)
		parent.node(b.finish(LegacyIndexAccess))
	case Star:
		if mode == attributeTraversalSteps {
			p.report(NestedAttributeSplat, p.tokens[p.look(context)].span)
			p.consumeLookahead(b, context)
			parent.node(b.finish(ErrorNode))
			return true
		}
		p.consumeLookahead(b, context)
		p.steps(b, context, attributeTraversalSteps)
		parent.node(b.finish(AttributeSplat))
	default:
		p.report(ExpectedAttributeName, p.tokens[p.look(context)].span)
		parent.node(b.finish(ErrorNode))
		return false
	}
	return true
}

// bracketStep completes a step whose '[' is in b. As upstream, [*] is matched
// in the outer newline context, so a[\n*] is invalid outside parentheses while
// a[\n0] is valid.
func (p *parser) bracketStep(parent, b *nodeBuilder, context newlineContext) {
	if p.peek(context) == Star {
		p.consumeLookahead(b, context)
		if p.expect(b, CloseBracket, ExpectedClosingBracket, context) {
			p.steps(b, context, allTraversalSteps)
		}
		parent.node(b.finish(FullSplat))
		return
	}

	p.operand(b, lowestPower, newlineTransparent)
	p.expectCloser(b, CloseBracket, ExpectedClosingBracket)
	parent.node(b.finish(IndexAccess))
}
