package lexer

import (
	"bytes"
	"sort"
	"unicode/utf8"
)

// Lex tokenizes source without modifying it. It always makes progress, including
// on invalid input, and emits EOF even for an empty source.
//
// Quoted templates expose their literal and expression boundaries. Heredocs
// temporarily report UnsupportedTemplate and preserve the suffix as Invalid.
func Lex(source []byte) Result {
	l := lexer{source: source}
	for l.offset < len(source) {
		start := l.offset
		kind := l.scan()
		l.result.Tokens = append(l.result.Tokens, Token{Kind: kind, Span: Span{start, l.offset}})
	}
	for _, frame := range l.modes {
		kind, width := UnterminatedQuotedTemplate, 1
		if frame.mode == modeExpression {
			kind, width = UnterminatedTemplateSequence, 2
		}
		l.error(kind, frame.start, frame.start+width)
	}
	l.result.Tokens = append(l.result.Tokens, Token{Kind: EOF, Span: Span{len(source), len(source)}})
	// A whole-comment error may precede an encoding error found inside it.
	sort.SliceStable(l.result.Diagnostics, func(i, j int) bool {
		return l.result.Diagnostics[i].Span.Start < l.result.Diagnostics[j].Span.Start
	})
	return l.result
}

type lexer struct {
	source []byte
	offset int
	result Result
	modes  []modeFrame
}

func (l *lexer) scan() Kind {
	if len(l.modes) > 0 {
		switch l.modes[len(l.modes)-1].mode {
		case modeQuoted:
			return l.quoted()
		case modeExpression:
			return l.templateExpression()
		}
	}
	return l.config()
}

func (l *lexer) config() Kind {
	c := l.source[l.offset]
	switch {
	case c == ' ' || c == '\t':
		for l.offset < len(l.source) && (l.source[l.offset] == ' ' || l.source[l.offset] == '\t') {
			l.offset++
		}
		return Whitespace
	case c == '\n':
		l.offset++
		return Newline
	case l.has("\r\n"):
		l.offset += 2
		return Newline
	case c == '#' || l.has("//"):
		for l.offset < len(l.source) && l.source[l.offset] != '\n' && !l.has("\r\n") {
			l.advanceRune()
		}
		return LineComment
	case l.has("/*"):
		return l.blockComment()
	case c == '"':
		l.modes = append(l.modes, modeFrame{mode: modeQuoted, start: l.offset})
		l.offset++
		return QuoteOpen
	case l.has("<<"):
		l.error(UnsupportedTemplate, l.offset, l.offset+2)
		for l.offset < len(l.source) {
			l.advanceRune()
		}
		return Invalid
	case digit(c):
		l.number()
		return Number
	}

	r, width := utf8.DecodeRune(l.source[l.offset:])
	if r == '\uFEFF' {
		l.offset += width
		return BOM
	}
	if identifierStart(r) {
		l.offset += width
		for l.offset < len(l.source) {
			r, width = utf8.DecodeRune(l.source[l.offset:])
			if !identifierContinue(r) {
				break
			}
			l.offset += width
		}
		return Identifier
	}
	if kind, width := l.punctuation(); width > 0 {
		l.offset += width
		return kind
	}
	start := l.offset
	l.advanceRune()
	if !(r == utf8.RuneError && width == 1) {
		l.error(InvalidCharacter, start, l.offset)
	}
	return Invalid
}

func (l *lexer) blockComment() Kind {
	start := l.offset
	l.offset += 2
	for l.offset < len(l.source) {
		if l.has("*/") {
			l.offset += 2
			return BlockComment
		}
		l.advanceRune()
	}
	l.error(UnterminatedBlockComment, start, l.offset)
	return BlockComment
}

// number consumes the longest complete NumericLit. A fractional or exponent
// suffix is included only if it has a digit; malformed adjacency is left for
// the parser (for example, 1e+ becomes Number, Identifier, Plus).
func (l *lexer) number() {
	l.digits()
	if l.offset+1 < len(l.source) && l.source[l.offset] == '.' && digit(l.source[l.offset+1]) {
		l.offset++
		l.digits()
	}
	if l.offset < len(l.source) && (l.source[l.offset] == 'e' || l.source[l.offset] == 'E') {
		next := l.offset + 1
		if next < len(l.source) && (l.source[next] == '+' || l.source[next] == '-') {
			next++
		}
		if next < len(l.source) && digit(l.source[next]) {
			l.offset = next
			l.digits()
		}
	}
}

func (l *lexer) digits() {
	for l.offset < len(l.source) && digit(l.source[l.offset]) {
		l.offset++
	}
}

func digit(c byte) bool { return c >= '0' && c <= '9' }

func (l *lexer) punctuation() (Kind, int) {
	if l.has("...") {
		return Ellipsis, 3
	}
	if len(l.source)-l.offset >= 2 {
		switch string(l.source[l.offset : l.offset+2]) {
		case "&&":
			return And, 2
		case "||":
			return Or, 2
		case "==":
			return EqualEqual, 2
		case "!=":
			return NotEqual, 2
		case "<=":
			return LessEqual, 2
		case ">=":
			return GreaterEqual, 2
		case "=>":
			return Arrow, 2
		case "::":
			return DoubleColon, 2
		}
	}
	switch l.source[l.offset] {
	case '{':
		return OpenBrace, 1
	case '}':
		return CloseBrace, 1
	case '[':
		return OpenBracket, 1
	case ']':
		return CloseBracket, 1
	case '(':
		return OpenParen, 1
	case ')':
		return CloseParen, 1
	case '+':
		return Plus, 1
	case '-':
		return Minus, 1
	case '*':
		return Star, 1
	case '/':
		return Slash, 1
	case '%':
		return Percent, 1
	case '!':
		return Bang, 1
	case '=':
		return Equal, 1
	case '<':
		return Less, 1
	case '>':
		return Greater, 1
	case ':':
		return Colon, 1
	case '?':
		return Question, 1
	case '.':
		return Dot, 1
	case ',':
		return Comma, 1
	}
	return Invalid, 0
}

func (l *lexer) has(text string) bool { return bytes.HasPrefix(l.source[l.offset:], []byte(text)) }

// advanceRune validates encoding even inside comments and unsupported suffixes.
// Every malformed UTF-8 byte advances by one; valid U+FFFD is not an encoding error.
func (l *lexer) advanceRune() {
	start := l.offset
	r, width := utf8.DecodeRune(l.source[start:])
	l.offset += width
	if r == utf8.RuneError && width == 1 {
		l.error(InvalidUTF8, start, l.offset)
	}
}

func (l *lexer) error(kind DiagnosticKind, start, end int) {
	l.result.Diagnostics = append(l.result.Diagnostics, Diagnostic{Kind: kind, Span: Span{start, end}})
}
