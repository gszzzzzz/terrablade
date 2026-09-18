package syntax

import (
	"bytes"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestBodyDeepNesting(t *testing.T) {
	const depth = 16384
	for _, test := range []struct {
		name, ending string
		missing      bool
	}{
		{"balanced", strings.Repeat("}\n", depth), false},
		{"unclosed", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := []byte(strings.Repeat("b {\n", depth) + "a=1\n" + test.ending)
			file := Parse(source)
			assertExpressionPartition(t, source, file)
			wantDiagnostics := 0
			if test.missing {
				wantDiagnostics = depth
			}
			if len(file.diagnostics) != wantDiagnostics {
				t.Fatalf("diagnostics = %d, want %d", len(file.diagnostics), wantDiagnostics)
			}
			for _, diagnostic := range file.diagnostics {
				if diagnostic != (Diagnostic{ExpectedClosingBrace, Span{len(source), len(source)}}) {
					t.Fatalf("unexpected diagnostic: %+v", diagnostic)
				}
			}
			nodes, _, _ := countTree(file.root, nil)
			if nodes != 2*depth+4 {
				t.Fatalf("nodes = %d, want %d", nodes, 2*depth+4)
			}
		})
	}
}

func TestBodyExpressionLimitRetainsOuterTail(t *testing.T) {
	source := []byte("outer {\n inner { a=" + strings.Repeat("!", maxRecursiveExpressionDepth+1) + "x }\n}\ntail=1\n")
	file := Parse(source)
	assertExpressionPartition(t, source, file)
	if len(file.diagnostics) != 1 || file.diagnostics[0].Kind != NestingLimitExceeded {
		t.Fatalf("expression shutdown cascaded into body errors: %+v", file.diagnostics)
	}
	// Once expression parsing halts, the file assembler owns the unparsed tail;
	// unfinished bodies cannot consume it through their normal trivia lookahead.
	last, ok := file.root.Child(file.root.ChildCount() - 3).Node()
	if !ok || last.Kind() != ErrorNode || !strings.HasSuffix(file.source[last.Span().Start:last.Span().End], "tail=1") {
		t.Fatal("file did not retain the unparsed body tail in its final ErrorNode")
	}
}

func TestBodyFlatItemsAndArena(t *testing.T) {
	const count = 20000
	var source strings.Builder
	for i := range count {
		source.WriteString("a")
		source.WriteString(strconv.Itoa(i))
		source.WriteString("=1\nb { a=2 }\n")
	}
	data := []byte(source.String())
	file := Parse(data)
	assertExpressionPartition(t, data, file)
	if len(file.diagnostics) != 0 {
		t.Fatalf("flat sibling bodies share attribute scopes: %+v", file.diagnostics)
	}
	arena := file.root.arena
	if len(arena.children) != len(arena.nodes)+len(arena.tokens)-1 {
		t.Fatal("arena contains duplicated or unreachable edges")
	}
	end := 0
	for _, node := range arena.nodes {
		if node.firstChild != end {
			t.Fatal("node ranges do not partition child storage")
		}
		end += node.childCount
	}
	if end != len(arena.children) {
		t.Fatal("unowned child references")
	}
}

func TestBodyAllocationScaling(t *testing.T) {
	// Variable values isolate body/arena storage from numeric validation's
	// deliberate math/big allocations.
	for _, pattern := range []string{"b { a=x }\n", "b {\n"} {
		measure := func(count int) float64 {
			source := strings.Repeat(pattern, count)
			if pattern == "b {\n" {
				source += strings.Repeat("}\n", count)
			}
			data := []byte(source)
			return testing.AllocsPerRun(5, func() { _ = Parse(data) })
		}
		small, large := measure(100), measure(1000)
		// Growth in the shared arena, scope table, and frame slices is allowed;
		// one heap allocation per body, builder, or label is not.
		if large > 2*small+24 {
			t.Fatalf("%q: 10x more bodies grew allocations from %g to %g", pattern, small, large)
		}
	}
}

func TestBodyLongLabelsAndRecovery(t *testing.T) {
	const count = 16384
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
	}{
		{
			"many labels",
			"b" + strings.Repeat(" \"x\"", count) + " {}\na=x",
			nil,
		},
		{
			"deep malformed construct",
			"? " + strings.Repeat("[", count) + "\nx\n" + strings.Repeat("]", count) + "\na=x",
			[]Diagnostic{{ExpectedBodyItem, Span{0, 1}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := Parse([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if !reflect.DeepEqual(file.diagnostics, test.diagnostics) {
				t.Fatalf("diagnostics = %+v, want %+v", file.diagnostics, test.diagnostics)
			}
			body, _ := file.root.Child(0).Node()
			last, _ := body.Child(body.ChildCount() - 1).Node()
			if last.Kind() != Attribute || file.source[last.Span().Start:last.Span().End] != "a=x" {
				t.Fatal("long construct swallowed the following attribute")
			}
		})
	}
}

// Inputs the fuzzer found interesting are checked in under testdata/fuzz/FuzzBody
// and run as part of the ordinary test suite, alongside the seeds below.
func FuzzBody(f *testing.F) {
	for _, test := range bodyCompatibilityCases {
		f.Add([]byte(test.source))
	}
	for _, source := range []string{
		"outer {\n ? [1 }\na=2\n",
		"b \"${f(}tail\" {}\na=1\n",
		"b \"${x #tail",
		"a=1 bad(\n2,3\n)\nb=2",
		"outer {\n a=[1 f(2,3),4]\n b=2\n}",
		"a=1 <<E\n%{if x}${x}%{endif}\nE\nb=2",
		"b {\n a=/*\xff",
		strings.Repeat("b {\n", 32) + "a=1\n" + strings.Repeat("}\n", 32),
		"b { a=" + strings.Repeat("!", maxRecursiveExpressionDepth+1) + "a }",
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		// Mutate only our clone for the ownership check: the fuzzing engine may
		// retain its original input after this callback for corpus minimization.
		input := bytes.Clone(source)
		file := Parse(input)
		assertExpressionPartition(t, source, file)
		if !bytes.Equal(input, source) {
			t.Fatal("body parser mutated input")
		}
		for _, diagnostic := range lex(source).Diagnostics {
			if !slices.Contains(file.diagnostics, diagnostic) {
				t.Fatalf("lost lexical diagnostic: %+v", diagnostic)
			}
		}
		if next := Parse(input); !reflect.DeepEqual(file, next) {
			t.Fatal("body parser is not deterministic")
		}
		clear(input)
		assertExpressionPartition(t, source, file)
	})
}
