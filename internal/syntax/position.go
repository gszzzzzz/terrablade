package syntax

import "strings"

// Position identifies a byte offset in a Result's source. It contains no source
// identity; callers must keep track of the Result to which it belongs. Position's
// zero value is not a source position; the start of a source is (0, 1, 1).
type Position struct {
	Offset int // Zero-based byte offset from the start of the source.
	Line   int // One-based line number.
	Column int // One-based byte column, not a rune count or display width.
}

// Position converts a byte offset to its line and byte column. It accepts
// 0 <= offset <= len(r.Source()), including EOF, and panics outside that range.
// A zero Result accepts only offset zero and returns (0, 1, 1).
//
// Each LF starts a new line immediately after that byte, so CRLF counts as one
// line ending. Offsets on CR or LF still belong to the preceding line; a lone CR
// is an ordinary byte. Tabs, a leading BOM, and malformed UTF-8 all count by
// bytes. Offsets inside a UTF-8 sequence are valid. EOF after a final LF is at
// column one of the next line.
//
// Position allocates no memory and scans the source prefix through offset.
// Its cost is O(offset); this lookup strategy is an implementation detail.
func (r Result) Position(offset int) Position {
	// Slicing validates both bounds, including negative offsets, like Text.
	prefix := r.source[:offset]
	return Position{
		Offset: offset,
		Line:   strings.Count(prefix, "\n") + 1,
		Column: offset - strings.LastIndexByte(prefix, '\n'),
	}
}
