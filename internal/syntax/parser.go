package syntax

import "sort"

// The parser bounds its own recursive descent, not the height of the resulting
// CST. Iterative productions may legitimately build deep trees without growing
// the parser call stack; downstream consumers must traverse those iteratively.
const maxRecursiveExpressionDepth = 1024

type expressionContext uint8

const (
	lineExpression expressionContext = iota
	delimitedExpression
)

type triviaClass uint8

const (
	notTrivia triviaClass = iota
	inlineTrivia
	lineTrivia
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

func isTrivia(kind TokenKind) bool { return classifyTrivia(kind) != notTrivia }

type parser struct {
	source      string
	tokens      []SyntaxToken
	arena       *syntaxArena
	pending     []elementRef
	pos         int
	diagnostics []Diagnostic
	depth       int
	halted      bool
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

// look does not consume trivia. Horizontal spaces and block comments are always
// transparent; line comments and newlines are transparent only inside delimiters.
// Committing a grammatical token later also commits its intervening trivia to
// the enclosing node. Unused lookahead leaves trailing trivia with the parent.
func (p *parser) look(context expressionContext) int {
	if p.halted {
		return len(p.tokens) - 1
	}
	return p.lookFrom(p.pos, context)
}

// File recovery needs this raw view after shutdown; grammar uses look instead.
func (p *parser) lookFrom(index int, context expressionContext) int {
	i := index
	for i < len(p.tokens)-1 {
		switch classifyTrivia(p.tokens[i].kind) {
		case inlineTrivia:
			i++
		case lineTrivia:
			if context == lineExpression {
				return i
			}
			i++
		default:
			return i
		}
	}
	return i
}

func (p *parser) peek(context expressionContext) TokenKind {
	return p.tokens[p.look(context)].kind
}

func (p *parser) keyword(word string, context expressionContext) bool {
	return p.keywordAt(word, p.look(context))
}

// A chosen token index lets collection lookahead test after its opener without
// consuming it. All keyword checks share the same Identifier/text comparison.
func (p *parser) keywordAt(word string, index int) bool {
	token := p.tokens[index]
	return token.kind == Identifier && p.source[token.span.Start:token.span.End] == word
}

func (p *parser) current() SyntaxToken {
	if p.halted {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos]
}

type nodeBuilder struct {
	parser *parser
	start  int
	mark   int
}

func (p *parser) begin() nodeBuilder {
	// Empty error nodes stay at the real cursor, not the virtual EOF after a halt,
	// so they cannot create a gap between consumed and still-unparsed source.
	return p.beginAt(p.tokens[p.pos].span.Start)
}

// Builders nest in grammar order. Each owns the tail of pending after mark;
// finishing a nested builder restores that tail before its parent appends the
// resulting node. Sharing this scratch buffer avoids one allocation per node.
func (p *parser) beginAt(start int) nodeBuilder {
	return nodeBuilder{parser: p, start: start, mark: len(p.pending)}
}

func (b *nodeBuilder) node(node SyntaxNode) {
	b.parser.pending = append(b.parser.pending, elementRef(node.index+1))
}

// consumeUntil appends raw tokens before index, leaving index unconsumed.
func (p *parser) consumeUntil(b *nodeBuilder, index int) {
	if p.halted {
		return
	}
	p.retainUntil(b, index)
}

// Raw copying is shared, but only file assembly may bypass consumeUntil's halt
// gate. Productions must never consume from this view after a limit.
func (p *parser) retainUntil(b *nodeBuilder, index int) {
	for p.pos < index {
		b.parser.pending = append(b.parser.pending, elementRef(-p.pos-1))
		p.pos++
	}
}

// consumeLookahead includes the looked-ahead grammatical token, but never EOF.
// The file assembler alone owns EOF, preventing duplicate sentinel leaves.
func (p *parser) consumeLookahead(b *nodeBuilder, context expressionContext) {
	i := p.look(context)
	if p.tokens[i].kind != EOF {
		i++
	}
	p.consumeUntil(b, i)
}

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

func (p *parser) file(root nodeBuilder) Result {
	p.retainRemainder(&root)
	// Lexical diagnostics can overlap later parser diagnostics; ties retain their
	// original order, with lexical diagnostics first.
	sort.SliceStable(p.diagnostics, func(i, j int) bool {
		return p.diagnostics[i].Span.Start < p.diagnostics[j].Span.Start
	})
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
	p.retainUntil(root, p.lookFrom(p.pos, delimitedExpression))
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
