package lexer

import "sort"

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
