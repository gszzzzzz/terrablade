package lexer

import "sort"

// HCL identifiers use the Unicode derived properties ID_Start and ID_Continue,
// not just letter and digit categories. For example, ID_Start includes U+2118
// (SCRIPT CAPITAL P), while ID_Continue also admits combining marks. Combining
// unicode.IsLetter and unicode.IsDigit would not implement these properties.
//
// Go's Unicode tables follow the toolchain version. Terrablade instead pins
// Unicode 17.0 for its chosen HCL lexical baseline, so upgrading Go does not
// silently change token boundaries. The generated ranges in unicode_tables.go
// are checked in; normal builds need neither downloads nor generation.
// Regenerate with go generate ./internal/lexer from the repository root. The
// generator checks the official DerivedCoreProperties.txt SHA-256 before
// emitting ID_Start/ID_Continue ranges. The HCL additions '_' and '-' stay here.
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
