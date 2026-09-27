package syntax

import (
	"bytes"
	"unicode/utf8"
)

// lex splits source into tokens that cover it exactly, followed by EOF.
func lex(source []byte) lexResult {
	l := lexer{source: source}
	for l.offset < len(source) {
		start := l.offset
		kind := l.scan()
		l.result.tokens = append(l.result.tokens, Token{kind: kind, span: Span{start, l.offset}})
	}

	// Report each unclosed template construct at its opener.
	for _, frame := range l.modes {
		kind, width := UnterminatedQuotedTemplate, 1
		switch frame.mode {
		case modeExpression:
			kind, width = UnterminatedTemplateSequence, len(interpolationOpener)
		case modeHeredoc:
			kind, width = UnterminatedHeredoc, frame.marker.Start-frame.start
		}
		l.report(kind, frame.start, frame.start+width)
	}

	l.result.tokens = append(l.result.tokens, Token{kind: EOF, span: Span{len(source), len(source)}})
	// An unterminated block comment is reported after the encoding errors
	// inside it.
	sortDiagnostics(l.result.diagnostics)
	return l.result
}

// lexer holds the scanning state. Each scan method consumes at least one byte.
type lexer struct {
	source []byte
	offset int
	result lexResult
	// modes holds the open template constructs, innermost last. An empty
	// stack means configuration mode.
	modes []modeFrame
}

func (l *lexer) scan() TokenKind {
	if len(l.modes) > 0 {
		switch l.modes[len(l.modes)-1].mode {
		case modeQuoted:
			return l.scanQuoted()
		case modeExpression:
			return l.scanTemplateExpression()
		case modeHeredoc:
			return l.scanHeredoc()
		}
	}
	return l.scanConfig()
}

const byteOrderMark rune = 0xFEFF

// scanConfig scans one token of the configuration language, which is also
// the language inside template sequences.
func (l *lexer) scanConfig() TokenKind {
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
	case l.hasPrefix("\r\n"):
		l.offset += 2
		return Newline
	case c == '#' || l.hasPrefix("//"):
		// The line ending is a separate Newline token.
		for l.offset < len(l.source) && l.source[l.offset] != '\n' && !l.hasPrefix("\r\n") {
			l.advanceRune()
		}
		return LineComment
	case l.hasPrefix("/*"):
		return l.blockComment()
	case c == '"':
		l.modes = append(l.modes, modeFrame{mode: modeQuoted, start: l.offset})
		l.offset++
		return QuoteOpen
	case l.hasPrefix(heredocIntroducer):
		// Without a complete heredoc header, this is a Less.
		if marker, ok := l.heredocOpener(); ok {
			l.modes = append(l.modes, modeFrame{mode: modeHeredoc, start: l.offset, marker: marker})
			l.offset = marker.Start
			return HeredocOpen
		}
		l.offset++
		return Less
	case digit(c):
		l.number()
		return Number
	}

	r, width := utf8.DecodeRune(l.source[l.offset:])
	if r == byteOrderMark {
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
	if kind, size := l.punctuation(); size > 0 {
		l.offset += size
		return kind
	}

	start := l.offset
	l.advanceRune()
	// advanceRune reports malformed encoding itself.
	if !isEncodingError(r, width) {
		l.report(InvalidCharacter, start, l.offset)
	}
	return Invalid
}

// blockComment scans from "/*" through "*/", or to EOF if unterminated.
func (l *lexer) blockComment() TokenKind {
	start := l.offset
	l.offset += 2
	for l.offset < len(l.source) {
		if l.hasPrefix("*/") {
			l.offset += 2
			return BlockComment
		}
		l.advanceRune()
	}
	l.report(UnterminatedBlockComment, start, l.offset)
	return BlockComment
}

// number scans a numeric candidate with upstream HCL's boundaries, which
// admit malformed candidates such as 1.0.2 for the parser to reject. Dots and
// exponents may repeat, but the token cannot end with a dot and an exponent
// needs a digit. See github.com/hashicorp/hcl/blob/v2.25.0/hclsyntax/scan_tokens.rl.
func (l *lexer) number() {
	l.offset++
	// i probes past dots; offset commits only through the last digit.
	for i := l.offset; i < len(l.source); {
		switch c := l.source[i]; {
		case digit(c):
			i++
			l.offset = i
		case c == '.':
			i++
		case c == 'e' || c == 'E':
			next := i + 1
			if next < len(l.source) && (l.source[next] == '+' || l.source[next] == '-') {
				next++
			}
			if next >= len(l.source) || !digit(l.source[next]) {
				return
			}
			i = next + 1
			l.offset = i
		default:
			return
		}
	}
}

// ContinuesNumber reports whether step, written directly after a number and a
// dot, would be scanned into that number, as .0 and .e2 are but .id and .e
// are not. Only a prefix matters: .e2suffix still extends the number.
func ContinuesNumber(step string) bool {
	l := lexer{source: []byte("0." + step)}
	l.number()
	return l.offset > len("0")
}

func digit(c byte) bool { return c >= '0' && c <= '9' }

// singlePunctuation maps a byte to its one-byte operator or delimiter, or
// Invalid.
var singlePunctuation = [256]TokenKind{
	'{': OpenBrace,
	'}': CloseBrace,
	'[': OpenBracket,
	']': CloseBracket,
	'(': OpenParen,
	')': CloseParen,
	'+': Plus,
	'-': Minus,
	'*': Star,
	'/': Slash,
	'%': Percent,
	'!': Bang,
	'=': Equal,
	'<': Less,
	'>': Greater,
	':': Colon,
	'?': Question,
	'.': Dot,
	',': Comma,
}

// punctuation returns the longest operator or delimiter at offset and its
// width, or zero width if there is none. It does not consume it.
func (l *lexer) punctuation() (TokenKind, int) {
	// Longer operators are tried first, so "==" is not two Equal tokens.
	if l.hasPrefix("...") {
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

	if kind := singlePunctuation[l.source[l.offset]]; kind != Invalid {
		return kind, 1
	}
	return Invalid, 0
}

func (l *lexer) hasPrefix(text string) bool {
	return bytes.HasPrefix(l.source[l.offset:], []byte(text))
}

// advanceRune consumes one rune, reporting a malformed byte as InvalidUTF8.
func (l *lexer) advanceRune() {
	start := l.offset
	r, width := utf8.DecodeRune(l.source[start:])
	l.offset += width
	if isEncodingError(r, width) {
		l.report(InvalidUTF8, start, l.offset)
	}
}

// isEncodingError reports whether a utf8 decode result is a malformed byte
// rather than an encoded U+FFFD.
func isEncodingError(r rune, width int) bool { return r == utf8.RuneError && width == 1 }

func (l *lexer) report(kind DiagnosticKind, start, end int) {
	l.result.diagnostics = append(l.result.diagnostics, Diagnostic{Kind: kind, Span: Span{start, end}})
}
