package lexer

import (
	"bytes"
	"unicode/utf8"
)

// A heredoc mode starts only for the complete <<[-]Identifier Newline prefix.
// Incomplete prefixes remain ordinary config tokens for the parser to reject.
func (l *lexer) heredocOpener() (Span, bool) {
	start := l.offset + 2
	if start < len(l.source) && l.source[start] == '-' {
		start++
	}
	r, width := utf8.DecodeRune(l.source[start:])
	if !identifierStart(r) {
		return Span{}, false
	}
	end := start + width
	for end < len(l.source) {
		r, width = utf8.DecodeRune(l.source[end:])
		if !identifierContinue(r) {
			break
		}
		end += width
	}
	if end < len(l.source) && (l.source[end] == '\n' || bytes.HasPrefix(l.source[end:], []byte("\r\n"))) {
		return Span{start, end}, true
	}
	return Span{}, false
}

func (l *lexer) heredoc() Kind {
	frame := l.modes[len(l.modes)-1]
	if l.offset == frame.marker.Start {
		l.offset = frame.marker.End
		return HeredocMarker
	}
	if l.offset == frame.marker.End {
		if l.source[l.offset] == '\r' {
			l.offset++
		}
		l.offset++
		return Newline
	}
	if end, ok := l.heredocEnd(frame.marker); ok {
		l.offset = end
		l.popMode()
		return HeredocEndMarker
	}
	if kind, ok := l.templateOpen(); ok {
		return kind
	}
	start := l.offset
	for l.offset < len(l.source) {
		if l.has("${") || l.has("%{") {
			break
		}
		if l.offset != start {
			if _, ok := l.heredocEnd(frame.marker); ok {
				break
			}
		}
		if l.escapedIntroducer() {
			continue
		}
		l.advanceRune()
	}
	return TemplateText
}

// Terraform-family implementations recognize a whitespace-trimmed marker line
// for both << and <<-. The dash affects later indentation removal, not closing
// marker recognition. Keep those surrounding bytes, and require a final newline
// just as native HCL does. No normalization is applied to marker identifiers.
func (l *lexer) heredocEnd(marker Span) (int, bool) {
	if l.offset == 0 || l.source[l.offset-1] != '\n' {
		return 0, false
	}
	lineLength := bytes.IndexByte(l.source[l.offset:], '\n')
	if lineLength < 0 {
		return 0, false
	}
	end := l.offset + lineLength
	if end > l.offset && l.source[end-1] == '\r' {
		end--
	}
	if bytes.Equal(bytes.TrimSpace(l.source[l.offset:end]), l.source[marker.Start:marker.End]) {
		return end, true
	}
	return 0, false
}
