package syntax

import (
	"sort"
	"unicode/utf8"
)

// HCL identifiers use the Unicode ID_Start and ID_Continue properties, which
// package unicode does not provide. unicode_tables.go pins them to one Unicode
// version so token boundaries do not vary with the Go toolchain.
//
// To update them, run go generate; the generator checks the pinned SHA-256
// of DerivedCoreProperties.txt.
//
//go:generate go run ../../tools/genunicode

type runeRange struct{ lo, hi rune }

// inRanges reports whether r is in one of the sorted ranges.
func inRanges(r rune, ranges []runeRange) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].hi >= r })
	return i < len(ranges) && ranges[i].lo <= r
}

// identifierStart reports whether r is ID_Start or '_'.
func identifierStart(r rune) bool {
	if r < utf8.RuneSelf {
		return asciiLetter(r) || r == '_'
	}
	return inRanges(r, idStart[:])
}

// identifierContinue reports whether r is ID_Continue or '-'.
func identifierContinue(r rune) bool {
	if r < utf8.RuneSelf {
		return asciiLetter(r) || '0' <= r && r <= '9' || r == '_' || r == '-'
	}
	return inRanges(r, idContinue[:])
}

func asciiLetter(r rune) bool { return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }
