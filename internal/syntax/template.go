package syntax

import "unicode/utf8"

type mode uint8

const (
	modeQuoted mode = iota
	modeExpression
	modeHeredoc
)

// Each nested expression owns its brace depth. A slice, rather than recursive
// scanning, keeps deeply nested templates from exhausting the Go call stack.
type modeFrame struct {
	mode   mode
	start  int
	braces int
	marker Span
}

func (l *lexer) popMode() { l.modes = l.modes[:len(l.modes)-1] }

func (l *lexer) templateExpression() Kind {
	frame := &l.modes[len(l.modes)-1]
	switch l.source[l.offset] {
	case '~':
		if l.offset == frame.start+2 || (frame.braces == 0 && l.has("~}")) {
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
	return l.config()
}

func (l *lexer) quoted() Kind {
	if l.source[l.offset] == '"' {
		l.offset++
		l.popMode()
		return QuoteClose
	}
	if kind, ok := l.templateOpen(); ok {
		return kind
	}
	for l.offset < len(l.source) {
		if l.source[l.offset] == '"' || l.has("${") || l.has("%{") {
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
			start := l.offset
			if l.has("\r\n") {
				l.offset++
			}
			l.offset++
			l.error(NewlineInQuotedTemplate, start, l.offset)
			continue
		}
		l.advanceRune()
	}
	return TemplateText
}

func (l *lexer) templateOpen() (Kind, bool) {
	kind := InterpolationOpen
	if l.has("%{") {
		kind = DirectiveOpen
	} else if !l.has("${") {
		return Invalid, false
	}
	l.modes = append(l.modes, modeFrame{mode: modeExpression, start: l.offset})
	l.offset += 2
	return kind, true
}

func (l *lexer) escapedIntroducer() bool {
	if l.has("$${") || l.has("%%{") {
		l.offset += 3
		return true
	}
	return false
}

// Escapes remain part of TemplateText. Validation does not decode or normalize
// source; malformed escape boundaries leave quotes and introducers available
// to the next scan iteration.
func (l *lexer) quotedEscape() {
	start := l.offset
	l.offset++
	if l.offset == len(l.source) {
		l.error(InvalidEscape, start, l.offset)
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
				l.error(InvalidEscape, start, l.offset)
				return
			}
			digit, ok := hexDigit(l.source[l.offset])
			if !ok {
				l.error(InvalidEscape, start, l.offset)
				return
			}
			value = value*16 + digit
			l.offset++
		}
		if value > utf8.MaxRune || value >= 0xD800 && value <= 0xDFFF {
			l.error(InvalidEscape, start, l.offset)
		}
	default:
		l.error(InvalidEscape, start, l.offset)
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
