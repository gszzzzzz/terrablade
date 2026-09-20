package syntax

// maxRecursiveExpressionDepth bounds the parser's own recursive descent, not
// the height of the resulting CST. Iterative productions may legitimately build
// deep trees without growing the parser call stack; downstream consumers must
// traverse those iteratively.
const maxRecursiveExpressionDepth = 1024

// newlineContext selects whether newlines and line comments end the construct
// being parsed. An attribute value, an object item, and a block header end at
// the end of their line, while anything inside (), [], {}, or a template
// sequence spans lines freely, so the same production is parsed under either
// rule depending on where it appears. Body parsing uses it too: a body item
// ends at its line, while the trivia between items does not.
type newlineContext uint8

const (
	// newlineTerminates stops lookahead at a Newline or LineComment, which the
	// enclosing production then treats as its terminator.
	newlineTerminates newlineContext = iota
	// newlineTransparent treats Newline and LineComment as trivia.
	newlineTransparent
)

// triviaClass separates the two kinds of trivia because only one of them can
// end a line-sensitive construct.
type triviaClass uint8

const (
	notTrivia triviaClass = iota
	// inlineTrivia is transparent to lookahead in every context.
	inlineTrivia
	// lineTrivia is transparent only in newlineTransparent context.
	lineTrivia
)

// classifyTrivia reports how lookahead treats a token kind.
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

func isTrivia(kind TokenKind) bool { return classifyTrivia(kind) != notTrivia }

// parser is a single-pass recursive-descent parser over the lexer's tokens.
// Three fields cooperate to bound recursion without losing source. depth
// counts the recursive productions currently active. When it reaches
// maxRecursiveExpressionDepth, haltAtLimit sets halted; from then on look and
// current present EOF while consumeUntil and report do nothing, so every
// active production unwinds as if the source had ended, and pos stays frozen
// at the first unparsed token. file then retains everything from pos through
// retainUntil, the one path that bypasses the gate.
type parser struct {
	source string
	tokens []SyntaxToken
	// arena receives finished nodes and their child references; every handle
	// returned to callers points into it.
	arena *syntaxArena
	// pending is the child stack shared by all open builders. A builder owns
	// the tail after its mark, and finish moves that tail into arena.children.
	pending []elementRef
	// pos indexes the next unconsumed token. It only moves forward.
	pos         int
	diagnostics []Diagnostic
	// depth counts nested expression and steps calls; see the type comment.
	depth int
	// halted is set once by haltAtLimit and never cleared; see the type comment.
	halted bool
}

func newParser(source []byte) *parser {
	lexed := lex(source)
	return &parser{
		source:      string(source),
		tokens:      lexed.Tokens,
		arena:       &syntaxArena{tokens: lexed.Tokens},
		diagnostics: lexed.Diagnostics,
	}
}

// look returns the index of the next grammatical token without consuming
// trivia. Horizontal spaces and block comments are always transparent; line
// comments and newlines are transparent only inside delimiters. Committing a
// grammatical token later also commits its intervening trivia to the enclosing
// node. Unused lookahead leaves trailing trivia with the parent.
func (p *parser) look(context newlineContext) int {
	if p.halted {
		return len(p.tokens) - 1
	}
	return p.lookFrom(p.pos, context)
}

// lookFrom scans forward from index over the trivia that context makes
// transparent and returns the index of the next grammatical token, or of the
// line trivia that ends a newlineTerminates construct. It ignores the halt
// gate: file recovery needs this raw view after shutdown, while grammar uses
// look.
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

// keyword reports whether the next grammatical token is the contextual keyword
// word. Keywords are ordinary identifiers, so the tree keeps their lexical
// kind.
func (p *parser) keyword(word string, context newlineContext) bool {
	return p.keywordAt(word, p.look(context))
}

// keywordAt tests the token at a chosen index. Collection lookahead uses it to
// test after an opener without consuming it. All keyword checks share the same
// Identifier/text comparison.
func (p *parser) keywordAt(word string, index int) bool {
	token := p.tokens[index]
	return token.kind == Identifier && p.source[token.span.Start:token.span.End] == word
}

// current returns the token at the cursor, or EOF once halted, so productions
// that inspect the cursor directly see the same exhausted stream as look.
func (p *parser) current() SyntaxToken {
	if p.halted {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos]
}

// nodeBuilder is a cursor snapshot for one node under construction: where the
// node starts and which pending children belong to it. The parser appends
// children through it, and finish turns them into an arena node. Builders are
// small values; copying one is cheap and does not duplicate children.
type nodeBuilder struct {
	parser *parser
	// start is the byte offset where the node's span begins, fixed at begin so
	// that an empty node still has a position.
	start int
	// mark is the length of parser.pending when the builder began; the
	// builder's children are pending[mark:].
	mark int
}

// begin starts a node at the real cursor. Empty error nodes stay at the real
// cursor, not the virtual EOF after a halt, so they cannot create a gap between
// consumed and still-unparsed source.
func (p *parser) begin() nodeBuilder {
	return p.beginAt(p.tokens[p.pos].span.Start)
}

// beginAt starts a node at an explicit offset, which lets an operator node wrap
// an already finished left operand. Builders nest in grammar order. Each owns
// the tail of pending after mark; finishing a nested builder restores that
// tail before its parent appends the resulting node. Sharing this scratch
// buffer avoids one allocation per node.
func (p *parser) beginAt(start int) nodeBuilder {
	return nodeBuilder{parser: p, start: start, mark: len(p.pending)}
}

func (b *nodeBuilder) node(node SyntaxNode) {
	b.parser.pending = append(b.parser.pending, elementRef(node.index+1))
}

func (p *parser) consumeUntil(b *nodeBuilder, index int) {
	if p.halted {
		return
	}
	p.retainUntil(b, index)
}

// retainUntil copies the raw tokens before index into the builder. Raw copying
// is shared, but only file assembly may bypass consumeUntil's halt gate.
// Productions must never consume from this view after a limit.
func (p *parser) retainUntil(b *nodeBuilder, index int) {
	for p.pos < index {
		b.parser.pending = append(b.parser.pending, elementRef(-p.pos-1))
		p.pos++
	}
}

// consumeLookahead includes the looked-ahead grammatical token, but never EOF.
// The file assembler alone owns EOF, preventing duplicate sentinel leaves.
func (p *parser) consumeLookahead(b *nodeBuilder, context newlineContext) {
	i := p.look(context)
	if p.tokens[i].kind != EOF {
		i++
	}
	p.consumeUntil(b, i)
}

// report records a diagnostic unless the parser has halted, so the single
// NestingLimitExceeded diagnostic is not followed by a cascade from the
// productions unwinding after it.
func (p *parser) report(kind DiagnosticKind, span Span) {
	if p.halted {
		return
	}
	p.diagnostics = append(p.diagnostics, Diagnostic{Kind: kind, Span: span})
}

// haltAtLimit makes the cursor appear exhausted to every production. The real
// position is frozen so file assembly can retain the rest without parsing it.
// Reporting through the same gate prevents cascades after this one diagnostic.
func (p *parser) haltAtLimit(span Span) {
	p.report(NestingLimitExceeded, span)
	p.halted = true
}

// finish moves the builder's children from pending into the arena and returns
// the new node. The span runs from start to the end of the last child, or is
// empty at start when there are none, which is how a missing operand is
// represented without a synthetic token. Truncating pending restores the
// parent's view, so nested builders must finish in LIFO order.
func (b nodeBuilder) finish(kind NodeKind) SyntaxNode {
	p := b.parser
	children := p.pending[b.mark:]
	span := Span{Start: b.start, End: b.start}
	if len(children) > 0 {
		span.End = (SyntaxElement{arena: p.arena, ref: children[len(children)-1]}).Span().End
	}

	node := SyntaxNode{arena: p.arena, index: len(p.arena.nodes)}
	p.arena.nodes = append(p.arena.nodes, nodeRecord{
		kind: kind, span: span,
		firstChild: len(p.arena.children), childCount: len(children),
	})
	p.arena.children = append(p.arena.children, children...)

	p.pending = p.pending[:b.mark]
	return node
}

// file assembles the File node once the grammar has finished: it retains any
// unparsed remainder, merges the diagnostic order, and appends the one EOF
// leaf.
func (p *parser) file(root nodeBuilder) Result {
	p.retainRemainder(&root)

	// Lexical diagnostics can overlap later parser diagnostics; ties retain their
	// original order, with lexical diagnostics first.
	sortDiagnostics(p.diagnostics)

	// EOF is the last token, and a token's reference is -(index+1), so EOF's
	// reference is -len(tokens). Appending it here rather than through
	// consumeUntil is what makes file the only owner of EOF.
	p.pending = append(p.pending, elementRef(-len(p.tokens)))
	return Result{
		source:      p.source,
		root:        root.finish(File),
		diagnostics: p.diagnostics,
	}
}

// retainRemainder is deliberately outside the grammar cursor. It must see the
// original tail even when productions see EOF after a limit. Outer trivia stays
// at File level, and unparsed non-trivia remains a flat, bounded ErrorNode.
func (p *parser) retainRemainder(root *nodeBuilder) {
	p.retainUntil(root, p.lookFrom(p.pos, newlineTransparent))

	if p.tokens[p.pos].kind != EOF {
		p.report(UnexpectedToken, p.tokens[p.pos].span)
		end := len(p.tokens) - 1
		for end > p.pos && isTrivia(p.tokens[end-1].kind) {
			end--
		}
		rest := p.begin()
		p.retainUntil(&rest, end)
		root.node(rest.finish(ErrorNode))
	}

	p.retainUntil(root, len(p.tokens)-1)
}
