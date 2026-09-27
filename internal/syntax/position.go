package syntax

import (
	"slices"
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

// Locate fills in Line and Column for every position from its Offset, which
// must be in [0, len(r.Source())]; EOF is valid and other offsets panic. It
// reorders positions by offset and scans the source once, so d positions take
// O(n + d log d) time rather than one scan each.
//
// Each LF starts a new line immediately after that byte, so CRLF counts as one
// line ending and one cluster. An offset inside a cluster, including the LF of
// a CRLF, has that cluster's starting column. Each malformed UTF-8 byte forms
// its own cluster, so columns never decrease as offsets advance within a line.
func (r Result) Locate(positions []*Position) {
	for _, position := range positions {
		if position.Offset < 0 || position.Offset > len(r.source) {
			panic("syntax: position offset out of range")
		}
	}
	slices.SortFunc(positions, func(a, b *Position) int { return a.Offset - b.Offset })

	// accept consumes the cluster ending at end: every position inside it
	// takes the cluster's starting line and column, then the scan advances.
	line, column, next := 1, 1, 0
	accept := func(end int, newline bool) {
		for next < len(positions) && positions[next].Offset < end {
			positions[next].Line, positions[next].Column = line, column
			next++
		}
		if newline {
			line, column = line+1, 1
		} else {
			column++
		}
	}

	source := r.source
	for start := 0; start < len(source); {
		// Grapheme iteration does not validate UTF-8. Separate valid runs so
		// even overlong or surrogate encodings cannot join a neighboring cluster.
		end := start
		for end < len(source) {
			runeValue, width := utf8.DecodeRuneInString(source[end:])
			if isEncodingError(runeValue, width) {
				break
			}
			end += width
		}
		if end == start {
			accept(start+1, false)
			start++
			continue
		}

		clusters := graphemes.FromString(source[start:end])
		for clusters.Next() {
			accept(start+clusters.End(), strings.HasSuffix(clusters.Value(), "\n"))
		}
		start = end
	}

	// The rest are at EOF, one past the final cluster.
	for _, position := range positions[next:] {
		position.Line, position.Column = line, column
	}
}
