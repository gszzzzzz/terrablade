package syntax

import (
	"bytes"
	"strings"
	"testing"
)

func TestFileOwnsSourceAndTree(t *testing.T) {
	source := []byte("x + 1\n")
	original := bytes.Clone(source)
	file := parseExpressionSource(source)
	if !bytes.Equal(source, original) {
		t.Fatal("parsing changed caller source")
	}
	for i := range source {
		source[i] = 'z'
	}
	var zero SyntaxNode
	if zero.Kind() != InvalidNode {
		t.Fatal("zero node must have the invalid kind")
	}
	child := file.root.Child(0).(SyntaxNode)
	child.kind = InvalidNode
	root := file.root
	root.kind = InvalidNode
	assertFilePartition(t, original, file)
	first := file.root.Child(0).(SyntaxNode)
	if first.Kind() != BinaryExpression {
		t.Fatal("changing returned child value changed stored tree")
	}
}

func TestChildTraversalAllocations(t *testing.T) {
	file := parseExpressionSource([]byte("x + 1 # comment\n"))
	width := 0
	allocations := testing.AllocsPerRun(100, func() {
		width = 0
		for i := range file.root.ChildCount() {
			span := file.root.Child(i).Span()
			width += span.End - span.Start
		}
	})
	if width != len(file.source) {
		t.Fatalf("traversed %d bytes, want %d", width, len(file.source))
	}
	if allocations != 0 {
		t.Fatalf("child traversal allocated %g times, want zero", allocations)
	}
}

func TestFileDeepInput(t *testing.T) {
	// Unsupported collections are recovered iteratively rather than recursing
	// through every balanced delimiter.
	source := []byte(strings.Repeat("[", 10000) + strings.Repeat("]", 10000))
	assertFilePartition(t, source, parseExpressionSource(source))
}

func assertFilePartition(t *testing.T, source []byte, file syntaxFile) {
	t.Helper()
	if file.source != string(source) {
		t.Fatal("file source differs from input")
	}
	if file.root.Kind() != File || file.root.Span() != (Span{Start: 0, End: len(source)}) {
		t.Fatalf("invalid root kind/span: %v %+v", file.root.Kind(), file.root.Span())
	}
	end, eofCount := 0, 0
	var reconstructed strings.Builder
	var visit func(SyntaxElement)
	visit = func(element SyntaxElement) {
		span := element.Span()
		if span.Start != end || span.End < span.Start || span.End > len(source) {
			t.Fatalf("non-contiguous or invalid span %+v after %d", span, end)
		}
		switch element := element.(type) {
		case SyntaxNode:
			for i := range element.ChildCount() {
				visit(element.Child(i))
			}
			if end != span.End {
				t.Fatalf("node span ends at %d, children end at %d", span.End, end)
			}
		case SyntaxToken:
			if eofCount != 0 {
				t.Fatal("token follows EOF")
			}
			if element.Kind() == EOF {
				eofCount++
				if span != (Span{Start: len(source), End: len(source)}) {
					t.Fatalf("invalid EOF span %+v", span)
				}
			} else if span.Start == span.End {
				t.Fatal("empty non-EOF token")
			}
			reconstructed.WriteString(file.source[span.Start:span.End])
			end = span.End
		default:
			t.Fatalf("unexpected element implementation %T", element)
		}
	}
	visit(file.root)
	if eofCount != 1 || reconstructed.String() != string(source) {
		t.Fatal("tree must contain one final EOF and reconstruct every input byte")
	}
	lastStart := -1
	for _, diagnostic := range file.diagnostics {
		span := diagnostic.Span
		if span.Start < lastStart || span.End < span.Start || span.End > len(source) {
			t.Fatalf("invalid diagnostic span/order: %+v", diagnostic)
		}
		lastStart = span.Start
	}
}
