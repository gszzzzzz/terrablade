package syntax

import (
	"strings"
	"unicode/utf8"
)

// Position identifies a byte offset in a Result's source. It contains no source
// identity; callers must keep track of the Result to which it belongs. Position's
// zero value is not a source position; the start of a source is (0, 1, 1).
type Position struct {
	Offset int // Zero-based byte offset from the start of the source.
	Line   int // One-based line number.
	Column int // One-based UTF-8 rune (code point) column, not a display width.
}

// Position converts a byte offset to its line and rune column. It accepts
// 0 <= offset <= len(r.Source()), including EOF, and panics outside that range.
// A zero Result accepts only offset zero and returns (0, 1, 1).
//
// Each LF starts a new line immediately after that byte, so CRLF counts as one
// line ending. Offsets on CR or LF still belong to the preceding line; a lone CR
// counts as one rune. Tabs and a leading BOM also count as one rune each.
// Columns count code points, unlike upstream HCL's grapheme-cluster columns:
// combining marks, emoji modifiers, and zero-width joiners count separately.
// Columns therefore need not match displayed character widths.
//
// An offset inside a valid multi-byte rune has the same column as that rune's
// start; its end is at the next column. Offset itself is never adjusted. Each
// malformed UTF-8 byte, including bytes of a truncated sequence, counts as one
// rune. Thus columns never decrease as offsets advance within a line. EOF after
// a final LF is at column one of the next line.
//
// Position allocates no memory and scans the source prefix through offset.
// Its cost is O(offset); this lookup strategy is an implementation detail.
func (r Result) Position(offset int) Position {
	// Slicing validates both bounds, including negative offsets, like Text.
	prefix := r.source[:offset]
	lineStart := strings.LastIndexByte(prefix, '\n') + 1
	columnEnd := offset
	if offset < len(r.source) && !utf8.RuneStart(r.source[offset]) {
		// Counting a truncated valid rune as malformed bytes could make columns
		// decrease when its final byte arrives. Inspect at most three preceding
		// bytes and fold only valid interior offsets back to their rune's start.
		for start := offset - 1; start >= lineStart && offset-start < utf8.UTFMax; start-- {
			if utf8.RuneStart(r.source[start]) {
				_, width := utf8.DecodeRuneInString(r.source[start:])
				if start+width > offset {
					columnEnd = start
				}
				break
			}
		}
	}
	return Position{
		Offset: offset,
		Line:   strings.Count(prefix, "\n") + 1,
		Column: utf8.RuneCountInString(r.source[lineStart:columnEnd]) + 1,
	}
}
