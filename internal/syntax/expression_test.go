package syntax

import (
	"bytes"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestExpressionShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"number spelling",
			"012.30E-2",
			`File(Literal("012.30E-2"))`,
		},
		{
			"upstream empty fraction before exponent",
			"1.e2",
			`File(Literal("1.e2"))`,
		},
		{
			"upstream empty fraction with signed exponent",
			"1.e+2",
			`File(Literal("1.e+2"))`,
		},
		{
			"subtraction after empty-fraction exponent",
			"1.e2-foo",
			`File(Binary(Literal("1.e2"), "-", Variable("foo")))`,
		},
		{
			"contextual true",
			"true",
			`File(Literal("true"))`,
		},
		{
			"contextual false",
			"false",
			`File(Literal("false"))`,
		},
		{
			"contextual null",
			"null",
			`File(Literal("null"))`,
		},
		{
			"for is an ordinary variable here",
			"for",
			`File(Variable("for"))`,
		},
		{
			"multiply before addition",
			"1 + 2 * 3",
			`File(Binary(Literal("1"), "+", Binary(Literal("2"), "*", Literal("3"))))`,
		},
		{
			"same precedence associates left",
			"a / b % c * d",
			`File(Binary(Binary(Binary(Variable("a"), "/", Variable("b")), "%", Variable("c")), "*", Variable("d")))`,
		},
		{
			"subtraction associates left",
			"a - b + c",
			`File(Binary(Binary(Variable("a"), "-", Variable("b")), "+", Variable("c")))`,
		},
		{
			"all binary precedence levels",
			"a || b && c == d < e + f * g",
			`File(Binary(Variable("a"), "||", Binary(Variable("b"), "&&", Binary(Variable("c"), "==", Binary(Variable("d"), "<", Binary(Variable("e"), "+", Binary(Variable("f"), "*", Variable("g"))))))))`,
		},
		{
			"comparison operators associate left",
			"a <= b > c >= d",
			`File(Binary(Binary(Binary(Variable("a"), "<=", Variable("b")), ">", Variable("c")), ">=", Variable("d")))`,
		},
		{
			"equality associates left",
			"a != b == c",
			`File(Binary(Binary(Variable("a"), "!=", Variable("b")), "==", Variable("c")))`,
		},
		{
			"parentheses change grouping",
			"(1 + 2) * 3",
			`File(Binary(Paren("(", Binary(Literal("1"), "+", Literal("2")), ")"), "*", Literal("3")))`,
		},
		{
			"unary binds tighter than binary",
			"!-a + b",
			`File(Binary(Unary("!", Unary("-", Variable("a"))), "+", Variable("b")))`,
		},
		{
			"conditional false arm associates right",
			"a ? b : c ? d : e",
			`File(Conditional(Variable("a"), "?", Variable("b"), ":", Conditional(Variable("c"), "?", Variable("d"), ":", Variable("e"))))`,
		},
		{
			"conditional middle arm groups independently",
			"a || b ? c ? d : e : f",
			`File(Conditional(Binary(Variable("a"), "||", Variable("b")), "?", Conditional(Variable("c"), "?", Variable("d"), ":", Variable("e")), ":", Variable("f")))`,
		},
		{
			"empty function call",
			"f()",
			`File(Call("f", "(", ")"))`,
		},
		{
			"keyword is a contextual function name",
			"true(false, null,)",
			`File(Call("true", "(", Literal("false"), ",", Literal("null"), ",", ")"))`,
		},
		{
			"expanded final argument",
			"f(a, xs...)",
			`File(Call("f", "(", Variable("a"), ",", Variable("xs"), "...", ")"))`,
		},
		{
			"namespaced call and postfix",
			"provider::aws::f(x).id",
			`File(Traversal(Call("provider", "::", "aws", "::", "f", "(", Variable("x"), ")"), Attribute(".", "id")))`,
		},
		{
			"multiline call",
			"f(\n a, # line\n b\n)",
			`File(Call("f", "(", Variable("a"), ",", Variable("b"), ")"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", file.diagnostics)
			}
			if got := expressionShape(file, file.root); got != test.shape {
				t.Fatalf("shape:\n%s\nwant:\n%s", got, test.shape)
			}
		})
	}
}

func TestExpressionDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		kind         DiagnosticKind
	}{
		{
			"missing expression",
			"",
			ExpectedExpression,
		},
		{
			"missing operand",
			"a +  ",
			ExpectedExpression,
		},
		{
			"missing close paren",
			"(a + b",
			ExpectedClosingParen,
		},
		{
			"missing conditional colon",
			"a ? b",
			ExpectedConditionalColon,
		},
		{
			"missing function name",
			"a::()",
			ExpectedFunctionName,
		},
		{
			"missing function open paren",
			"a::b",
			ExpectedOpeningParen,
		},
		{
			"missing argument separator",
			"f(1 2, 3)",
			ExpectedArgumentSeparator,
		},
		{
			"expanded argument must be final",
			"f(a..., b)",
			ExpectedClosingParen,
		},
		{
			"line break cannot continue unparenthesized binary",
			"a\n+ b",
			UnexpectedToken,
		},
		{
			"trailing material",
			"a b",
			UnexpectedToken,
		},
		{
			"quoted template deferred",
			`"hello ${a}"`,
			UnsupportedExpression,
		},
		{
			"heredoc deferred",
			"<<END\nhello\nEND\n",
			UnsupportedExpression,
		},
		{
			"tuple deferred",
			"[1, 2]",
			UnsupportedExpression,
		},
		{
			"object deferred",
			"{a = 1}",
			UnsupportedExpression,
		},
		{
			"for expression deferred",
			"[for a in xs : a]",
			UnsupportedExpression,
		},
		{
			"template directives deferred",
			`"%{if a}x%{endif}"`,
			UnsupportedExpression,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if !slices.ContainsFunc(file.diagnostics, func(d Diagnostic) bool { return d.Kind == test.kind }) {
				t.Fatalf("missing diagnostic %v in %+v", test.kind, file.diagnostics)
			}
		})
	}
}

func TestExpressionRecoveryPreservesFollowingArgument(t *testing.T) {
	file := parseExpressionSource([]byte("f(1 2, 3)"))
	want := `File(Call("f", "(", Literal("1"), Error("2"), ",", Literal("3"), ")"))`
	if got := expressionShape(file, file.root); got != want {
		t.Fatalf("recovered shape = %s, want %s", got, want)
	}
}

func TestExpressionPreservesLexicalDiagnostics(t *testing.T) {
	source := []byte("a + /*\xff")
	file := parseExpressionSource(source)
	assertExpressionPartition(t, source, file)
	for _, diagnostic := range lex(source).Diagnostics {
		if !slices.Contains(file.diagnostics, diagnostic) {
			t.Fatalf("lost lexical diagnostic %+v", diagnostic)
		}
	}
}

func FuzzExpression(f *testing.F) {
	for _, source := range []string{
		"",
		"f(1 2, 3)",
		"a ? b : c ? d : e",
		"!-a.b[0] + 2",
		"provider::aws::f(a, xs...)",
		"foo.0e-1.*.bar[0][*].baz",
		"foo[*].*.0e1",
		"foo.0e1.0",
		"1.0.2",
		"1e1e2foo",
		"1.e2-foo",
		"(a /* x */ + # y\n b)",
		`f("${a}", [for a in b : a])`,
		"a + /*\xff",
		strings.Repeat("!", maxRecursiveExpressionDepth+1) + "a",
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, source []byte) {
		original := bytes.Clone(source)
		file := parseExpressionSource(source)
		assertExpressionPartition(t, original, file)
		if !bytes.Equal(source, original) {
			t.Fatal("parser mutated source")
		}
		if second := parseExpressionSource(source); !reflect.DeepEqual(file, second) {
			t.Fatal("parser is not deterministic")
		}
		clear(source)
		assertExpressionPartition(t, original, file)
	})
}

func assertExpressionPartition(t *testing.T, source []byte, file syntaxFile) {
	t.Helper()
	assertFilePartition(t, source, file)
	var tokens []SyntaxToken
	stack := []SyntaxElement{file.root}
	for len(stack) > 0 {
		element := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch element := element.(type) {
		case SyntaxNode:
			for i := element.ChildCount() - 1; i >= 0; i-- {
				stack = append(stack, element.Child(i))
			}
		case SyntaxToken:
			tokens = append(tokens, SyntaxToken{kind: element.Kind(), span: element.Span()})
		}
	}
	if want := lex(source).Tokens; !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tree leaves differ from lexer tokens:\n%+v\nwant:\n%+v", tokens, want)
	}
}

// expressionShape omits trivia only for readable grammar assertions. Separate
// partition and placement assertions verify every token, including all trivia.
func expressionShape(file syntaxFile, element SyntaxElement) string {
	switch element := element.(type) {
	case SyntaxToken:
		if isTrivia(element.Kind()) || element.Kind() == EOF {
			return ""
		}
		span := element.Span()
		return strconv.Quote(file.source[span.Start:span.End])
	case SyntaxNode:
		names := map[NodeKind]string{
			File: "File", Error: "Error", LiteralExpression: "Literal",
			VariableExpression: "Variable", ParenthesizedExpression: "Paren",
			UnaryExpression: "Unary", BinaryExpression: "Binary",
			ConditionalExpression: "Conditional", FunctionCallExpression: "Call",
			TraversalExpression: "Traversal", AttributeAccess: "Attribute",
			IndexAccess: "Index", LegacyIndexAccess: "LegacyIndex",
			AttributeSplat: "AttributeSplat", FullSplat: "FullSplat",
		}
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

func TestContiguousNumericCandidateDiagnostics(t *testing.T) {
	for _, test := range []struct {
		source string
		want   Diagnostic
	}{
		{
			"1.0.2",
			Diagnostic{
				Kind: InvalidNumber,
				Span: Span{Start: 0, End: 5},
			},
		},
		{
			"1..2",
			Diagnostic{
				Kind: InvalidNumber,
				Span: Span{Start: 0, End: 4},
			},
		},
		{
			"1...2",
			Diagnostic{
				Kind: InvalidNumber,
				Span: Span{Start: 0, End: 5},
			},
		},
		{
			"1e1e2",
			Diagnostic{
				Kind: InvalidNumber,
				Span: Span{Start: 0, End: 5},
			},
		},
		{
			"foo.0e1.0",
			Diagnostic{
				Kind: InvalidLegacyIndex,
				Span: Span{Start: 4, End: 9},
			},
		},
		{
			"f(1.0.2, x)",
			Diagnostic{
				Kind: InvalidNumber,
				Span: Span{Start: 2, End: 7},
			},
		},
	} {
		t.Run(test.source, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if len(file.diagnostics) != 1 || file.diagnostics[0] != test.want {
				t.Fatalf("diagnostics = %+v, want %+v", file.diagnostics, test.want)
			}
		})
	}
}

func TestExpressionDepthBoundaries(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(int) string
	}{
		{
			"unary recursion",
			func(count int) string { return strings.Repeat("!", count) + "a" },
		},
		{
			"parenthesis recursion",
			func(count int) string { return strings.Repeat("(", count) + "a" + strings.Repeat(")", count) },
		},
		{
			"conditional recursion",
			func(count int) string { return strings.Repeat("a ? b : ", count) + "c" },
		},
		{
			"full splat recursion",
			func(count int) string { return "foo" + strings.Repeat("[*]", count) },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			accepted := []byte(test.make(maxRecursiveExpressionDepth - 1))
			file := parseExpressionSource(accepted)
			assertExpressionPartition(t, accepted, file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("boundary input rejected: %+v", file.diagnostics)
			}
			for _, count := range []int{maxRecursiveExpressionDepth, maxRecursiveExpressionDepth * 8} {
				source := []byte(test.make(count))
				file = parseExpressionSource(source)
				assertExpressionPartition(t, source, file)
				if len(file.diagnostics) != 1 || file.diagnostics[0].Kind != NestingLimitExceeded {
					t.Fatalf("limit diagnostics = %+v", file.diagnostics)
				}
				if again := parseExpressionSource(source); !reflect.DeepEqual(file, again) {
					t.Fatal("depth recovery is not deterministic")
				}
			}
		})
	}
}

func TestFlatBinaryChainDoesNotHitRecursionLimit(t *testing.T) {
	const terms = maxRecursiveExpressionDepth * 8
	source := []byte(strings.Repeat("a + ", terms-1) + "a")
	file := parseExpressionSource(source)
	assertExpressionPartition(t, source, file)
	if len(file.diagnostics) != 0 {
		t.Fatalf("flat binary chain rejected: %+v", file.diagnostics)
	}
}
