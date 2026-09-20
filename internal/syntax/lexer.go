package syntax

import (
	"bytes"
	"unicode/utf8"
)

// lex tokenizes source without modifying it. It always makes progress, including
// on invalid input, and emits EOF even for an empty source.
//
// Quoted templates and heredocs expose literal and expression boundaries while
// preserving their raw spelling; lex neither evaluates escapes nor strips text.
func lex(source []byte) lexResult {
	l := lexer{source: source}
	for l.offset < len(source) {
		start := l.offset
		kind := l.scan()
		l.result.Tokens = append(l.result.Tokens, SyntaxToken{kind: kind, span: Span{start, l.offset}})
	}

	// Diagnose every still-open template frame at its opener, without inventing
	// closing tokens or discarding the already-tokenized partial contents.
	for _, frame := range l.modes {
		kind, width := UnterminatedQuotedTemplate, 1
		switch frame.mode {
		case modeExpression:
			kind, width = UnterminatedTemplateSequence, len(interpolationOpener)
		case modeHeredoc:
			kind, width = UnterminatedHeredoc, frame.marker.Start-frame.start
		}
		l.error(kind, frame.start, frame.start+width)
	}

	l.result.Tokens = append(l.result.Tokens, SyntaxToken{kind: EOF, span: Span{len(source), len(source)}})
	// An unterminated block comment is reported after the encoding errors found
	// inside it, yet its span starts before theirs, so the order needs fixing.
	sortDiagnostics(l.result.Diagnostics)
	return l.result
}

// lexer scans one source buffer from its start. Each scanning method produces
// one token and advances offset; lex records the span it covered.
type lexer struct {
	source []byte
	// offset is the next unread byte. Every scan advances it by at least one
	// byte, which is what guarantees termination on arbitrary input.
	offset int
	// result accumulates tokens and diagnostics in the order they are found.
	result lexResult
	// modes holds the open template constructs, innermost last. An empty
	// stack means configuration mode.
	modes []modeFrame
}

// scan produces the next token in the innermost open template mode, or in
// configuration mode when no template construct is open.
func (l *lexer) scan() TokenKind {
	if len(l.modes) > 0 {
		switch l.modes[len(l.modes)-1].mode {
		case modeQuoted:
			return l.quoted()
		case modeExpression:
			return l.templateExpression()
		case modeHeredoc:
			return l.heredoc()
		}
	}
	return l.config()
}

// byteOrderMark is U+FEFF. Only a leading one is meaningful, but the lexer
// records every occurrence as a BOM token and leaves placement to the parser.
const byteOrderMark rune = 0xFEFF

// config scans one token of the configuration sub-language, which is also the
// language inside ${...} and %{...} sequences. It works in two phases: the
// byte switch handles every construct that begins with a specific ASCII byte,
// then the remainder decodes one rune, because BOM, identifiers, and invalid
// characters are defined on code points rather than bytes.
func (l *lexer) config() TokenKind {
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
		// Keep the terminator separate so the parser can honor newline-sensitive
		// values while preserving the exact comment spelling.
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
	case l.has(heredocIntroducer):
		// A complete marker commits template mode; otherwise consume only the
		// first '<' so the second remains available to the next scan.
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
	if kind, width := l.punctuation(); width > 0 {
		l.offset += width
		return kind
	}

	start := l.offset
	l.advanceRune()
	// advanceRune already reports malformed encoding; do not double-label it.
	if !isEncodingError(r, width) {
		l.error(InvalidCharacter, start, l.offset)
	}
	return Invalid
}

// blockComment scans from "/*" through "*/". An unterminated comment keeps the
// BlockComment kind through EOF so its source remains recognizable; the
// diagnostic then covers the whole comment.
func (l *lexer) blockComment() TokenKind {
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

// number follows upstream HCL's numeric-candidate boundaries, including malformed
// candidates such as 1.0.2. Dots and exponent fragments can repeat, but the token
// cannot end with a dot and each exponent fragment requires at least one digit.
// This also preserves accepted spellings such as 1.e2 and keeps the minus in
// 1.e2-foo separate. The parser validates the candidate's numeric value.
// See github.com/hashicorp/hcl/blob/v2.25.0/hclsyntax/scan_tokens.rl.
func (l *lexer) number() {
	l.offset++
	// i probes through dots, but offset commits only through the last digit.
	// Trailing dots therefore remain available for attribute/ellipsis tokens.
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

func digit(c byte) bool { return c >= '0' && c <= '9' }

// singlePunctuation maps an ASCII byte to its one-byte operator or delimiter.
// Unlisted bytes map to Invalid.
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

// punctuation matches the longest operator or delimiter at the cursor without
// consuming it, so config decides how to record it. A zero width means none.
// Every punctuation token passes through here, so the two-byte operators are
// a switch on the byte pair rather than a table scan: the sequential prefix
// comparisons of a table cost the lexer about ten percent on operator-heavy
// input. They are tested before the one-byte table so that, for example, "=="
// cannot lex as two Equal tokens.
func (l *lexer) punctuation() (TokenKind, int) {
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

	if kind := singlePunctuation[l.source[l.offset]]; kind != Invalid {
		return kind, 1
	}
	return Invalid, 0
}

func (l *lexer) has(text string) bool { return bytes.HasPrefix(l.source[l.offset:], []byte(text)) }

// advanceRune validates encoding even inside comments and template literals.
// Every malformed UTF-8 byte advances by one; valid U+FFFD is not an encoding error.
func (l *lexer) advanceRune() {
	start := l.offset
	r, width := utf8.DecodeRune(l.source[start:])
	l.offset += width
	if isEncodingError(r, width) {
		l.error(InvalidUTF8, start, l.offset)
	}
}

// isEncodingError reports whether a utf8 decode result denotes a malformed
// byte rather than a genuine U+FFFD: the decoders return RuneError with width
// one only for invalid input, while an encoded U+FFFD has width three.
func isEncodingError(r rune, width int) bool { return r == utf8.RuneError && width == 1 }

func (l *lexer) error(kind DiagnosticKind, start, end int) {
	l.result.Diagnostics = append(l.result.Diagnostics, Diagnostic{Kind: kind, Span: Span{start, end}})
}
