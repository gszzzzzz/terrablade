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

func (p *parser) current() SyntaxToken {
	if p.halted {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos]
}

type nodeBuilder struct {
	start    int
	children []SyntaxElement
}

func (p *parser) begin() nodeBuilder {
	// Empty error nodes stay at the real cursor, not the virtual EOF after a halt,
	// so they cannot create a gap between consumed and still-unparsed source.
	return nodeBuilder{start: p.tokens[p.pos].span.Start}
}

func (b *nodeBuilder) node(node SyntaxNode) {
	b.children = append(b.children, node)
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
		b.children = append(b.children, p.tokens[p.pos])
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
	span := Span{Start: b.start, End: b.start}
	if len(b.children) > 0 {
		span.End = b.children[len(b.children)-1].Span().End
	}
	return SyntaxNode{kind: kind, span: span, children: b.children}
}

func (p *parser) file(root nodeBuilder) syntaxFile {
	p.retainRemainder(&root)
	// Lexical diagnostics can overlap later parser diagnostics; ties retain their
	// original order, with lexical diagnostics first.
	sort.SliceStable(p.diagnostics, func(i, j int) bool {
		return p.diagnostics[i].Span.Start < p.diagnostics[j].Span.Start
	})
	root.children = append(root.children, SyntaxToken{
		kind: EOF,
		span: p.tokens[len(p.tokens)-1].span,
	})
	return syntaxFile{
		source: p.source,
		root: SyntaxNode{
			kind:     File,
			span:     Span{Start: 0, End: len(p.source)},
			children: root.children,
		},
		diagnostics: p.diagnostics,
	}
}

// retainRemainder is deliberately outside the grammar cursor. It must see the
// original tail even when productions see EOF after a limit. Outer trivia stays
// at File level, and unparsed non-trivia remains a flat, bounded Error node.
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
		root.node(rest.finish(Error))
	}
	p.retainUntil(root, len(p.tokens)-1)
}
