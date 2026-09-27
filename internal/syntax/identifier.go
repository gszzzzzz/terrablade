package syntax

import (
	"sort"
	"unicode/utf8"
)

// HCL identifiers require the Unicode derived properties ID_Start/ID_Continue,
// which unicode.IsLetter/IsDigit do not reproduce (for example, ID_Start admits
// U+2118 and ID_Continue admits combining marks). Go's Unicode data also varies
// with the toolchain, so Terrablade checks in version-pinned Unicode ranges in
// unicode_tables.go for both exact properties and stable token boundaries.
// Normal builds need no downloads or generation. To update the tables, run
// go generate ./internal/syntax from the repository root; the generator verifies
// the pinned SHA-256 of official DerivedCoreProperties.txt before emitting them.
// The HCL additions '_' and '-' stay here.
//
//go:generate go run ../../tools/genunicode

type runeRange struct{ lo, hi rune }

// inRanges reports whether r falls in one of the sorted, non-overlapping
// ranges, by binary search on the range ends.
func inRanges(r rune, ranges []runeRange) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].hi >= r })
	return i < len(ranges) && ranges[i].lo <= r
}

// identifierStart reports whether r may begin an identifier: ID_Start or the
// HCL addition '_'. ASCII, where ID_Start is the letters, skips the search.
func identifierStart(r rune) bool {
	if r < utf8.RuneSelf {
		return asciiLetter(r) || r == '_'
	}
	return inRanges(r, idStart[:])
}

// identifierContinue reports whether r may continue an identifier: ID_Continue
// or the HCL addition '-'. ASCII ID_Continue is letters, digits, and '_'.
func identifierContinue(r rune) bool {
	if r < utf8.RuneSelf {
		return asciiLetter(r) || '0' <= r && r <= '9' || r == '_' || r == '-'
	}
	return inRanges(r, idContinue[:])
}

func asciiLetter(r rune) bool { return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }
