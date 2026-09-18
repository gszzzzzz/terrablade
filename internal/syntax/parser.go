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

type parser struct {
	source      string
	tokens      []token
	pos         int
	diagnostics []Diagnostic
	depth       int
	limited     bool
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
	i := p.pos
	for i < len(p.tokens)-1 {
		switch p.tokens[i].Kind {
		case Whitespace, BlockComment:
			i++
		case LineComment, Newline:
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

type nodeBuilder struct {
	start    int
	children []SyntaxElement
	height   int
}

func (p *parser) begin() nodeBuilder {
	return nodeBuilder{start: p.tokens[p.pos].Span.Start, height: 1}
}

func (b *nodeBuilder) node(node SyntaxNode) {
	b.children = append(b.children, node)
	b.height = max(b.height, node.height+1)
}

func (p *parser) before(b *nodeBuilder, index int) {
	for p.pos < index {
		token := p.tokens[p.pos]
		b.children = append(b.children, SyntaxToken{kind: token.Kind, span: token.Span})
		p.pos++
	}
}

// take consumes through the looked-ahead grammatical token, but never EOF.
// The file assembler alone owns EOF, preventing duplicate sentinel leaves.
func (p *parser) take(b *nodeBuilder, context expressionContext) {
	i := p.look(context)
	if p.tokens[i].Kind != EOF {
		i++
	}
	p.before(b, i)
}

func (p *parser) report(kind DiagnosticKind, span Span) {
	p.diagnostics = append(p.diagnostics, Diagnostic{Kind: kind, Span: span})
}

func (p *parser) limit(span Span) {
	if !p.limited {
		p.report(NestingLimitExceeded, span)
		p.limited = true
	}
}

func (p *parser) finish(kind NodeKind, b nodeBuilder) SyntaxNode {
	span := Span{Start: b.start, End: b.start}
	if len(b.children) > 0 {
		span.End = b.children[len(b.children)-1].Span().End
	}
	if b.height > maxExpressionDepth {
		p.limit(span)
		// Flatten only the overflowing structure into a lossless Error node.
		// This also bounds the tree seen by future recursive consumers.
		var leaves []SyntaxElement
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
