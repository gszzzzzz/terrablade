package syntax

// parseBodySource is the internal complete-file entry point. The public result
// contract is separate from the grammar and remains intentionally unexported.
func parseBodySource(source []byte) syntaxFile {
	p := newParser(source)
	root := p.begin()
	// Upstream accepts a single BOM at byte zero despite the native HCL spec.
	// Preserve it outside Body, where it cannot be confused with a body item.
	if p.current().kind == BOM {
		p.consumeUntil(&root, p.pos+1)
	}
	root.node(p.body())
	return p.file(root)
}

type bodyFrame struct {
	block     nodeBuilder
	body      nodeBuilder
	single    bool
	attribute bool
}

type bodyAttributeKey struct {
	// Nested bodies start after distinct consumed '{' tokens. Only the deepest
	// unfinished body can start at EOF, so body.start uniquely identifies scope.
	bodyStart int
	name      string
}

// Bodies nest iteratively. A deep block hierarchy therefore consumes arena and
// frame storage proportional to source size, without growing the Go call stack.
// The one attribute table keys by body start, keeping sibling scopes distinct
// without allocating a separate map for every small body.
func (p *parser) body() SyntaxNode {
	frames := []bodyFrame{{body: p.begin()}}
	attributes := make(map[bodyAttributeKey]struct{})
	for {
		frame := &frames[len(frames)-1]
		if frame.single && frame.attribute {
			kind := p.peek(lineExpression)
			if kind != CloseBrace && kind != EOF {
				p.report(ExpectedSingleLineBlockEnd, p.tokens[p.look(lineExpression)].span)
				p.recoverUntil(&frame.body, lineExpression, bodyItemBoundaries)
				// A newline after a malformed single-line body is a useful recovery
				// point: retain later attributes rather than swallowing the block.
				frame.single = false
			}
		}
		p.consumeUntil(&frame.body, p.look(delimitedExpression))
		kind := p.current().kind
		if kind == EOF || kind == CloseBrace && len(frames) > 1 {
			body := frame.body.finish(Body)
			if len(frames) == 1 {
				return body
			}
			block := frame.block
			block.node(body)
			p.expect(&block, CloseBrace, ExpectedClosingBrace, lineExpression)
			frames = frames[:len(frames)-1]
			parent := &frames[len(frames)-1].body
			parent.node(block.finish(Block))
			p.bodyItemEnd(parent)
			continue
		}
		if kind != Identifier {
			p.report(ExpectedBodyItem, p.current().span)
			if kind == CloseBrace {
				// Only the file body can encounter an unmatched brace here.
				bad := p.begin()
				p.consumeUntil(&bad, p.pos+1)
				frame.body.node(bad.finish(Error))
			} else {
				p.recoverUntil(&frame.body, lineExpression, bodyItemBoundaries)
			}
			frame.single = false
			continue
		}

		item := p.begin()
		name := p.current().span
		p.consumeUntil(&item, p.pos+1)
		if p.peek(lineExpression) == Equal {
			key := bodyAttributeKey{frame.body.start, p.source[name.Start:name.End]}
			if _, duplicate := attributes[key]; duplicate {
				p.report(DuplicateAttribute, name)
			}
			attributes[key] = struct{}{}
			p.consumeLookahead(&item, lineExpression)
			p.operand(&item, 0, lineExpression)
			frame.body.node(item.finish(Attribute))
			frame.attribute = true
			if !frame.single {
				p.bodyItemEnd(&frame.body)
			}
			continue
		}
		if frame.single {
			p.report(ExpectedSingleLineAttribute, name)
			// Finish the partial item before recovery reuses the pending tail.
			frame.body.node(item.finish(Error))
			p.recoverUntil(&frame.body, lineExpression, bodyItemBoundaries)
			frame.single = false
			continue
		}
		kind = p.peek(lineExpression)
		if kind != OpenBrace && kind != QuoteOpen && kind != Identifier {
			p.report(ExpectedAttributeOrBlock, p.tokens[p.look(lineExpression)].span)
			frame.body.node(item.finish(Error))
			p.recoverUntil(&frame.body, lineExpression, bodyItemBoundaries)
			continue
		}
		if !p.blockHeader(&item) {
			frame.body.node(item.finish(Block))
			p.recoverUntil(&frame.body, lineExpression, bodyItemBoundaries)
			continue
		}
		kind = p.peek(lineExpression)
		frames = append(frames, bodyFrame{
			block:  item,
			body:   p.begin(),
			single: !lineSeparators.has(kind) && kind != CloseBrace && kind != EOF,
		})
	}
}

// Only a single-line block's sole attribute may touch its containing '}'. All
// other attributes and blocks require a newline, including before an outer '}'.
// EOF can terminate a file's last item without a final newline, as upstream does.
func (p *parser) bodyItemEnd(body *nodeBuilder) {
	kind := p.peek(lineExpression)
	if lineSeparators.has(kind) || kind == EOF {
		return
	}
	p.report(ExpectedBodyItemSeparator, p.tokens[p.look(lineExpression)].span)
	p.recoverUntil(body, lineExpression, bodyItemBoundaries)
}

func (p *parser) blockHeader(block *nodeBuilder) bool {
	for {
		switch p.peek(lineExpression) {
		case OpenBrace:
			p.consumeLookahead(block, lineExpression)
			return true
		case Identifier, QuoteOpen:
			p.consumeUntil(block, p.look(lineExpression))
			label := p.begin()
			if p.current().kind == QuoteOpen {
				p.quotedBlockLabel(&label)
			} else {
				p.consumeUntil(&label, p.pos+1)
			}
			block.node(label.finish(BlockLabel))
		default:
			p.report(ExpectedBlockOpeningBrace, p.tokens[p.look(lineExpression)].span)
			return false
		}
	}
}

// Labels allow string escapes, but never template interpolation or directives.
// Invalid sequences remain raw Error subtrees, so they cannot be mistaken for
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
			next := p.look(delimitedExpression)
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
			label.node(bad.finish(Error))
		}
	}
}
