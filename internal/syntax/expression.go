package syntax

import (
	"math/big"
	"strings"
)

// parseExpressionSource is an internal test seam for an attribute-style value:
// unparenthesized newlines terminate it. It is not a configuration-file parser.
// Leading and trailing trivia belong to File. Remaining non-trivia is an error,
// including valid HCL constructs not implemented at this checkpoint.
func parseExpressionSource(source []byte) syntaxFile {
	p := newParser(source)
	root := p.begin()
	p.before(&root, p.look(delimitedExpression))
	p.operand(&root, 0, lineExpression)
	p.before(&root, p.look(delimitedExpression))
	if p.peek(delimitedExpression) != EOF {
		if !p.limited {
			p.report(UnexpectedToken, p.tokens[p.pos].Span)
		}
		end := len(p.tokens) - 1
		for end > p.pos && trivia(p.tokens[end-1].Kind) {
			end--
		}
		rest := p.begin()
		p.before(&rest, end)
		root.node(p.finish(Error, rest))
	}
	p.before(&root, len(p.tokens)-1)
	return p.file(root)
}

func trivia(kind Kind) bool {
	switch kind {
	case Whitespace, BlockComment, LineComment, Newline:
		return true
	}
	return false
}

// operand commits only trivia that precedes an actual expression. A missing
// operand gets an empty Error node at the cursor; no synthetic token is emitted
// and trailing trivia remains available to the enclosing structure.
func (p *parser) operand(b *nodeBuilder, minimum int, context expressionContext) {
	i := p.look(context)
	switch p.tokens[i].Kind {
	case EOF, CloseParen, CloseBracket, CloseBrace, Comma, Colon, Newline, LineComment:
		if !p.limited {
			p.report(ExpectedExpression, p.tokens[i].Span)
		}
		b.node(p.finish(Error, p.begin()))
		return
	}
	p.before(b, i)
	b.node(p.expression(minimum, context))
}

// expression uses Pratt binding powers: conditional 0 (right associative),
// binary 1..6 (left associative), unary 7, then postfix traversal.
func (p *parser) expression(minimum int, context expressionContext) SyntaxNode {
	if p.depth == maxExpressionDepth {
		p.limit(p.tokens[p.pos].Span)
		b := p.begin()
		p.take(&b, context)
		return p.finish(Error, b)
	}
	p.depth++
	defer func() { p.depth-- }()
	left := p.prefix(context)
	for !p.limited {
		kind := p.peek(context)
		if kind == Question && minimum == 0 {
			b := nodeBuilder{start: left.span.Start, height: 1}
			b.node(left)
			p.take(&b, context)
			p.operand(&b, 0, context)
			if p.expect(&b, Colon, ExpectedConditionalColon, context) {
				p.operand(&b, 0, context)
			}
			left = p.finish(ConditionalExpression, b)
			continue
		}
		power := binaryPower(kind)
		if power == 0 || power < minimum {
			break
		}
		b := nodeBuilder{start: left.span.Start, height: 1}
		b.node(left)
		p.take(&b, context)
		p.operand(&b, power+1, context)
		left = p.finish(BinaryExpression, b)
	}
	return left
}

func binaryPower(kind Kind) int {
	switch kind {
	case Or:
		return 1
	case And:
		return 2
	case EqualEqual, NotEqual:
		return 3
	case Less, LessEqual, Greater, GreaterEqual:
		return 4
	case Plus, Minus:
		return 5
	case Star, Slash, Percent:
		return 6
	}
	return 0
}

func (p *parser) prefix(context expressionContext) SyntaxNode {
	b := p.begin()
	token := p.tokens[p.pos]
	kind := Error
	switch token.Kind {
	case Number:
		p.number(&b, context, false)
		kind = LiteralExpression
	case Identifier:
		p.take(&b, context)
		if p.peek(context) == OpenParen || p.peek(context) == DoubleColon {
			p.call(&b, context)
			kind = FunctionCallExpression
		} else {
			switch p.source[token.Span.Start:token.Span.End] {
			case "true", "false", "null":
				kind = LiteralExpression
			default:
				kind = VariableExpression
			}
		}
	case OpenParen:
		p.take(&b, context)
		p.operand(&b, 0, delimitedExpression)
		p.expect(&b, CloseParen, ExpectedClosingParen, delimitedExpression)
		kind = ParenthesizedExpression
	case Minus, Bang:
		p.take(&b, context)
		p.operand(&b, 7, context)
		kind = UnaryExpression
	case OpenBracket, OpenBrace, QuoteOpen, HeredocOpen:
		p.report(UnsupportedExpression, token.Span)
		p.unsupported(&b)
	default:
		p.report(ExpectedExpression, token.Span)
		p.take(&b, context)
	}
	left := p.finish(kind, b)
	if !p.limited && (p.peek(context) == Dot || p.peek(context) == OpenBracket) {
		b = nodeBuilder{start: left.span.Start, height: 1}
		b.node(left)
		p.steps(&b, context, false)
		left = p.finish(TraversalExpression, b)
	}
	return left
}

// number validates the lexer's numeric candidate without evaluating the
// expression. Legacy dot-index syntax additionally rejects any decimal point.
func (p *parser) number(b *nodeBuilder, context expressionContext, legacy bool) {
	span := p.tokens[p.look(context)].Span
	text := p.source[span.Start:span.End]
	if legacy && strings.Contains(text, ".") {
		p.report(InvalidLegacyIndex, span)
	} else if _, _, err := big.ParseFloat(text, 10, 512, big.ToNearestEven); err != nil {
		// This is the same representability check as upstream cty.ParseNumberVal,
		// using the standard library and retaining no evaluated value in the CST.
		p.report(InvalidNumber, span)
	}
	p.take(b, context)
}

func (p *parser) expect(b *nodeBuilder, kind Kind, diagnostic DiagnosticKind, context expressionContext) bool {
	if p.limited {
		return false
	}
	if p.peek(context) == kind {
		p.take(b, context)
		return true
	}
	p.report(diagnostic, p.tokens[p.look(context)].Span)
	return false
}

func (p *parser) call(b *nodeBuilder, context expressionContext) {
	for p.peek(context) == DoubleColon {
		p.take(b, context)
		if !p.expect(b, Identifier, ExpectedFunctionName, context) {
			return
		}
	}
	if !p.expect(b, OpenParen, ExpectedOpeningParen, context) {
		return
	}
	for !p.limited {
		if p.peek(delimitedExpression) == CloseParen {
			p.take(b, delimitedExpression)
			return
		}
		p.operand(b, 0, delimitedExpression)
		switch p.peek(delimitedExpression) {
		case CloseParen:
			p.take(b, delimitedExpression)
			return
		case Ellipsis:
			p.take(b, delimitedExpression)
			p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
			return
		case Comma:
			p.take(b, delimitedExpression)
		case EOF, CloseBracket, CloseBrace:
			p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
			return
		default:
			if !p.limited {
				p.report(ExpectedArgumentSeparator, p.tokens[p.look(delimitedExpression)].Span)
			}
			p.recoverArgument(b)
			if p.peek(delimitedExpression) != Comma {
				p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
				return
			}
			p.take(b, delimitedExpression)
		}
	}
}

// recoverArgument preserves malformed material until a separator or closer.
// The next argument may still be parsed. Every non-boundary iteration consumes
// at least one token; missing expressions can return without consuming only when
// their caller will consume the delimiter or leave this production.
func (p *parser) recoverArgument(parent *nodeBuilder) {
	p.before(parent, p.look(delimitedExpression))
	b := p.begin()
	for {
		switch p.peek(delimitedExpression) {
		case EOF, Comma, CloseParen, CloseBracket, CloseBrace:
			if len(b.children) > 0 {
				parent.node(p.finish(Error, b))
			}
			return
		}
		p.take(&b, delimitedExpression)
	}
}

// unsupported consumes one balanced unsupported construct without recursion.
// Nested template/interpolation delimiters remain visible in the lexical stream.
func (p *parser) unsupported(b *nodeBuilder) {
	var ends []Kind
	for p.tokens[p.pos].Kind != EOF {
		kind := p.tokens[p.pos].Kind
		if close := closing(kind); close != Invalid {
			ends = append(ends, close)
		} else if len(ends) > 0 && ends[len(ends)-1] == kind {
			ends = ends[:len(ends)-1]
		}
		p.before(b, p.pos+1)
		if len(ends) == 0 {
			return
		}
	}
}

func closing(kind Kind) Kind {
	switch kind {
	case OpenParen:
		return CloseParen
	case OpenBracket:
		return CloseBracket
	case OpenBrace:
		return CloseBrace
	case QuoteOpen:
		return QuoteClose
	case HeredocOpen:
		return HeredocEndMarker
	case InterpolationOpen, DirectiveOpen:
		return TemplateSequenceEnd
	}
	return Invalid
}
