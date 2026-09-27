package syntax

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// Position is a location in a Result's source. The start of a source is
// {0, 1, 1}.
type Position struct {
	Offset int // byte offset, from 0
	Line   int // line number, from 1
	Column int // grapheme-cluster column, from 1; see Result.Locate
}

// Locate sets Line and Column of each position from its Offset, which must be
// in [0, len(r.Source())]; other offsets panic. It sorts positions by offset
// and scans the source once.
//
// Only LF starts a new line. Columns count Unicode 17 extended grapheme
// clusters, so CRLF is one cluster; an offset inside a cluster has that
// cluster's column. Each malformed UTF-8 byte is a cluster of its own.
func (r Result) Locate(positions []*Position) {
	for _, position := range positions {
		if position.Offset < 0 || position.Offset > len(r.source) {
			panic("syntax: position offset out of range")
		}
	}
	slices.SortFunc(positions, func(a, b *Position) int { return a.Offset - b.Offset })

	// accept locates the positions in the cluster ending at end.
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
		// graphemes does not validate UTF-8, so segment only valid runs.
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

	// The rest are at EOF.
	for _, position := range positions[next:] {
		position.Line, position.Column = line, column
	}
}
