package syntax

import "sort"

// Limits are implementation details. Both recursion and constructed tree height
// are bounded: iterative left-associative operators can also make deep trees.
const maxExpressionDepth = 256

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

func classifyTrivia(kind Kind) triviaClass {
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

func isTrivia(kind Kind) bool { return classifyTrivia(kind) != notTrivia }

type parser struct {
	source      string
	tokens      []token
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
		switch classifyTrivia(p.tokens[i].Kind) {
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

func (p *parser) peek(context expressionContext) Kind {
	return p.tokens[p.look(context)].Kind
}

func (p *parser) current() token {
	if p.halted {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.pos]
}

type nodeBuilder struct {
	start    int
	children []SyntaxElement
	height   int
}

func (p *parser) begin() nodeBuilder {
	// Empty error nodes stay at the real cursor, not the virtual EOF after a halt,
	// so they cannot create a gap between consumed and still-unparsed source.
	return nodeBuilder{start: p.tokens[p.pos].Span.Start, height: 1}
}

func (b *nodeBuilder) node(node SyntaxNode) {
	b.children = append(b.children, node)
	b.height = max(b.height, node.height+1)
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
		token := p.tokens[p.pos]
		b.children = append(b.children, SyntaxToken{kind: token.Kind, span: token.Span})
		p.pos++
	}
}

// consumeLookahead includes the looked-ahead grammatical token, but never EOF.
// The file assembler alone owns EOF, preventing duplicate sentinel leaves.
func (p *parser) consumeLookahead(b *nodeBuilder, context expressionContext) {
	i := p.look(context)
	if p.tokens[i].Kind != EOF {
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

func (p *parser) finish(kind NodeKind, b nodeBuilder) SyntaxNode {
	span := Span{Start: b.start, End: b.start}
	if len(b.children) > 0 {
		span.End = b.children[len(b.children)-1].Span().End
	}
	if b.height > maxExpressionDepth {
		p.haltAtLimit(span)
		// Flatten only the overflowing structure into a lossless Error node.
		// This also bounds the tree seen by future recursive consumers.
		var leaves []SyntaxElement
		// The LIFO walk visits rightmost leaves first. Reversing once afterward
		// restores source order without recursion through the overflowing tree.
		stack := append([]SyntaxElement(nil), b.children...)
		for len(stack) > 0 {
			element := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			switch element := element.(type) {
			case SyntaxToken:
				leaves = append(leaves, element)
			case SyntaxNode:
				stack = append(stack, element.children...)
			}
		}
		for i, j := 0, len(leaves)-1; i < j; i, j = i+1, j-1 {
			leaves[i], leaves[j] = leaves[j], leaves[i]
		}
		b.children, b.height, kind = leaves, 1, Error
	}
	return SyntaxNode{kind: kind, span: span, children: b.children, height: b.height}
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
		span: p.tokens[len(p.tokens)-1].Span,
	})
	return syntaxFile{
		source: p.source,
		root: SyntaxNode{
			kind:     File,
			span:     Span{Start: 0, End: len(p.source)},
			children: root.children,
			height:   root.height,
		},
		diagnostics: p.diagnostics,
	}
}

// retainRemainder is deliberately outside the grammar cursor. It must see the
// original tail even when productions see EOF after a limit. Outer trivia stays
// at File level, and unparsed non-trivia remains a flat, bounded Error node.
func (p *parser) retainRemainder(root *nodeBuilder) {
	p.retainUntil(root, p.lookFrom(p.pos, delimitedExpression))
	if p.tokens[p.pos].Kind != EOF {
		p.report(UnexpectedToken, p.tokens[p.pos].Span)
		end := len(p.tokens) - 1
		for end > p.pos && isTrivia(p.tokens[end-1].Kind) {
			end--
		}
		rest := p.begin()
		p.retainUntil(&rest, end)
		root.node(p.finish(Error, rest))
	}
	p.retainUntil(root, len(p.tokens)-1)
}
