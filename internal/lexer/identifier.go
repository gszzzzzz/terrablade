package lexer

import "sort"

// HCL identifiers require the Unicode derived properties ID_Start/ID_Continue,
// which unicode.IsLetter/IsDigit do not reproduce (for example, ID_Start admits
// U+2118 and ID_Continue admits combining marks). Go's Unicode data also varies
// with the toolchain, so Terrablade checks in generated Unicode 17.0 ranges in
// unicode_tables.go for both exact properties and stable token boundaries.
// Normal builds need no downloads or generation. To update the tables, run
// go generate ./internal/lexer from the repository root; the generator verifies
// the pinned SHA-256 of official DerivedCoreProperties.txt before emitting them.
// The HCL additions '_' and '-' stay here.
//
//go:generate go run ../../tools/genunicode

// UnicodeVersion pins identifiers independently of the Go toolchain's tables.
const UnicodeVersion = "17.0.0"

type runeRange struct{ lo, hi rune }

func inRanges(r rune, ranges []runeRange) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].hi >= r })
	return i < len(ranges) && ranges[i].lo <= r
}

func identifierStart(r rune) bool {
	return r == '_' || inRanges(r, idStart[:])
}

func identifierContinue(r rune) bool {
	return r == '-' || inRanges(r, idContinue[:])
}
