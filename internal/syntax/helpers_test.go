package syntax

// Helpers shared by multiple test files.

import (
	"bytes"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type tokenText struct {
	kind TokenKind
	text string
}

// assertTokenPartition checks the losslessness rule that both phases share: a
// token sequence tiles the source with no gap or overlap, only the final EOF
// is empty, a BOM token spans exactly one U+FEFF, and concatenating the tokens
// reproduces the input. The lexer's own output and the leaves of a parsed tree
// both pass through here so the rule is written down once.
func assertTokenPartition(t *testing.T, source []byte, tokens []SyntaxToken) {
	t.Helper()
	if len(tokens) == 0 {
		t.Fatal("missing EOF")
	}
	end := 0
	var reconstructed []byte
	for i, token := range tokens {
		span := token.Span()
		if span.Start != end || span.End < span.Start || span.End > len(source) {
			t.Fatalf("invalid partition at token %d: %+v, previous end %d", i, token, end)
		}
		if token.Kind() == EOF {
			if i != len(tokens)-1 || span.Start != len(source) || span.End != len(source) {
				t.Fatalf("invalid EOF: %+v", token)
			}
		} else if span.Start == span.End {
			t.Fatalf("empty non-EOF token: %+v", token)
		}
		if token.Kind() == BOM && !bytes.Equal(source[span.Start:span.End], []byte("\uFEFF")) {
			t.Fatalf("BOM token does not span exactly one UTF-8 BOM: %+v", token)
		}
		reconstructed = append(reconstructed, source[span.Start:span.End]...)
		end = span.End
	}
	if tokens[len(tokens)-1].Kind() != EOF || end != len(source) {
		t.Fatal("missing final EOF or source bytes")
	}
	if !bytes.Equal(reconstructed, source) {
		t.Fatal("tokens do not reconstruct source")
	}
}

// assertDiagnosticOrder checks the ordering and bounds Result.Diagnostics
// promises. Both phases produce diagnostics, so both check them the same way.
func assertDiagnosticOrder(t *testing.T, source []byte, diagnostics []Diagnostic) {
	t.Helper()
	lastStart := -1
	for _, diagnostic := range diagnostics {
		span := diagnostic.Span
		if span.Start < lastStart || span.Start < 0 || span.End < span.Start || span.End > len(source) {
			t.Fatalf("invalid diagnostic span/order: %+v", diagnostic)
		}
		lastStart = span.Start
	}
}

// assertLexInvariants checks a lexer result, which has no tree around it yet.
func assertLexInvariants(t *testing.T, source []byte, result lexResult) {
	t.Helper()
	assertTokenPartition(t, source, result.Tokens)
	assertDiagnosticOrder(t, source, result.Diagnostics)
}

// assertTreeInvariants is the one internal walker over a parsed tree. It
// checks that the Result owns its source and a File spanning every byte, that
// node spans exactly cover their children in source order, that the leaves are
// exactly the lexer's tokens, that only File and Body may begin or end with
// trivia, and that error nodes imply at least one diagnostic. Diagnostics
// without error nodes remain legitimate, for example a missing closer or an
// unrepresentable number literal. Exact ErrorNode-to-diagnostic relationships
// belong to focused recovery tests.
//
// checkResult in result_test.go is the public twin of this walker: it asserts
// the same shape through the exported API, which cannot reach the unexported
// fields used here, so the two cannot be merged.
func assertTreeInvariants(t *testing.T, source []byte, file Result) {
	t.Helper()
	if file.source != string(source) {
		t.Fatal("file source differs from input")
	}
	if file.root.Kind() != File || file.root.Span() != (Span{Start: 0, End: len(source)}) {
		t.Fatalf("invalid root kind/span: %v %+v", file.root.Kind(), file.root.Span())
	}

	type frame struct {
		element SyntaxElement
		exit    bool
	}
	var leaves []SyntaxToken
	end, errors := 0, 0
	stack := []frame{{element: file.root.Element()}}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		element := current.element
		span := element.Span()
		if current.exit {
			if end != span.End {
				t.Fatalf("node span ends at %d, children end at %d", span.End, end)
			}
			continue
		}
		if span.Start != end || span.End < span.Start || span.End > len(source) {
			t.Fatalf("non-contiguous or invalid span %+v after %d", span, end)
		}
		if node, ok := element.Node(); ok {
			if node.Kind() == ErrorNode {
				errors++
			}
			count := node.ChildCount()
			if node.Kind() != File && node.Kind() != Body && count > 0 {
				if token, ok := node.Child(0).Token(); ok && isTrivia(token.Kind()) {
					t.Fatalf("%v begins with %v trivia: %s", node.Kind(), token.Kind(), expressionShape(file, element))
				}
				if token, ok := node.Child(count - 1).Token(); ok && isTrivia(token.Kind()) {
					t.Fatalf("%v ends with %v trivia: %s", node.Kind(), token.Kind(), expressionShape(file, element))
				}
			}
			stack = append(stack, frame{element: element, exit: true})
			for i := count - 1; i >= 0; i-- {
				stack = append(stack, frame{element: node.Child(i)})
			}
		} else if token, ok := element.Token(); ok {
			leaves = append(leaves, SyntaxToken{kind: token.Kind(), span: span})
			end = span.End
		} else {
			t.Fatal("invalid element in tree")
		}
	}

	assertTokenPartition(t, source, leaves)
	if want := lex(source).Tokens; !reflect.DeepEqual(leaves, want) {
		t.Fatalf("tree leaves differ from lexer tokens:\n%+v\nwant:\n%+v", leaves, want)
	}
	if errors > 0 && len(file.diagnostics) == 0 {
		t.Fatalf("%d error nodes without any diagnostic: %s", errors, expressionShape(file, file.root.Element()))
	}
	assertDiagnosticOrder(t, source, file.diagnostics)
}

// assertTokens pins the exact token stream of a source the lexer must accept
// without diagnostics.
func assertTokens(t *testing.T, text string, want []tokenText) {
	t.Helper()
	source := []byte(text)
	result := lex(source)
	assertLexInvariants(t, source, result)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	var got []tokenText
	for _, token := range result.Tokens[:len(result.Tokens)-1] {
		got = append(got, tokenText{token.Kind(), string(source[token.Span().Start:token.Span().End])})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens = %#v\nwant %#v", got, want)
	}
}

// assertDiagnosticsAndShape pins the exact diagnostics, including spans, and
// the recovered tree so that cascading errors and recovery regressions surface.
func assertDiagnosticsAndShape(t *testing.T, source string, diagnostics []Diagnostic, shape string) {
	t.Helper()
	file := parseExpressionSource([]byte(source))
	assertTreeInvariants(t, []byte(source), file)
	if !reflect.DeepEqual(file.diagnostics, diagnostics) {
		t.Errorf("diagnostics = %+v\nwant %+v", file.diagnostics, diagnostics)
	}
	if got := expressionShape(file, file.root.Element()); got != shape {
		t.Errorf("shape:\n%s\nwant:\n%s", got, shape)
	}
}

// assertBody is the whole-file counterpart of assertDiagnosticsAndShape: it
// parses a configuration rather than a bare expression and prints the tree
// vertically, because body trees nest too deeply to read on one line.
func assertBody(t *testing.T, source string, diagnostics []Diagnostic, shape string) {
	t.Helper()
	file := Parse([]byte(source))
	assertTreeInvariants(t, []byte(source), file)
	if !reflect.DeepEqual(file.diagnostics, diagnostics) {
		t.Errorf("diagnostics: %+v\nwant: %+v", file.diagnostics, diagnostics)
	}
	if got := bodyShape(file, file.root.Element()); got != shape {
		t.Errorf("tree:\n%s\nwant:\n%s", got, shape)
	}
}

// The two shape printers differ only in layout: expressionShape nests on one
// line, bodyShape indents one element per line. Both omit trivia, for
// readability alone; assertTreeInvariants and the trivia-ownership tests
// independently pin every token, every span, and each comment's parent.
func expressionShape(file Result, current SyntaxElement) string {
	if element, ok := current.Token(); ok {
		if isTrivia(element.Kind()) || element.Kind() == EOF {
			return ""
		}
		span := element.Span()
		return strconv.Quote(file.source[span.Start:span.End])
	} else if element, ok := current.Node(); ok {
		names := shapeNodeNames
		var children []string
		for i := range element.ChildCount() {
			if child := expressionShape(file, element.Child(i)); child != "" {
				children = append(children, child)
			}
		}
		return names[element.Kind()] + "(" + strings.Join(children, ", ") + ")"
	}
	return "<invalid>"
}

func bodyShape(file Result, root SyntaxElement) string {
	var out strings.Builder
	var visit func(SyntaxElement, int)
	visit = func(element SyntaxElement, depth int) {
		if node, ok := element.Node(); ok {
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(shapeNodeNames[node.Kind()])
			out.WriteByte('\n')
			for i := range node.ChildCount() {
				visit(node.Child(i), depth+1)
			}
		} else if token, ok := element.Token(); ok && !isTrivia(token.Kind()) && token.Kind() != EOF {
			out.WriteString(strings.Repeat("  ", depth))
			span := token.Span()
			out.WriteString(strconv.Quote(file.source[span.Start:span.End]))
			out.WriteByte('\n')
		}
	}
	visit(root, 0)
	return strings.TrimSuffix(out.String(), "\n")
}

var shapeNodeNames = map[NodeKind]string{
	File: "File", ErrorNode: "Error", LiteralExpression: "Literal",
	VariableExpression: "Variable", ParenthesizedExpression: "Paren",
	UnaryExpression: "Unary", BinaryExpression: "Binary",
	ConditionalExpression: "Conditional", FunctionCallExpression: "Call",
	TraversalExpression: "Traversal", AttributeAccess: "AttrAccess",
	IndexAccess: "Index", LegacyIndexAccess: "LegacyIndex",
	AttributeSplat: "AttributeSplat", FullSplat: "FullSplat",
	TupleExpression:  "Tuple",
	ObjectExpression: "Object", ObjectItem: "Item",
	ForExpression:      "For",
	TemplateExpression: "Template", TemplateInterpolation: "Interpolation",
	TemplateDirective: "Directive", TemplateIf: "TemplateIf", TemplateFor: "TemplateFor",
	Body: "Body", Attribute: "Attribute", Block: "Block", BlockLabel: "Label",
}

// countTree walks every node and token with a caller-owned stack, which is how
// the allocation tests and benchmarks traverse without allocating.
func countTree(root SyntaxNode, stack []SyntaxElement) (nodes, tokens, width int) {
	stack = append(stack[:0], root.Element())
	for len(stack) > 0 {
		element := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node, ok := element.Node(); ok {
			nodes++
			for i := node.ChildCount() - 1; i >= 0; i-- {
				stack = append(stack, node.Child(i))
			}
		} else if token, ok := element.Token(); ok {
			tokens++
			span := token.Span()
			width += span.End - span.Start
		}
	}
	return nodes, tokens, width
}

// assertEnumNames checks a String table against the enumeration's own count,
// so a kind added without a name fails here rather than printing a number.
func assertEnumNames(t *testing.T, want []string, count uint8, name func(uint8) string) {
	t.Helper()
	if len(want) != int(count) {
		t.Fatalf("test covers %d values, enum defines %d", len(want), count)
	}
	for value, expected := range want {
		if got := name(uint8(value)); got != expected {
			t.Errorf("value %d = %q, want %q", value, got, expected)
		}
	}
}
