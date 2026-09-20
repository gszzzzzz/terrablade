package syntax_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestPositionUnicode17Conformance(t *testing.T) {
	// Vendored official boundary vectors keep the oracle independent of the
	// runtime grapheme library and make the Unicode contract testable offline.
	data, err := os.ReadFile("testdata/GraphemeBreakTest-17.0.0.txt")
	if err != nil {
		t.Fatal(err)
	}
	cases, offsets := 0, 0
	for number, line := range strings.Split(string(data), "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		cases++
		t.Run(strconv.Itoa(number+1), func(t *testing.T) {
			var source strings.Builder
			var boundaries []int
			for _, field := range fields {
				switch field {
				case "÷":
					boundaries = append(boundaries, source.Len())
				case "×":
				default:
					value, err := strconv.ParseInt(field, 16, 32)
					if err != nil {
						t.Fatal(err)
					}
					source.WriteRune(rune(value))
				}
			}
			text := source.String()
			result := syntax.Parse([]byte(text))
			want := syntax.Position{Line: 1, Column: 1}
			for index := 0; index+1 < len(boundaries); index++ {
				start, end := boundaries[index], boundaries[index+1]
				for offset := start; offset < end; offset++ {
					want.Offset = offset
					offsets++
					if got := result.Position(offset); got != want {
						t.Fatalf("Position(%d) = %+v, want %+v for %q", offset, got, want, text)
					}
				}
				if strings.Contains(text[start:end], "\n") {
					want.Line++
					want.Column = 1
				} else {
					want.Column++
				}
			}
			want.Offset = len(text)
			offsets++
			if got := result.Position(len(text)); got != want {
				t.Fatalf("Position(EOF) = %+v, want %+v for %q", got, want, text)
			}
		})
	}
	if cases != 766 || offsets != 5503 {
		t.Fatalf("fixture contains %d cases and %d offsets, want 766 and 5503", cases, offsets)
	}
}
