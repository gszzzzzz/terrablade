package syntax_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/apparentlymart/go-textseg/v17/textseg"

	"terrablade/internal/syntax"
)

func TestParseResourceRecovery(t *testing.T) {
	// The public contract promises bounded recursive descent with a lossless
	// result, not a particular numeric nesting threshold.
	source := "outer {\n a=" + strings.Repeat("!", 16384) + "value\n}\ntail=1\n"
	result := syntax.Parse([]byte(source))
	checkResult(t, result, source)
	diagnostics := result.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Kind != syntax.NestingLimitExceeded {
		t.Fatalf("recursive nesting produced %+v, want a single limit diagnostic", diagnostics)
	}
	limit := diagnostics[0]
	if limit.Kind.Message() != "Expression nesting exceeds the parser limit." {
		t.Fatalf("nesting limit message = %q", limit.Kind.Message())
	}
	if got := result.Position(limit.Span.Start); got.Offset != limit.Span.Start || got.Line != 2 || got.Column != limit.Span.Start-len("outer {\n")+1 {
		t.Fatalf("nesting limit position = %+v for span %+v", got, limit.Span)
	}
	if got := result.Position(len(source)); got.Line != 5 || got.Column != 1 {
		t.Fatalf("position in retained unparsed tail = %+v", got)
	}
	root := result.Root()
	foundTail := false
	for i := range root.ChildCount() {
		if node, ok := root.Child(i).Node(); ok && node.Kind() == syntax.ErrorNode {
			foundTail = strings.Contains(result.Text(node.Span()), "tail=1")
		}
	}
	if !foundTail {
		t.Fatal("the unparsed tail must remain available beside the partial Body")
	}
}

func TestParseDeepBody(t *testing.T) {
	const depth = 8192
	source := strings.Repeat("block {\n", depth) + "a=value\n" + strings.Repeat("}\n", depth)
	result := syntax.Parse([]byte(source))
	if len(result.Diagnostics()) != 0 {
		t.Fatalf("iterative body nesting produced diagnostics: %+v", result.Diagnostics())
	}
	entries := checkResult(t, result, source)
	blocks := 0
	for _, entry := range entries {
		if entry.node == syntax.Block {
			blocks++
		}
	}
	if blocks != depth {
		t.Fatalf("tree contains %d blocks, want %d", blocks, depth)
	}
}

// Keep regression discoveries in testdata/fuzz/FuzzParse; this target exercises
// only the exported interface, including independent diagnostic snapshots.
func FuzzParse(f *testing.F) {
	for _, source := range []string{
		"",
		"\ufeffa=1\n",
		"b name \"label\" {\n a={for k,v in xs:k=>v... if v}\n}\n",
		"a=\"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}\"\n",
		"a=<<-E\n ${x}\n E\nb=2",
		"a=[1 f(2,3),4]\nb=2",
		"a=1\na=2\n",
		"b \"${f(}tail\" {}\na=1\n",
		"a=\xff\nb=/*\x00",
		"\ufeffa=\"é🙂\"\r\n\tb=\xff\rc=1\n",
		"# e\u0301👩‍💻🇰🇷\u0600\xc0\xaf\n",
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		// Never mutate the fuzz engine's retained input during ownership checks.
		input := bytes.Clone(source)
		result := syntax.Parse(input)
		if !bytes.Equal(input, source) {
			t.Fatal("Parse mutated its input")
		}
		wantSource := string(source)
		first := checkResult(t, result, wantSource)
		again := syntax.Parse(input)
		if !slices.Equal(first, checkResult(t, again, wantSource)) || !slices.Equal(result.Diagnostics(), again.Diagnostics()) {
			t.Fatal("Parse produced different results for identical bytes")
		}
		clear(result.Diagnostics())
		if !slices.Equal(result.Diagnostics(), again.Diagnostics()) {
			t.Fatal("mutating diagnostics changed the Result")
		}
		clear(input)
		if !slices.Equal(first, checkResult(t, result, wantSource)) {
			t.Fatal("reusing input changed the Result")
		}
		// Probe arbitrary byte boundaries as well as diagnostic endpoints. Keep
		// the number of lookups bounded: Position intentionally scans its prefix.
		offsets := []int{0, len(source) / 2, len(source)}
		diagnostics := result.Diagnostics()
		if len(diagnostics) != 0 {
			first, last := diagnostics[0], diagnostics[len(diagnostics)-1]
			offsets = append(offsets, first.Span.Start, first.Span.End, last.Span.Start, last.Span.End)
		}
		// A NUL is a single-byte control cluster that cannot join either neighbor.
		// Normalize malformed bytes this way for an oracle independent of the
		// implementation's valid runs, line slicing, and bounded lookahead.
		normalized := bytes.Clone(source)
		for index := 0; index < len(normalized); {
			runeValue, width := utf8.DecodeRune(normalized[index:])
			if runeValue == utf8.RuneError && width == 1 {
				normalized[index] = 0
			}
			index += width
		}
		for _, offset := range offsets {
			want := syntax.Position{Offset: offset, Line: 1, Column: 1}
			for index := 0; index < offset; {
				advance, cluster, _ := textseg.ScanGraphemeClusters(normalized[index:], true)
				if index+advance > offset {
					break
				}
				index += advance
				if cluster[len(cluster)-1] == '\n' {
					want.Line++
					want.Column = 1
				} else {
					want.Column++
				}
			}
			if got := result.Position(offset); got != want || again.Position(offset) != want {
				t.Fatalf("Position(%d) = %+v, want %+v on both independent parses", offset, got, want)
			}
		}
	})
}
