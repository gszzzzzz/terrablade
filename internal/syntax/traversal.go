package syntax

// traversalMode selects which steps a steps call may consume.
type traversalMode uint8

const (
	// allTraversalSteps accepts dot and bracket steps.
	allTraversalSteps traversalMode = iota
	// attributeTraversalSteps accepts only dot steps: the suffix of an
	// attribute splat, where a bracket step ends the projection and belongs
	// to the enclosing traversal instead.
	attributeTraversalSteps
)

// continuesTraversal reports whether kind begins another step in mode.
func continuesTraversal(kind TokenKind, mode traversalMode) bool {
	return kind == Dot || (kind == OpenBracket && mode != attributeTraversalSteps)
}

// steps keeps traversal steps flat except where splat semantics group suffixes.
// Attribute splats contain only following dot accesses; a full splat contains every
// following traversal, including nested splats. This follows upstream HCL's
// parseExpressionTraversals without introducing evaluated or synthetic nodes.
func (p *parser) steps(parent *nodeBuilder, context expressionContext, mode traversalMode) {
	// An absent suffix costs no recursive level. Otherwise a complete splat at
	// the depth boundary would fail merely while checking for another step.
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
		// Inter-step trivia belongs at traversal level; the step starts at '.' or '['.
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

// dotStep completes a step whose '.' is already in b: an attribute name, a
// legacy numeric index, or an attribute splat. It reports false when nothing
// valid follows the dot, which ends the traversal with an ErrorNode for the
// stray dot.
func (p *parser) dotStep(parent, b *nodeBuilder, context expressionContext, mode traversalMode) bool {
	switch p.peek(context) {
	case Identifier:
		p.consumeLookahead(b, context)
		parent.node(b.finish(AttributeAccess))
	case Number:
		// The lexer makes foo.0.1 the tokens foo, '.', and 0.1, so the legacy
		// index rule against a decimal point is checked on the number here.
		p.number(b, context, true)
		parent.node(b.finish(LegacyIndexAccess))
	case Star:
		if mode == attributeTraversalSteps {
			// Nested .* is invalid within a legacy projection. A preceding
			// bracket step would already have ended that projection.
			p.report(NestedAttributeSplat, p.tokens[p.look(context)].span)
			p.consumeLookahead(b, context)
			parent.node(b.finish(ErrorNode))
			return true
		}
		p.consumeLookahead(b, context)
		// Bracket indexing stays outside this legacy projection as a sibling.
		p.steps(b, context, attributeTraversalSteps)
		parent.node(b.finish(AttributeSplat))
	default:
		p.report(ExpectedAttributeName, p.tokens[p.look(context)].span)
		parent.node(b.finish(ErrorNode))
		return false
	}
	return true
}

// bracketStep completes a step whose '[' is already in b: a full splat [*] or
// an index expression. Upstream detects [*] in the outer newline context before
// entering ordinary index-expression mode. Thus a[\n*] is invalid outside
// parens, while a[\n0] is valid. Preserve this distinction and all raw trivia.
func (p *parser) bracketStep(parent, b *nodeBuilder, context expressionContext) {
	if p.peek(context) == Star {
		p.consumeLookahead(b, context)
		if p.expect(b, CloseBracket, ExpectedClosingBracket, context) {
			// Every suffix projects per element, so it stays inside this splat.
			p.steps(b, context, allTraversalSteps)
		}
		parent.node(b.finish(FullSplat))
		return
	}

	p.operand(b, lowestPower, delimitedExpression)
	p.expect(b, CloseBracket, ExpectedClosingBracket, delimitedExpression)
	parent.node(b.finish(IndexAccess))
}
