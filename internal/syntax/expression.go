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
	p.consumeUntil(&root, p.look(delimitedExpression))
	p.operand(&root, 0, lineExpression)
	return p.file(root)
}

// operand commits only trivia that precedes an actual expression. A missing
// operand gets an empty Error node at the cursor; no synthetic token is emitted
// and trailing trivia remains available to the enclosing structure.
func (p *parser) operand(b *nodeBuilder, minimum int, context expressionContext) {
	i := p.look(context)
	switch p.tokens[i].kind {
	case EOF, CloseParen, CloseBracket, CloseBrace, Comma, Colon, Newline, LineComment:
		p.report(ExpectedExpression, p.tokens[i].span)
		b.node(p.begin().finish(Error))
		return
	}
	p.consumeUntil(b, i)
	b.node(p.expression(minimum, context))
}

// expression uses Pratt binding powers: conditional 0 (right associative),
// binary 1..6 (left associative), unary 7, then postfix traversal.
func (p *parser) expression(minimum int, context expressionContext) SyntaxNode {
	if p.depth == maxRecursiveExpressionDepth {
		span := p.current().span
		b := p.begin()
		// Retain the offending token in a non-empty Error before freezing the
		// cursor: this boundary makes progress, while File recovers the remainder.
		p.consumeLookahead(&b, context)
		p.haltAtLimit(span)
		return b.finish(Error)
	}
	p.depth++
	defer func() { p.depth-- }()
	left := p.prefix(context)
	for {
		kind := p.peek(context)
		// Only the weakest binding level may consume '?'. Parsing both arms at
		// zero lets the false arm absorb another conditional, associating right.
		if kind == Question && minimum == 0 {
			b := p.beginAt(left.Span().Start)
			b.node(left)
			p.consumeLookahead(&b, context)
			p.operand(&b, 0, context)
			if p.expect(&b, Colon, ExpectedConditionalColon, context) {
				p.operand(&b, 0, context)
			}
			left = b.finish(ConditionalExpression)
			continue
		}
		power := binaryPower(kind)
		// Weaker operators belong to the caller; zero also leaves delimiters and
		// EOF untouched so the enclosing production can finish or recover.
		if power == 0 || power < minimum {
			break
		}
		b := p.beginAt(left.Span().Start)
		b.node(left)
		p.consumeLookahead(&b, context)
		// A same-precedence operator cannot enter the RHS. This loop consumes it
		// next, wrapping the previous result on the left rather than the right.
		p.operand(&b, power+1, context)
		left = b.finish(BinaryExpression)
	}
	return left
}

func binaryPower(kind TokenKind) int {
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
	token := p.current()
	kind := Error
	switch token.kind {
	case Number:
		p.number(&b, context, false)
		kind = LiteralExpression
	case Identifier:
		p.consumeLookahead(&b, context)
		// Keywords are contextual: true() and null::f() are function calls.
		if p.peek(context) == OpenParen || p.peek(context) == DoubleColon {
			p.call(&b, context)
			kind = FunctionCallExpression
		} else {
			switch p.source[token.span.Start:token.span.End] {
			case "true", "false", "null":
				kind = LiteralExpression
			default:
				kind = VariableExpression
			}
		}
	case OpenParen:
		p.consumeLookahead(&b, context)
		p.operand(&b, 0, delimitedExpression)
		p.expect(&b, CloseParen, ExpectedClosingParen, delimitedExpression)
		kind = ParenthesizedExpression
	case Minus, Bang:
		p.consumeLookahead(&b, context)
		// Unary operands include postfix traversal but exclude every binary level.
		p.operand(&b, 7, context)
		kind = UnaryExpression
	case OpenBracket, OpenBrace, QuoteOpen, HeredocOpen:
		p.report(UnsupportedExpression, token.span)
		p.unsupported(&b)
	default:
		p.report(ExpectedExpression, token.span)
		p.consumeLookahead(&b, context)
	}
	left := b.finish(kind)
	if p.peek(context) == Dot || p.peek(context) == OpenBracket {
		b = p.beginAt(left.Span().Start)
		b.node(left)
		p.steps(&b, context, allTraversalSteps)
		left = b.finish(TraversalExpression)
	}
	return left
}

// number validates the lexer's numeric candidate without evaluating the
// expression. Legacy dot-index syntax additionally rejects any decimal point.
func (p *parser) number(b *nodeBuilder, context expressionContext, legacy bool) {
	span := p.tokens[p.look(context)].span
	text := p.source[span.Start:span.End]
	if legacy && strings.Contains(text, ".") {
		p.report(InvalidLegacyIndex, span)
	} else if _, _, err := big.ParseFloat(text, 10, 512, big.ToNearestEven); err != nil {
		// This is the same representability check as upstream cty.ParseNumberVal,
		// using the standard library and retaining no evaluated value in the CST.
		p.report(InvalidNumber, span)
	}
	p.consumeLookahead(b, context)
}

// A mismatch leaves the token for the enclosing production. Consuming it here
// could steal that production's closer or attach trailing trivia to this node.
func (p *parser) expect(b *nodeBuilder, kind TokenKind, diagnostic DiagnosticKind, context expressionContext) bool {
	if p.peek(context) == kind {
		p.consumeLookahead(b, context)
		return true
	}
	p.report(diagnostic, p.tokens[p.look(context)].span)
	return false
}

func (p *parser) call(b *nodeBuilder, context expressionContext) {
	for p.peek(context) == DoubleColon {
		p.consumeLookahead(b, context)
		if !p.expect(b, Identifier, ExpectedFunctionName, context) {
			return
		}
	}
	if !p.expect(b, OpenParen, ExpectedOpeningParen, context) {
		return
	}
	// Only the arguments ignore newlines; namespace/name and '(' use the outer
	// context. Testing ')' before an operand permits empty lists and trailing ','.
	for {
		if p.peek(delimitedExpression) == CloseParen {
			p.consumeLookahead(b, delimitedExpression)
			return
		}
		p.operand(b, 0, delimitedExpression)
		switch p.peek(delimitedExpression) {
		case CloseParen:
			p.consumeLookahead(b, delimitedExpression)
			return
		case Ellipsis:
			// Expansion is final: a following comma must not reopen the argument loop.
			p.consumeLookahead(b, delimitedExpression)
			p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
			return
		case Comma:
			p.consumeLookahead(b, delimitedExpression)
		case EOF, CloseBracket, CloseBrace:
			p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
			return
		default:
			p.report(ExpectedArgumentSeparator, p.tokens[p.look(delimitedExpression)].span)
			p.recoverArgument(b)
			if p.peek(delimitedExpression) != Comma {
				p.expect(b, CloseParen, ExpectedClosingParen, delimitedExpression)
				return
			}
			p.consumeLookahead(b, delimitedExpression)
		}
	}
}

// recoverArgument preserves malformed material until a separator or closer.
// The next argument may still be parsed. Every non-boundary iteration consumes
// at least one token; missing expressions can return without consuming only when
// their caller will consume the delimiter or leave this production.
func (p *parser) recoverArgument(parent *nodeBuilder) {
	p.consumeUntil(parent, p.look(delimitedExpression))
	b := p.begin()
	for {
		switch p.peek(delimitedExpression) {
		case EOF, Comma, CloseParen, CloseBracket, CloseBrace:
			if len(p.pending) > b.mark {
				parent.node(b.finish(Error))
			}
			return
		}
		p.consumeLookahead(&b, delimitedExpression)
	}
}

// unsupported consumes one balanced unsupported construct without recursion.
// Nested template/interpolation delimiters remain visible in the lexical stream.
func (p *parser) unsupported(b *nodeBuilder) {
	if p.halted {
		return
	}
	// Raw tokens preserve template whitespace and delimiters in the Error subtree;
	// expression lookahead would interpret trivia in the wrong sub-language.
	// An unterminated construct still ends at its last non-trivia token, so the
	// trailing trivia of the file stays with the parent like any other node.
	var ends []TokenKind
	end := p.pos
	for i := p.pos; p.tokens[i].kind != EOF; i++ {
		kind := p.tokens[i].kind
		if close := closing(kind); close != Invalid {
			ends = append(ends, close)
		} else if len(ends) > 0 && ends[len(ends)-1] == kind {
			ends = ends[:len(ends)-1]
		}
		if !isTrivia(kind) {
			end = i + 1
		}
		if len(ends) == 0 {
			break
		}
	}
	p.consumeUntil(b, end)
}

func closing(kind TokenKind) TokenKind {
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
