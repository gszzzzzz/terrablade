package syntax_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"

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
	root := result.Root()
	foundTail := false
	for i := range root.ChildCount() {
		if node, ok := root.Child(i).Node(); ok && node.Kind() == syntax.Error {
			span := node.Span()
			foundTail = strings.Contains(result.Source()[span.Start:span.End], "tail=1")
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
	})
}
