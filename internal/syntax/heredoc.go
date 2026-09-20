package syntax

import (
	"bytes"
	"unicode/utf8"
)

// heredocIntroducer opens a heredoc. An optional '-' after it selects the
// indented form, which affects only later indentation removal, not lexing.
const heredocIntroducer = "<<"

// heredocOpener reports the marker span of a complete <<[-]Identifier Newline
// prefix at the cursor. Only that complete prefix starts heredoc mode;
// incomplete prefixes remain ordinary config tokens for the parser to reject.
func (l *lexer) heredocOpener() (Span, bool) {
	start := l.offset + len(heredocIntroducer)
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

// heredoc scans one token inside a heredoc. The frame keeps no explicit state:
// heredocOpener guaranteed that the marker and its newline follow the opener
// contiguously, so the cursor's position relative to the marker span tells
// which header token is due. The marker and its newline are separate tokens
// so that the line ending stays an ordinary Newline, which the parser consumes
// through the same trivia lookahead as every other line ending.
func (l *lexer) heredoc() TokenKind {
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

	// Literal text runs until a template opener or a closing marker line. The
	// closer check is skipped at the run's first position only because it was
	// already made above and would fail again.
	start := l.offset
	for l.offset < len(l.source) {
		if l.has(interpolationOpener) || l.has(directiveOpener) {
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

// heredocEnd reports the end offset of a closing marker line starting at the
// cursor. Terraform-family implementations recognize a whitespace-trimmed
// marker line for both << and <<-. The dash affects later indentation removal,
// not closing marker recognition. Keep those surrounding bytes, and require a
// final newline just as native HCL does. No normalization is applied to marker
// identifiers.
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
