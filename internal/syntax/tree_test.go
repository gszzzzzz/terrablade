package syntax

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestAssembleFile(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		kinds  []Kind
	}{
		{"empty", "", []Kind{EOF}},
		{"trivia", " \t# comment\r\n/* block */\n", []Kind{
			Whitespace, LineComment, Newline, BlockComment, Newline, EOF,
		}},
		{"attribute spelling", "x = (1 + 2) # end\n", []Kind{
			Identifier, Whitespace, Equal, Whitespace, OpenParen, Number,
			Whitespace, Plus, Whitespace, Number, CloseParen, Whitespace,
			LineComment, Newline, EOF,
		}},
		{"BOM and unicode", "\uFEFF한글 = 1\n", []Kind{
			BOM, Identifier, Whitespace, Equal, Whitespace, Number, Newline, EOF,
		}},
		{"template", `"hi ${x}"`, []Kind{
			QuoteOpen, TemplateText, InterpolationOpen, Identifier,
			TemplateSequenceEnd, QuoteClose, EOF,
		}},
		{"malformed bytes", "\xff/* open", []Kind{Invalid, BlockComment, EOF}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := assembleFile([]byte(test.source))
			assertFilePartition(t, []byte(test.source), file)
			var kinds []Kind
			for _, child := range file.root.Children() {
				token, ok := child.(SyntaxToken)
				if !ok {
					t.Fatalf("flat foundation child has type %T", child)
				}
				kinds = append(kinds, token.Kind())
			}
			if !reflect.DeepEqual(kinds, test.kinds) {
				t.Fatalf("kinds = %v, want %v", kinds, test.kinds)
			}
		})
	}
}

func TestFileOwnsSourceAndTree(t *testing.T) {
	source := []byte("x = 1\n")
	original := bytes.Clone(source)
	file := assembleFile(source)
	if !bytes.Equal(source, original) {
		t.Fatal("assembly changed caller source")
	}
	for i := range source {
		source[i] = 'z'
	}
	children := file.root.Children()
	children[0] = SyntaxToken{}
	children = append(children, SyntaxNode{})
	root := file.root
	root = SyntaxNode{}
	if root.Kind() != InvalidNode {
		t.Fatal("zero node must not claim to be a file")
	}
	assertFilePartition(t, original, file)
	first := file.root.Children()[0].(SyntaxToken)
	if first.Kind() != Identifier {
		t.Fatal("changing returned children changed stored tree")
	}
}

func TestFilePreservesLexicalDiagnostics(t *testing.T) {
	file := assembleFile([]byte("/*\xff"))
	want := []Diagnostic{
		{Kind: UnterminatedBlockComment, Span: Span{Start: 0, End: 3}},
		{Kind: InvalidUTF8, Span: Span{Start: 2, End: 3}},
	}
	if !reflect.DeepEqual(file.diagnostics, want) {
		t.Fatalf("diagnostics = %+v, want %+v", file.diagnostics, want)
	}
	assertFilePartition(t, []byte("/*\xff"), file)
}

func TestFileDeepInput(t *testing.T) {
	// The file foundation is iterative; grammar nesting limits belong to the
	// later recursive parser, not to lossless token storage.
	source := []byte(strings.Repeat("[", 10000) + strings.Repeat("]", 10000))
	assertFilePartition(t, source, assembleFile(source))
}

func FuzzAssembleFile(f *testing.F) {
	for _, source := range []string{
		"", "x = 1\r\n", "\uFEFF# header\n/* comment */\n",
		"/*\xff", "\x00\xc0\xaf\xed\xa0\x80", `"${{a="${x}"}}"`,
		"<<-END\n%{if x}${y}%{endif}\n END\n", "\"${\"${",
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		original := bytes.Clone(source)
		file := assembleFile(source)
		assertFilePartition(t, original, file)
		if !bytes.Equal(source, original) {
			t.Fatal("assembly changed source")
		}
		if second := assembleFile(source); !reflect.DeepEqual(file, second) {
			t.Fatal("assembly is not deterministic")
		}
		// Changing the input buffer after assembly cannot invalidate tree spans
		// or alter text, including invalid UTF-8 and embedded NUL bytes.
		clear(source)
		assertFilePartition(t, original, file)
	})
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
			for _, child := range element.Children() {
				visit(child)
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
