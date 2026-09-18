package syntax_test

import (
	"bytes"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"terrablade/internal/syntax"
)

func TestParse(t *testing.T) {
	for _, test := range []struct {
		name, source string
		errors       bool
	}{
		{"empty", "", false},
		{"trivia", " \t# comment\r\n/* block */\n", false},
		{"BOM", "\ufeffa = 1\n", false},
		{"body", "resource \"example\" name {\n value = f(var.items[*].name, [1,2], {a=true})\n}\n", false},
		{"templates", "locals {\n values = [for k,v in var.items : {name=k, data=v} if v != null]\n message = \"%{if var.ok}${join(\",\", var.values)}%{else}none%{endif}\"\n script = <<-EOT\n  %{for x in var.items}${x}\n  %{endfor}\n  EOT\n}\n", false},
		{"invalid bytes", "\xff\x00\r", true},
		{"missing value", "a =\nb = 2\n", true},
		{"unclosed block", "b {\n a = 1\n", true},
		{"unclosed template", "a = \"${f(a}\"\n", true},
		{"duplicate attribute", "a = 1\na = 2\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := []byte(test.source)
			result := syntax.Parse(input)
			if !bytes.Equal(input, []byte(test.source)) {
				t.Fatal("Parse changed the caller's input")
			}
			if got := len(result.Diagnostics()) != 0; got != test.errors {
				t.Fatalf("diagnostics = %+v, want errors %v", result.Diagnostics(), test.errors)
			}
			first := checkResult(t, result, test.source)
			second := syntax.Parse(input)
			if !slices.Equal(first, checkResult(t, second, test.source)) || !slices.Equal(result.Diagnostics(), second.Diagnostics()) {
				t.Fatal("identical input produced different trees or diagnostics")
			}
			clear(input)
			if !slices.Equal(first, checkResult(t, result, test.source)) {
				t.Fatal("reusing the caller's input changed the result")
			}
		})
	}
}

func TestResultZeroAndEmpty(t *testing.T) {
	var zero syntax.Result
	if zero.Source() != "" || zero.Diagnostics() != nil || zero.Root() != (syntax.SyntaxNode{}) {
		t.Fatal("zero Result must contain no source, diagnostics, or tree")
	}
	if zero.Root().Kind() != syntax.InvalidNode || zero.Root().Span() != (syntax.Span{}) || zero.Root().ChildCount() != 0 {
		t.Fatal("zero Result must have an invalid, empty root")
	}
	for _, input := range [][]byte{nil, {}} {
		result := syntax.Parse(input)
		checkResult(t, result, "")
		root := result.Root()
		if root.ChildCount() != 2 || result.Diagnostics() != nil {
			t.Fatal("parsed empty input must contain only Body and EOF without diagnostics")
		}
		body, ok := root.Child(0).Node()
		if !ok || body.Kind() != syntax.Body || body.ChildCount() != 0 {
			t.Fatal("parsed empty input must contain an empty Body")
		}
		eof, ok := root.Child(1).Token()
		if !ok || eof.Kind() != syntax.EOF || eof.Span() != (syntax.Span{}) {
			t.Fatal("parsed empty input must end in a zero-width EOF")
		}
	}
}

func TestResultCopiesAndHandleLifetime(t *testing.T) {
	result := syntax.Parse([]byte("name = value\n"))
	copied := result
	want := checkResult(t, copied, "name = value\n")
	result = syntax.Parse([]byte("other = true\n"))
	if !slices.Equal(want, checkResult(t, copied, "name = value\n")) {
		t.Fatal("replacing a Result changed a previous copy")
	}
	checkResult(t, result, "other = true\n")

	// Only the retained handles and source string survive this scope. No Result
	// is needed to keep tree storage alive, and text is retained independently.
	root, element, token, source := func() (syntax.SyntaxNode, syntax.SyntaxElement, syntax.SyntaxToken, string) {
		parsed := syntax.Parse([]byte("answer = 42\n"))
		body, _ := parsed.Root().Child(0).Node()
		attribute, _ := body.Child(0).Node()
		leaf := attribute.Child(0)
		token, _ := leaf.Token()
		return parsed.Root(), leaf, token, parsed.Source()
	}()
	runtime.GC()
	if root.Kind() != syntax.File || root.Span() != (syntax.Span{Start: 0, End: len(source)}) {
		t.Fatal("node did not retain its tree")
	}
	if retained, ok := element.Token(); !ok || retained != token || token.Kind() != syntax.Identifier {
		t.Fatal("element did not retain its lexical value")
	}
	span := token.Span()
	if source[span.Start:span.End] != "answer" {
		t.Fatal("retained token span no longer identifies its source text")
	}
	body, _ := root.Child(0).Node()
	attribute, _ := body.Child(0).Node()
	if attribute.Child(0) != element {
		t.Fatal("retained node and element disagree about tree identity")
	}
}

func TestResultDiagnosticOwnershipAndOrder(t *testing.T) {
	// Both offsets produce a lexical and a parser error. Lexing the whole input
	// before parsing must not reorder later lexical errors before earlier ones.
	result := syntax.Parse([]byte("\xff\nx=\xff\n"))
	want := []syntax.Diagnostic{
		{Kind: syntax.InvalidUTF8, Span: syntax.Span{Start: 0, End: 1}},
		{Kind: syntax.ExpectedBodyItem, Span: syntax.Span{Start: 0, End: 1}},
		{Kind: syntax.InvalidUTF8, Span: syntax.Span{Start: 4, End: 5}},
		{Kind: syntax.ExpectedExpression, Span: syntax.Span{Start: 4, End: 5}},
	}
	if got := result.Diagnostics(); !slices.Equal(got, want) {
		t.Fatalf("diagnostics = %+v, want %+v", got, want)
	}
	copyOfResult := result
	diagnostics := result.Diagnostics()
	clear(diagnostics)
	diagnostics = append(diagnostics, syntax.Diagnostic{Kind: syntax.DuplicateAttribute})
	if !slices.Equal(result.Diagnostics(), want) || !slices.Equal(copyOfResult.Diagnostics(), want) {
		t.Fatal("mutating returned diagnostics changed shared Result storage")
	}
	if len(diagnostics) != len(want)+1 {
		t.Fatal("diagnostic copy could not be extended")
	}
}

func TestResultConcurrentReads(t *testing.T) {
	source := "block {\n a = \xff\n}\n"
	result := syntax.Parse([]byte(source))
	wantTree := checkResult(t, result, source)
	wantDiagnostics := result.Diagnostics()
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			copied := result
			for range 20 {
				diagnostics := copied.Diagnostics()
				if !slices.Equal(diagnostics, wantDiagnostics) || !slices.Equal(checkResult(t, copied, source), wantTree) {
					t.Error("concurrent access changed an immutable result")
					return
				}
				clear(diagnostics)
			}
		})
	}
	readers.Wait()
}

func TestResultAccessAllocations(t *testing.T) {
	result := syntax.Parse([]byte("a = [for x in xs : x]\n"))
	stack := make([]syntax.SyntaxElement, 0, 64)
	width := 0
	allocations := testing.AllocsPerRun(100, func() {
		width = 0
		if len(result.Diagnostics()) != 0 {
			panic("valid input has diagnostics")
		}
		source := result.Source()
		stack = append(stack[:0], result.Root().Element())
		for len(stack) != 0 {
			element := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if node, ok := element.Node(); ok {
				for i := node.ChildCount() - 1; i >= 0; i-- {
					stack = append(stack, node.Child(i))
				}
			} else if token, ok := element.Token(); ok {
				span := token.Span()
				width += len(source[span.Start:span.End])
			}
		}
	})
	if width != len(result.Source()) || allocations != 0 {
		t.Fatalf("access/traversal visited %d bytes with %g allocations", width, allocations)
	}
}

// treeEntry records only the public observations, not private storage identity.
type treeEntry struct {
	node     syntax.NodeKind
	token    syntax.TokenKind
	span     syntax.Span
	children int
}

func checkResult(t *testing.T, result syntax.Result, source string) []treeEntry {
	t.Helper()
	root := result.Root()
	if result.Source() != source || root.Kind() != syntax.File || root.Span() != (syntax.Span{Start: 0, End: len(source)}) {
		t.Fatal("parse result must own the source and a File spanning every byte")
	}
	type frame struct {
		element syntax.SyntaxElement
		exit    bool
	}
	stack := []frame{{element: root.Element()}}
	var entries []treeEntry
	var reconstructed strings.Builder
	end, eof, errors := 0, 0, 0
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		span := current.element.Span()
		if current.exit {
			if span.End != end {
				t.Fatalf("node ends at %d, children end at %d", span.End, end)
			}
			continue
		}
		if span.Start != end || span.End < span.Start || span.End > len(source) {
			t.Fatalf("invalid or non-contiguous span %+v after %d", span, end)
		}
		if node, ok := current.element.Node(); ok {
			entries = append(entries, treeEntry{node: node.Kind(), span: span, children: node.ChildCount()})
			if node.Kind() == syntax.Error {
				errors++
			}
			stack = append(stack, frame{element: current.element, exit: true})
			for i := node.ChildCount() - 1; i >= 0; i-- {
				stack = append(stack, frame{element: node.Child(i)})
			}
		} else if token, ok := current.element.Token(); ok {
			entries = append(entries, treeEntry{token: token.Kind(), span: span})
			if eof != 0 {
				t.Fatal("token appears after EOF")
			}
			if token.Kind() == syntax.EOF {
				eof++
				if span != (syntax.Span{Start: len(source), End: len(source)}) {
					t.Fatalf("invalid EOF span %+v", span)
				}
			} else if span.Start == span.End {
				t.Fatal("non-EOF token is empty")
			}
			reconstructed.WriteString(result.Source()[span.Start:span.End])
			end = span.End
		} else {
			t.Fatal("invalid element in parsed tree")
		}
	}
	if eof != 1 || reconstructed.String() != source {
		t.Fatal("tree must reconstruct the original bytes and have one final EOF")
	}
	diagnostics := result.Diagnostics()
	if errors > 0 && len(diagnostics) == 0 {
		t.Fatal("Error nodes must be accompanied by diagnostics")
	}
	previous := 0
	for _, diagnostic := range diagnostics {
		span := diagnostic.Span
		if span.Start < previous || span.End < span.Start || span.End > len(source) {
			t.Fatalf("invalid diagnostic span or order: %+v", diagnostic)
		}
		previous = span.Start
	}
	return entries
}
