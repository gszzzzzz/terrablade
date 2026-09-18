package syntax_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"terrablade/internal/syntax"
)

func TestResultPosition(t *testing.T) {
	for _, test := range []struct {
		name, source string
		starts       []int
	}{
		{"empty", "", []int{0}},
		{"ASCII", "abc", []int{0}},
		{"LF", "ab\nc\n", []int{0, 3, 5}},
		{"consecutive LF", "\n\n\n", []int{0, 1, 2, 3}},
		{"CRLF", "ab\r\nc\r\n", []int{0, 4, 7}},
		{"mixed line endings", "a\r\nb\nc\r\n", []int{0, 3, 5, 8}},
		{"lone CR", "\rabc\r\r", []int{0}},
		{"tabs", "\t\ta\n\tb", []int{0, 4}},
		{"unicode interiors", "aé🙂\n你", []int{0, 8}},
		{"combining characters", "e\u0301\n", []int{0, 4}},
		{"unicode line characters", "\u0085\u2028\u2029", []int{0}},
		{"BOM", "\ufeffa=1\n", []int{0, 7}},
		{"malformed UTF-8 and NUL", "\xff\x00\xc0\x80\n\xe2\x82", []int{0, 5}},
		{"newline in comment token", "/* a\nb */\nx=1", []int{0, 5, 10}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := []byte(test.source)
			result := syntax.Parse(input)
			clear(input)
			// The explicit line starts describe every byte boundary, including
			// UTF-8 interiors, CR/LF bytes, empty lines, and the final EOF.
			for i, start := range test.starts {
				end := len(test.source) + 1
				if i+1 < len(test.starts) {
					end = test.starts[i+1]
				}
				for offset := start; offset < end; offset++ {
					want := syntax.Position{Offset: offset, Line: i + 1, Column: offset - start + 1}
					if got := result.Position(offset); got != want {
						t.Errorf("Position(%d) = %+v, want %+v", offset, got, want)
					}
				}
			}
		})
	}
}

func TestResultPositionZeroAndBounds(t *testing.T) {
	var zero syntax.Result
	if got := zero.Position(0); got != (syntax.Position{Offset: 0, Line: 1, Column: 1}) {
		t.Fatalf("zero Result.Position(0) = %+v", got)
	}
	for _, result := range []syntax.Result{zero, syntax.Parse(nil), syntax.Parse([]byte("a=1\n"))} {
		for _, offset := range []int{-1, -int(^uint(0)>>1) - 1, len(result.Source()) + 1, int(^uint(0) >> 1)} {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("Position(%d) did not panic for source of length %d", offset, len(result.Source()))
					}
				}()
				result.Position(offset)
			}()
		}
	}
}

func TestResultPositionCopiesAndConcurrentReads(t *testing.T) {
	const source = "\ufeffb {\r\n\tname=\"é🙂\"\n}\n\xff"
	result := syntax.Parse([]byte(source))
	copied := result
	want := make([]syntax.Position, len(source)+1)
	for offset := range want {
		want[offset] = result.Position(offset)
	}
	result = syntax.Parse([]byte("replacement=1\n"))
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			local := copied
			for range 20 {
				for offset, position := range want {
					if got := local.Position(offset); got != position {
						t.Errorf("copied Position(%d) = %+v, want %+v", offset, got, position)
					}
				}
				for _, diagnostic := range local.Diagnostics() {
					if diagnostic.Kind.Message() == "Unknown diagnostic." {
						t.Error("concurrent read lost a diagnostic message")
					}
				}
			}
		})
	}
	readers.Wait()
	if got := result.Position(len(result.Source())); got != (syntax.Position{Offset: 14, Line: 2, Column: 1}) {
		t.Fatalf("replacement Result.Position(EOF) = %+v", got)
	}
}

func TestResultPositionAllocations(t *testing.T) {
	result := syntax.Parse([]byte(strings.Repeat("# a comment\r\n", 100)))
	var position syntax.Position
	allocations := testing.AllocsPerRun(100, func() {
		position = result.Position(len(result.Source()))
	})
	if position.Line != 101 || position.Column != 1 || allocations != 0 {
		t.Fatalf("Position(EOF) = %+v with %g allocations", position, allocations)
	}
}

func BenchmarkResultPosition(b *testing.B) {
	for _, size := range []int{4096, 65536, 1048576} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			source := strings.Repeat("#"+strings.Repeat("x", 62)+"\n", size/64)
			result := syntax.Parse([]byte(source))
			var position syntax.Position
			b.ReportAllocs()
			b.SetBytes(int64(len(source)))
			for b.Loop() {
				position = result.Position(len(source))
			}
			if position.Offset != len(source) || position.Line != size/64+1 || position.Column != 1 {
				b.Fatalf("Position(EOF) = %+v", position)
			}
		})
	}
}
