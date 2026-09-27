package syntax

import (
	"testing"
	"unicode/utf8"
)

func TestIdentifierProperties(t *testing.T) {
	for _, test := range []struct {
		r                   rune
		start, continuation bool
	}{
		{'a', true, true}, {'_', true, true}, {'-', false, true},
		{'0', false, true}, {'한', true, true}, {'é', true, true},
		{'\u0301', false, true}, {'\u2118', true, true},
		{'\u00B7', false, true}, {'\u200C', false, true},
		{'\u200D', false, true}, {'\uFEFF', false, false},
		{'😀', false, false}, {'\u00A0', false, false},
		// Sidetic and Tolong Siki were added in Unicode 17.0.
		{'\U00010940', true, true}, {'\U00011DB0', true, true},
		{'\U0010FFFF', false, false},
	} {
		if got := identifierStart(test.r); got != test.start {
			t.Errorf("start(%U) = %v, want %v", test.r, got, test.start)
		}
		if got := identifierContinue(test.r); got != test.continuation {
			t.Errorf("continue(%U) = %v, want %v", test.r, got, test.continuation)
		}
	}
}

func TestUnicodeTablesAreOrdered(t *testing.T) {
	for name, ranges := range map[string][]runeRange{"start": idStart[:], "continue": idContinue[:]} {
		for i, r := range ranges {
			if r.lo > r.hi || r.lo < 0 || r.hi > 0x10FFFF {
				t.Fatalf("%s: invalid range %v", name, r)
			}
			if i > 0 && ranges[i-1].hi >= r.lo {
				t.Fatalf("%s: overlapping or unordered range %v", name, r)
			}
		}
	}
}

func TestIdentifierASCIIMatchesTables(t *testing.T) {
	for r := range rune(utf8.RuneSelf) {
		if got, want := identifierStart(r), r == '_' || inRanges(r, idStart[:]); got != want {
			t.Errorf("identifierStart(%q) = %v, table says %v", r, got, want)
		}
		if got, want := identifierContinue(r), r == '-' || inRanges(r, idContinue[:]); got != want {
			t.Errorf("identifierContinue(%q) = %v, table says %v", r, got, want)
		}
	}
}
