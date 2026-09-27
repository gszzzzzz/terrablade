package syntax

// bodyFrame is one open body. For a block body, block holds the header.
type bodyFrame struct {
	block nodeBuilder
	body  nodeBuilder
	// single marks a body that began on the header line, as in b { a = 1 },
	// which may hold only one attribute. Recovery clears it.
	single    bool
	attribute bool // the body has an attribute
}

// bodyAttributeKey identifies an attribute name within one body. Distinct
// bodies have distinct start offsets.
type bodyAttributeKey struct {
	bodyStart int
	name      string
}

// body parses the file body and, iteratively, every block body nested in it.
func (p *parser) body() Node {
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

		frames = p.bodyItem(frames, attributes)
	}
}

// singleLineBodyEnd requires a single-line body to end after its attribute.
// On error the body continues as an ordinary multi-line body.
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
// to the enclosing body.
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

// bodyItem parses one attribute or block header, pushing a frame for a block
// body.
func (p *parser) bodyItem(frames []bodyFrame, attributes map[bodyAttributeKey]struct{}) []bodyFrame {
	frame := &frames[len(frames)-1]
	kind := p.current().kind
	if kind != Identifier {
		p.report(ExpectedBodyItem, p.current().span)
		if kind == CloseBrace {
			// An unmatched '}' in the file body. recoverUntil stops at it,
			// so consume it here.
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
		p.report(ExpectedSingleLineAttribute, name)
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

	// A body that starts on the header line is single-line.
	next = p.peek(newlineTerminates)
	return append(frames, bodyFrame{
		block:  item,
		body:   p.begin(),
		single: !lineSeparators.has(next) && next != CloseBrace && next != EOF,
	})
}

// bodyAttribute parses the rest of an attribute whose name is in item and
// whose '=' is next.
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
	// The body loop checks the end of a single-line body.
	if !frame.single {
		p.bodyItemEnd(&frame.body)
	}
}

// bodyItemEnd requires a newline or EOF after an attribute or block. Only the
// attribute of a single-line body may be followed directly by '}'.
func (p *parser) bodyItemEnd(body *nodeBuilder) {
	kind := p.peek(newlineTerminates)
	if lineSeparators.has(kind) || kind == EOF {
		return
	}
	p.report(ExpectedBodyItemSeparator, p.tokens[p.look(newlineTerminates)].span)
	p.recoverUntil(body, newlineTerminates, bodyItemBoundaries)
}

// blockHeader parses the labels and opening brace after a block type,
// reporting whether the brace was found.
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

// quotedBlockLabel parses a quoted label. Labels allow escapes but not
// template sequences, which are kept as raw ErrorNodes.
func (p *parser) quotedBlockLabel(label *nodeBuilder) {
	p.consumeUntil(label, p.pos+1)
	for {
		switch p.current().kind {
		case QuoteClose:
			p.consumeUntil(label, p.pos+1)
			return
		// The lexer reports the unterminated quote.
		case EOF:
			return
		case TemplateText:
			p.consumeUntil(label, p.pos+1)
		case Whitespace, Newline, LineComment, BlockComment:
			// Trivia at EOF, left by an unterminated sequence, belongs to Body.
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
