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
		{"malformed UTF-8 and NUL", "\xff\x00\xc0\x80\n\xe2\x82", []int{0, 5}},
		{"newline in comment token", "/* a\nb */\nx=1", []int{0, 5, 10}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := []byte(test.source)
			result := syntax.Parse(input)
			clear(input)
			// These fixtures use one byte per rune. Explicit line starts describe
			// every CR/LF byte, empty line, and the final EOF.
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

func TestResultPositionRuneColumns(t *testing.T) {
	for _, test := range []struct {
		name, source string
		columns      []int
	}{
		{"valid/mixed widths", "aé🙂你", []int{1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 5}},
		{"valid/combining mark", "e\u0301", []int{1, 2, 2, 3}},
		{"valid/emoji ZWJ sequence", "👩‍💻", []int{1, 1, 1, 1, 2, 2, 2, 3, 3, 3, 3, 4}},
		{"valid/emoji modifier", "👍🏽", []int{1, 1, 1, 1, 2, 2, 2, 2, 3}},
		{"valid/regional indicators", "🇰🇷", []int{1, 1, 1, 1, 2, 2, 2, 2, 3}},
		{"valid/BOM", "\ufeffa", []int{1, 1, 1, 2, 3}},
		{"valid/unicode line characters", "\u0085\u2028\u2029", []int{1, 1, 2, 2, 2, 3, 3, 3, 4}},
		{"valid/replacement rune", "\ufffd", []int{1, 1, 1, 2}},
		{"malformed/invalid lead", "\xff", []int{1, 2}},
		{"malformed/stray continuations", "\x80\xbf", []int{1, 2, 3}},
		{"malformed/long continuation run", "\x80\x80\x80\x80\x80", []int{1, 2, 3, 4, 5, 6}},
		{"malformed/truncated three-byte sequence", "\xe2\x82", []int{1, 2, 3}},
		{"malformed/truncated four-byte sequence", "\xf0\x9f\x99", []int{1, 2, 3, 4}},
		{"malformed/overlong sequence", "\xc0\xaf", []int{1, 2, 3}},
		{"malformed/surrogate", "\xed\xa0\x80", []int{1, 2, 3, 4}},
		{"malformed/out of range", "\xf4\x90\x80\x80", []int{1, 2, 3, 4, 5}},
		{"mixed/invalid lead before valid rune", "\xc3é", []int{1, 2, 2, 3}},
		{"mixed/stray continuation after valid rune", "é\x80", []int{1, 1, 2, 3}},
		{"mixed/stray continuations around valid rune", "\x80🙂\x80", []int{1, 2, 2, 2, 2, 3, 4}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if len(test.columns) != len(test.source)+1 {
				t.Fatal("fixture must specify every byte offset, including EOF")
			}
			// Prefix with a CRLF to verify rune counting restarts on each line.
			input := []byte("# first\r\n" + test.source)
			result := syntax.Parse(input)
			clear(input)
			previous := 1
			for relative, column := range test.columns {
				offset := len("# first\r\n") + relative
				want := syntax.Position{Offset: offset, Line: 2, Column: column}
				got := result.Position(offset)
				if got != want {
					t.Errorf("Position(%d) = %+v, want %+v", offset, got, want)
				}
				if got.Column < previous {
					t.Errorf("column decreased from %d to %d at offset %d", previous, got.Column, offset)
				}
				previous = got.Column
			}
		})
	}
}

func TestResultPositionRuneLineEndings(t *testing.T) {
	result := syntax.Parse([]byte("é\r\n🙂\n"))
	for _, want := range []syntax.Position{
		{Offset: 0, Line: 1, Column: 1},
		{Offset: 1, Line: 1, Column: 1},
		{Offset: 2, Line: 1, Column: 2},
		{Offset: 3, Line: 1, Column: 3},
		{Offset: 4, Line: 2, Column: 1},
		{Offset: 5, Line: 2, Column: 1},
		{Offset: 6, Line: 2, Column: 1},
		{Offset: 7, Line: 2, Column: 1},
		{Offset: 8, Line: 2, Column: 2},
		{Offset: 9, Line: 3, Column: 1},
	} {
		if got := result.Position(want.Offset); got != want {
			t.Errorf("Position(%d) = %+v, want %+v", want.Offset, got, want)
		}
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
	result := syntax.Parse([]byte(strings.Repeat("# é🙂\xff\r\n", 100)))
	column := 0
	allocations := testing.AllocsPerRun(100, func() {
		for offset := range len(result.Source()) + 1 {
			column += result.Position(offset).Column
		}
	})
	if column == 0 || allocations != 0 {
		t.Fatalf("position columns sum to %d with %g allocations", column, allocations)
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
