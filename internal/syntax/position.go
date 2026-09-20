package syntax

import (
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// Position identifies a byte offset in a Result's source. It contains no source
// identity; callers must keep track of the Result to which it belongs. Position's
// zero value is not a source position; the start of a source is (0, 1, 1).
type Position struct {
	Offset int // Zero-based byte offset from the start of the source.
	Line   int // One-based line number.
	Column int // One-based Unicode 17 grapheme-cluster column, not a display width.
}

// Position converts a byte offset to its line and grapheme-cluster column.
// It accepts 0 <= offset <= len(r.Source()), including EOF. Offsets outside that
// range panic. A zero Result accepts only offset zero and returns (0, 1, 1).
//
// Each LF starts a new line immediately after that byte, so CRLF counts as one
// line ending and one cluster. Offsets on CR and LF share the preceding line's
// column; a lone CR, tab, or leading BOM counts as one cluster. Columns use
// Unicode 17 extended grapheme clusters, not terminal display widths. Upstream
// HCL can select other Unicode tables by toolchain, so rare boundaries may differ.
//
// An offset inside a cluster has the same column as that cluster's start; its
// end is at the next column. Offset itself is never adjusted. Each malformed
// UTF-8 byte, including bytes of a truncated sequence, forms its own cluster.
// Thus columns never decrease as offsets advance within a line. EOF after a
// final LF is at column one of the next line.
//
// Position allocates no memory. Each call takes O(offset) time; the lookup
// strategy is an implementation detail.
func (r Result) Position(offset int) Position {
	// Slicing validates both bounds, including negative offsets, like Text.
	prefix := r.source[:offset]
	lineStart := strings.LastIndexByte(prefix, '\n') + 1
	position := Position{
		Offset: offset,
		Line:   strings.Count(prefix, "\n") + 1,
		Column: 1,
	}

	// A UAX #29 boundary depends on preceding state and the next code point.
	// Include the byte at offset and at most three following bytes, completing
	// that code point without scanning an arbitrarily long cluster beyond it.
	limit := offset + min(utf8.UTFMax, len(r.source)-offset)
	data := r.source[lineStart:limit]
	target := offset - lineStart

	for consumed := 0; consumed < target; {
		// Grapheme iteration does not validate UTF-8. Separate valid runs so
		// even overlong or surrogate encodings cannot join a preceding cluster.
		validEnd := consumed
		for validEnd < len(data) {
			runeValue, width := utf8.DecodeRuneInString(data[validEnd:])
			if isEncodingError(runeValue, width) {
				break
			}
			validEnd += width
		}
		if validEnd == consumed {
			// A malformed byte is a cluster of its own, one column wide.
			consumed++
			position.Column++
			continue
		}

		start := consumed
		clusters := graphemes.FromString(data[start:validEnd])
		for consumed < target && clusters.Next() {
			end := start + clusters.End()
			if end > target {
				// The offset is inside this cluster and shares its column.
				return position
			}
			consumed = end
			position.Column++
		}
	}
	return position
}
