package syntax

// bodyFrame is one open body in the iterative descent through blocks. The
// file body has no block; every nested frame pairs the block's builder, which
// already holds the header, with the builder for its body.
type bodyFrame struct {
	block nodeBuilder
	body  nodeBuilder
	// single marks a block whose body began on the header line, as in
	// b { a = 1 }. Upstream allows exactly one attribute there, followed by the
	// closing brace on the same line. Recovery clears single once that form is
	// broken so the following lines parse as an ordinary body.
	single bool
	// attribute records that the body has parsed an attribute, after which a
	// single-line body must end.
	attribute bool
}

// bodyAttributeKey identifies an attribute name within one body so that one
// map can diagnose duplicates per scope.
type bodyAttributeKey struct {
	// Nested bodies start after distinct consumed '{' tokens. Only the deepest
	// unfinished body can start at EOF, so body.start uniquely identifies scope.
	bodyStart int
	name      string
}

// body parses the file body and every block body nested in it. Bodies nest
// iteratively: a deep block hierarchy consumes arena and frame storage
// proportional to source size, without growing the Go call stack. The one
// attribute table keys by body start, keeping sibling scopes distinct without
// allocating a separate map for every small body. Each loop iteration ends a
// single-line body, closes a block, or parses one item.
func (p *parser) body() SyntaxNode {
	frames := []bodyFrame{{body: p.begin()}}
	attributes := make(map[bodyAttributeKey]struct{})
	for {
		frame := &frames[len(frames)-1]
		if frame.single && frame.attribute {
			p.singleLineBodyEnd(frame)
		}

		p.consumeUntil(&frame.body, p.look(newlineTransparent))
		kind := p.current().kind
		if len(frames) == 1 && kind == EOF {
			return frame.body.finish(Body)
		}
		if len(frames) > 1 && (kind == EOF || kind == CloseBrace) {
			frames = p.closeBlock(frames)
			continue
		}

		// An unmatched '}' in the file body reaches bodyItem like any other
		// token that cannot start an item.
		frames = p.bodyItem(frames, attributes)
	}
}

// singleLineBodyEnd enforces that a single-line block ends right after its one
// attribute, which is upstream's stricter single-line rule. After reporting,
// the frame becomes an ordinary body: a newline after a malformed single-line
// body is a useful recovery point, retaining later attributes rather than
// swallowing the block.
func (p *parser) singleLineBodyEnd(frame *bodyFrame) {
	kind := p.peek(newlineTerminates)
	if kind == CloseBrace || kind == EOF {
		return
	}
	p.report(ExpectedSingleLineBlockEnd, p.tokens[p.look(newlineTerminates)].span)
	p.recoverUntil(&frame.body, newlineTerminates, bodyItemBoundaries)
	frame.single = false
}

// closeBlock finishes the innermost block at its '}' or at EOF and appends it
// to the enclosing body. Like any other item, the block must be followed by a
// line end, which bodyItemEnd checks on the parent's behalf.
func (p *parser) closeBlock(frames []bodyFrame) []bodyFrame {
	frame := frames[len(frames)-1]
	block := frame.block
	block.node(frame.body.finish(Body))
	p.expect(&block, CloseBrace, ExpectedClosingBrace, newlineTerminates)

	frames = frames[:len(frames)-1]
	parent := &frames[len(frames)-1].body
	parent.node(block.finish(Block))
	p.bodyItemEnd(parent)
	return frames
}

// bodyItem parses one attribute or block header at the cursor, or recovers
// from a token that cannot start either. It returns the frame stack, extended
// by one frame when a block header opened a new body.
func (p *parser) bodyItem(frames []bodyFrame, attributes map[bodyAttributeKey]struct{}) []bodyFrame {
	frame := &frames[len(frames)-1]
	kind := p.current().kind
	if kind != Identifier {
		p.report(ExpectedBodyItem, p.current().span)
		if kind == CloseBrace {
			// Only the file body can encounter an unmatched brace here. It is a
			// recovery boundary, so recoverUntil would not consume it; wrapping
			// just that token is what guarantees progress.
			bad := p.begin()
			p.consumeUntil(&bad, p.pos+1)
			frame.body.node(bad.finish(ErrorNode))
		} else {
			p.recoverUntil(&frame.body, newlineTerminates, bodyItemBoundaries)
		}
		frame.single = false
		return frames
	}

	item := p.begin()
	name := p.current().span
	p.consumeUntil(&item, p.pos+1)
	if p.peek(newlineTerminates) == Equal {
		p.bodyAttribute(frame, &item, name, attributes)
		return frames
	}
	if frame.single {
		// Upstream allows only an attribute in a single-line block, so a name
		// without '=' cannot start a nested block here.
		p.report(ExpectedSingleLineAttribute, name)
		// Finish the partial item before recovery reuses the pending tail.
		frame.body.node(item.finish(ErrorNode))
		p.recoverUntil(&frame.body, newlineTerminates, bodyItemBoundaries)
		frame.single = false
		return frames
	}

	next := p.peek(newlineTerminates)
	if next != OpenBrace && next != QuoteOpen && next != Identifier {
		p.report(ExpectedAttributeOrBlock, p.tokens[p.look(newlineTerminates)].span)
		frame.body.node(item.finish(ErrorNode))
		p.recoverUntil(&frame.body, newlineTerminates, bodyItemBoundaries)
		return frames
	}
	if !p.blockHeader(&item) {
		frame.body.node(item.finish(Block))
		p.recoverUntil(&frame.body, newlineTerminates, bodyItemBoundaries)
		return frames
	}

	// A body that begins on the header line is single-line unless that line
	// ends immediately, which leaves an ordinary multi-line body.
	next = p.peek(newlineTerminates)
	return append(frames, bodyFrame{
		block:  item,
		body:   p.begin(),
		single: !lineSeparators.has(next) && next != CloseBrace && next != EOF,
	})
}

// bodyAttribute parses the rest of an attribute whose name is already in item
// and whose '=' is next. Duplicate names within one body are an error;
// attributes, keyed by body start, tracks them across the iterative descent.
func (p *parser) bodyAttribute(frame *bodyFrame, item *nodeBuilder, name Span, attributes map[bodyAttributeKey]struct{}) {
	key := bodyAttributeKey{frame.body.start, p.source[name.Start:name.End]}
	if _, duplicate := attributes[key]; duplicate {
		p.report(DuplicateAttribute, name)
	}
	attributes[key] = struct{}{}

	p.consumeLookahead(item, newlineTerminates)
	p.operand(item, lowestPower, newlineTerminates)
	frame.body.node(item.finish(Attribute))
	frame.attribute = true
	// A single-line body checks its end at the top of the body loop instead.
	if !frame.single {
		p.bodyItemEnd(&frame.body)
	}
}

// bodyItemEnd requires a line end after an attribute or block. Only a
// single-line block's sole attribute may touch its containing '}'. All other
// attributes and blocks require a newline, including before an outer '}'. EOF
// can terminate a file's last item without a final newline, as upstream does.
func (p *parser) bodyItemEnd(body *nodeBuilder) {
	kind := p.peek(newlineTerminates)
	if lineSeparators.has(kind) || kind == EOF {
		return
	}
	p.report(ExpectedBodyItemSeparator, p.tokens[p.look(newlineTerminates)].span)
	p.recoverUntil(body, newlineTerminates, bodyItemBoundaries)
}

// blockHeader parses the labels after a block type and its opening brace,
// reporting whether the brace was found. Labels are identifiers or quoted
// literals, never expressions, so a label sees only the lexer's tokens.
func (p *parser) blockHeader(block *nodeBuilder) bool {
	for {
		switch p.peek(newlineTerminates) {
		case OpenBrace:
			p.consumeLookahead(block, newlineTerminates)
			return true
		case Identifier, QuoteOpen:
			p.consumeUntil(block, p.look(newlineTerminates))
			label := p.begin()
			if p.current().kind == QuoteOpen {
				p.quotedBlockLabel(&label)
			} else {
				p.consumeUntil(&label, p.pos+1)
			}
			block.node(label.finish(BlockLabel))
		default:
			p.report(ExpectedBlockOpeningBrace, p.tokens[p.look(newlineTerminates)].span)
			return false
		}
	}
}

// quotedBlockLabel parses a quoted label from its opening quote. Labels allow
// string escapes, but never template interpolation or directives. Invalid
// sequences remain raw ErrorNode subtrees, so they cannot be mistaken for
// references or executable expressions by consumers of a partial tree.
func (p *parser) quotedBlockLabel(label *nodeBuilder) {
	p.consumeUntil(label, p.pos+1)
	for {
		switch p.current().kind {
		case QuoteClose:
			p.consumeUntil(label, p.pos+1)
			return
		case EOF:
			// The lexer already reports an unterminated quote at its opener.
			return
		case TemplateText:
			p.consumeUntil(label, p.pos+1)
		case Whitespace, Newline, LineComment, BlockComment:
			// Recovery from an unterminated sequence can leave expression trivia
			// at EOF. Keep that tail with Body, as for unfinished expressions.
			next := p.look(newlineTransparent)
			if p.tokens[next].kind == EOF {
				return
			}
			p.consumeUntil(label, next)
		default:
			p.report(ExpectedLiteralBlockLabel, p.current().span)
			bad := p.begin()
			if closing(p.current().kind) != Invalid {
				p.skipConstruct(&bad)
			} else {
				p.consumeUntil(&bad, p.pos+1)
			}
			label.node(bad.finish(ErrorNode))
		}
	}
}
