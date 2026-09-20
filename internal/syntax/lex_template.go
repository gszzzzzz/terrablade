package syntax

import "unicode/utf8"

// Template sequence openers. Both are two bytes long, which
// scanTemplateExpression relies on to find a strip marker directly after the
// opener without knowing which one started the frame.
const (
	interpolationOpener = "${"
	directiveOpener     = "%{"
)

// mode identifies the sub-language the lexer is scanning. Configuration mode
// has no frame; each template construct pushes a frame until its closer.
type mode uint8

const (
	// modeQuoted is inside "...": literal text until the closing quote.
	modeQuoted mode = iota
	// modeExpression is inside ${...} or %{...}: configuration tokens until
	// the brace that balances the opener.
	modeExpression
	// modeHeredoc is inside a heredoc: literal text until the marker line.
	modeHeredoc
)

// modeFrame records one open template construct. Each nested expression owns
// its brace depth. A slice, rather than recursive scanning, keeps deeply nested
// templates from exhausting the Go call stack.
type modeFrame struct {
	mode mode
	// start is the offset of the opener, where an unterminated construct is
	// diagnosed once the source ends.
	start int
	// braces counts unclosed '{' inside a modeExpression frame so that an
	// object constructor's brace does not end the sequence. Other modes leave
	// it zero.
	braces int
	// marker is the heredoc delimiter's span. scanHeredoc compares the cursor
	// with it to tell which header token is due. Other modes leave it empty.
	marker Span
}

func (l *lexer) popMode() { l.modes = l.modes[:len(l.modes)-1] }

// scanTemplateExpression scans one token inside ${...} or %{...}. Only the
// strip marker and the brace that closes the sequence differ from
// configuration mode; everything else is delegated to scanConfig so the parser
// sees the ordinary expression grammar.
func (l *lexer) scanTemplateExpression() TokenKind {
	frame := &l.modes[len(l.modes)-1]
	switch l.source[l.offset] {
	case '~':
		// A '~' is a strip marker only at the sequence's edges: directly after
		// the opener, or directly before the closing brace at depth zero. Any
		// other '~' falls through to scanConfig, which reports InvalidCharacter.
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

	// Newlines inside an interpolation are ordinary Newline tokens. The parser
	// uses newlineTransparent rules here, just like inside parentheses or
	// brackets.
	return l.scanConfig()
}

// scanQuoted scans one token inside a quoted template. A literal run stops at
// the closing quote and at template openers, which the next call handles, so
// the text between them is one TemplateText token with its escapes left
// encoded.
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
			// A literal newline is an error, but the template still runs on to
			// its closing quote: ending the run here would leave that quote to
			// open a new template and the next line to be lexed inside it.
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

// templateOpen enters expression mode at a "${" or "%{" opener and reports
// which one it consumed. It is shared by quoted templates and heredocs.
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

// escapedIntroducer skips "$${" or "%%{", the escaped spellings of the template
// openers. They remain literal text; decoding them, "$${" to "${" and "%%{" to
// "%{", is a consumer's job.
func (l *lexer) escapedIntroducer() bool {
	if l.hasPrefix("$${") || l.hasPrefix("%%{") {
		l.offset += 3
		return true
	}
	return false
}

// quotedEscape validates one backslash escape and leaves it encoded inside the
// TemplateText token: the tree must reproduce the original spelling, so
// decoding and normalizing belong to consumers. A malformed escape ends where
// validation stopped, which leaves a following quote or template opener for
// the next scan iteration instead of swallowing it into the bad escape.
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
		// Surrogate code points and values past the Unicode range have no
		// UTF-8 encoding, so no consumer could decode them.
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
