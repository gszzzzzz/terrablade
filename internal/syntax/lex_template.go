package syntax

import "unicode/utf8"

// Template sequence openers. scanTemplateExpression relies on their having
// the same length.
const (
	interpolationOpener = "${"
	directiveOpener     = "%{"
)

// mode identifies the template construct the lexer is inside.
type mode uint8

const (
	modeQuoted     mode = iota // "..."
	modeExpression             // ${...} or %{...}
	modeHeredoc
)

// modeFrame records one open template construct.
type modeFrame struct {
	mode   mode
	start  int  // offset of the opener
	braces int  // unclosed '{' in a modeExpression frame
	marker Span // heredoc delimiter
}

func (l *lexer) popMode() { l.modes = l.modes[:len(l.modes)-1] }

// scanTemplateExpression scans one token inside ${...} or %{...}. Only strip
// markers and the closing brace differ from configuration mode.
func (l *lexer) scanTemplateExpression() TokenKind {
	frame := &l.modes[len(l.modes)-1]
	switch l.source[l.offset] {
	case '~':
		// A '~' is a strip marker only directly after the opener or before
		// the closing brace; elsewhere it is an invalid character.
		if l.offset == frame.start+len(interpolationOpener) || (frame.braces == 0 && l.hasPrefix("~}")) {
			l.offset++
			return StripMarker
		}
	case '}':
		if frame.braces == 0 {
			l.offset++
			l.popMode()
			return TemplateSequenceEnd
		}
		frame.braces--
	case '{':
		frame.braces++
	}

	return l.scanConfig()
}

// scanQuoted scans one token inside a quoted template. Literal text, escapes
// included, is one TemplateText token up to the closing quote or an opener.
func (l *lexer) scanQuoted() TokenKind {
	if l.source[l.offset] == '"' {
		l.offset++
		l.popMode()
		return QuoteClose
	}
	if kind, ok := l.templateOpen(); ok {
		return kind
	}

	for l.offset < len(l.source) {
		if l.source[l.offset] == '"' || l.hasPrefix(interpolationOpener) || l.hasPrefix(directiveOpener) {
			break
		}
		if l.escapedIntroducer() {
			continue
		}
		if l.source[l.offset] == '\\' {
			l.quotedEscape()
			continue
		}
		if l.source[l.offset] == '\n' || l.source[l.offset] == '\r' {
			// A literal newline is an error, but the text runs on to the
			// closing quote, which would otherwise open a new template.
			start := l.offset
			if l.hasPrefix("\r\n") {
				l.offset++
			}
			l.offset++
			l.report(NewlineInQuotedTemplate, start, l.offset)
			continue
		}
		l.advanceRune()
	}
	return TemplateText
}

// templateOpen consumes a "${" or "%{" opener and enters expression mode.
func (l *lexer) templateOpen() (TokenKind, bool) {
	kind := InterpolationOpen
	if l.hasPrefix(directiveOpener) {
		kind = DirectiveOpen
	} else if !l.hasPrefix(interpolationOpener) {
		return Invalid, false
	}
	l.modes = append(l.modes, modeFrame{mode: modeExpression, start: l.offset})
	l.offset += len(interpolationOpener)
	return kind, true
}

// escapedIntroducer consumes an escaped opener, "$${" or "%%{", as literal text.
func (l *lexer) escapedIntroducer() bool {
	if l.hasPrefix("$${") || l.hasPrefix("%%{") {
		l.offset += 3
		return true
	}
	return false
}

// quotedEscape consumes and validates one backslash escape. A malformed escape
// ends where validation failed, so a following quote is not swallowed.
func (l *lexer) quotedEscape() {
	start := l.offset
	l.offset++
	if l.offset == len(l.source) {
		l.report(InvalidEscape, start, l.offset)
		return
	}

	c := l.source[l.offset]
	switch c {
	case 'n', 'r', 't', '"', '\\':
		l.offset++
		return
	case 'u', 'U':
		l.offset++
		digits := 4
		if c == 'U' {
			digits = 8
		}
		var value uint32
		for range digits {
			if l.offset == len(l.source) {
				l.report(InvalidEscape, start, l.offset)
				return
			}
			digit, ok := hexDigit(l.source[l.offset])
			if !ok {
				l.report(InvalidEscape, start, l.offset)
				return
			}
			value = value*16 + digit
			l.offset++
		}
		// Surrogates and values past the Unicode range have no encoding.
		if value > utf8.MaxRune || (value >= 0xD800 && value <= 0xDFFF) {
			l.report(InvalidEscape, start, l.offset)
		}
	default:
		l.report(InvalidEscape, start, l.offset)
	}
}

func hexDigit(c byte) (uint32, bool) {
	switch {
	case c >= '0' && c <= '9':
		return uint32(c - '0'), true
	case c >= 'a' && c <= 'f':
		return uint32(c - 'a' + 10), true
	case c >= 'A' && c <= 'F':
		return uint32(c - 'A' + 10), true
	}
	return 0, false
}
