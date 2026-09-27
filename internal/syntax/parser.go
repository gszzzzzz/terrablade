package syntax

// maxRecursiveExpressionDepth bounds the parser's recursion, not the tree's
// height.
const maxRecursiveExpressionDepth = 1024

// newlineContext selects whether newlines and line comments end the construct
// being parsed. Attribute values, object items, and block headers end at the
// end of their line; inside (), [], or a template sequence, lines do not matter.
type newlineContext uint8

const (
	newlineTerminates newlineContext = iota
	newlineTransparent
)

// triviaClass distinguishes trivia that can end a line-sensitive construct.
type triviaClass uint8

const (
	notTrivia    triviaClass = iota
	inlineTrivia             // transparent in every context
	lineTrivia               // transparent only in newlineTransparent
)

func classifyTrivia(kind TokenKind) triviaClass {
	switch kind {
	case Whitespace, BlockComment:
		// HCL treats a block comment as inline whitespace even if it spans lines.
		return inlineTrivia
	case LineComment, Newline:
		return lineTrivia
	default:
		return notTrivia
	}
}

// parser is a recursive-descent parser over the lexer's tokens.
//
// When depth reaches maxRecursiveExpressionDepth, the parser halts: look and
// current then return EOF and consumeUntil and report do nothing, so every
// active production unwinds as if the source had ended. pos stays at the first
// unparsed token, and file keeps the remainder through retainUntil.
type parser struct {
	source string
	tokens []Token
	arena  *arena
	// pending holds the children of all open builders; each builder's
	// children follow its mark.
	pending     []elementRef
	pos         int // next unconsumed token
	diagnostics []Diagnostic
	depth       int // active expression and steps calls
	halted      bool
}

func newParser(source []byte) *parser {
	lexed := lex(source)
	return &parser{
		source:      string(source),
		tokens:      lexed.tokens,
		arena:       &arena{tokens: lexed.tokens},
		diagnostics: lexed.diagnostics,
	}
}

// look returns the index of the next token that is not trivia in context,
// without consuming anything. Trivia is consumed only with a following token,
// so trivia that ends a construct is left to the enclosing node.
func (p *parser) look(context newlineContext) int {
	if p.halted {
		return len(p.tokens) - 1
	}
	return p.lookFrom(p.pos, context)
}

// lookFrom is look starting at index, ignoring whether the parser halted.
func (p *parser) lookFrom(index int, context newlineContext) int {
	i := index
	for i < len(p.tokens)-1 {
		switch classifyTrivia(p.tokens[i].kind) {
		case inlineTrivia:
			i++
		case lineTrivia:
			if context == newlineTerminates {
				return i
			}
			i++
		default:
			return i
		}
	}
	return i
}

func (p *parser) peek(context newlineContext) TokenKind {
	return p.tokens[p.look(context)].kind
}

// keyword reports whether the next token is the contextual keyword word.
// Keywords stay Identifier tokens in the tree.
func (p *parser) keyword(word string, context newlineContext) bool {
	return p.keywordAt(word, p.look(context))
}

func (p *parser) keywordAt(word string, index int) bool {
	token := p.tokens[index]
	return token.kind == Identifier && p.source[token.span.Start:token.span.End] == word
}

// current returns the token at pos, or EOF once halted.
func (p *parser) current() Token {
	if p.halted {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos]
}

// nodeBuilder collects the children of a node under construction. Builders
// nest in grammar order and must finish in reverse order.
type nodeBuilder struct {
	parser *parser
	start  int // byte offset of the span, so an empty node has a position
	mark   int // the builder's children are parser.pending[mark:]
}

// begin starts a node at pos. After a halt, pos is still the real position,
// so an empty node leaves no gap before the unparsed remainder.
func (p *parser) begin() nodeBuilder {
	return p.beginAt(p.tokens[p.pos].span.Start)
}

// beginAt starts a node at start, so that an operator node can wrap a
// finished left operand.
func (p *parser) beginAt(start int) nodeBuilder {
	return nodeBuilder{parser: p, start: start, mark: len(p.pending)}
}

func (b *nodeBuilder) node(node Node) {
	b.parser.pending = append(b.parser.pending, elementRef(node.index+1))
}

// consumeUntil appends the tokens from pos up to index to b and advances pos.
// It does nothing once the parser has halted.
func (p *parser) consumeUntil(b *nodeBuilder, index int) {
	if p.halted {
		return
	}
	p.retainUntil(b, index)
}

// retainUntil is consumeUntil without the halt check. Only file assembly
// may use it.
func (p *parser) retainUntil(b *nodeBuilder, index int) {
	for p.pos < index {
		b.parser.pending = append(b.parser.pending, elementRef(-p.pos-1))
		p.pos++
	}
}

// consumeLookahead consumes through the next token in context, unless it is
// EOF, which only file appends.
func (p *parser) consumeLookahead(b *nodeBuilder, context newlineContext) {
	i := p.look(context)
	if p.tokens[i].kind != EOF {
		i++
	}
	p.consumeUntil(b, i)
}

// report records a diagnostic unless the parser has halted, so unwinding
// productions add nothing after NestingLimitExceeded.
func (p *parser) report(kind DiagnosticKind, span Span) {
	if p.halted {
		return
	}
	p.diagnostics = append(p.diagnostics, Diagnostic{Kind: kind, Span: span})
}

// haltAtLimit reports NestingLimitExceeded and halts the parser.
func (p *parser) haltAtLimit(span Span) {
	p.report(NestingLimitExceeded, span)
	p.halted = true
}

// finish moves the builder's children into the arena and returns the node.
// A node without children has an empty span at start.
func (b nodeBuilder) finish(kind NodeKind) Node {
	p := b.parser
	children := p.pending[b.mark:]
	span := Span{Start: b.start, End: b.start}
	if len(children) > 0 {
		span.End = (Element{arena: p.arena, ref: children[len(children)-1]}).Span().End
	}

	node := Node{arena: p.arena, index: len(p.arena.nodes)}
	p.arena.nodes = append(p.arena.nodes, nodeRecord{
		kind: kind, span: span,
		firstChild: len(p.arena.children), childCount: len(children),
	})
	p.arena.children = append(p.arena.children, children...)

	p.pending = p.pending[:b.mark]
	return node
}

// file appends the unparsed remainder and EOF to root and returns the Result.
func (p *parser) file(root nodeBuilder) Result {
	p.retainRemainder(&root)

	sortDiagnostics(p.diagnostics)

	// EOF is the last token.
	p.pending = append(p.pending, elementRef(-len(p.tokens)))
	return Result{
		source:      p.source,
		root:        root.finish(File),
		diagnostics: p.diagnostics,
	}
}

// retainRemainder appends any tokens the grammar did not consume, as after a
// halt: trivia directly to root, everything between in one flat ErrorNode.
func (p *parser) retainRemainder(root *nodeBuilder) {
	p.retainUntil(root, p.lookFrom(p.pos, newlineTransparent))

	if p.tokens[p.pos].kind != EOF {
		p.report(UnexpectedToken, p.tokens[p.pos].span)
		end := len(p.tokens) - 1
		for end > p.pos && p.tokens[end-1].kind.IsTrivia() {
			end--
		}
		rest := p.begin()
		p.retainUntil(&rest, end)
		root.node(rest.finish(ErrorNode))
	}

	p.retainUntil(root, len(p.tokens)-1)
}
