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
		{
			"parentheses allow operators after line breaks",
			"(a\n+ b # c\n* c)",
			`File(Paren("(", Binary(Variable("a"), "+", Binary(Variable("b"), "*", Variable("c"))), ")"))`,
		},
		{
			"unary before conditional",
			"!a ? -b : c",
			`File(Conditional(Unary("!", Variable("a")), "?", Unary("-", Variable("b")), ":", Variable("c")))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", file.diagnostics)
			}
			if got := expressionShape(file, file.root.Element()); got != test.shape {
				t.Fatalf("shape:\n%s\nwant:\n%s", got, test.shape)
			}
		})
	}
}

// TestExpressionDiagnostics pins every diagnostic, its span, and the recovered
// tree for malformed input. A second diagnostic is expected only where the
// remainder genuinely cannot belong to the failed production.
func TestExpressionDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"missing expression",
			"",
			[]Diagnostic{{ExpectedExpression, Span{0, 0}}},
			`File(Error())`,
		},
		{
			"missing expression after leading trivia",
			" # c\n",
			[]Diagnostic{{ExpectedExpression, Span{5, 5}}},
			`File(Error())`,
		},
		{
			"missing operand",
			"a +  ",
			[]Diagnostic{{ExpectedExpression, Span{5, 5}}},
			`File(Binary(Variable("a"), "+", Error()))`,
		},
		{
			"missing unary operand",
			"!",
			[]Diagnostic{{ExpectedExpression, Span{1, 1}}},
			`File(Unary("!", Error()))`,
		},
		{
			"operator without operand",
			"* a",
			[]Diagnostic{{ExpectedExpression, Span{0, 1}}, {UnexpectedToken, Span{2, 3}}},
			`File(Error("*"), Error("a"))`,
		},
		{
			"missing close paren",
			"(a + b",
			[]Diagnostic{{ExpectedClosingParen, Span{6, 6}}},
			`File(Paren("(", Binary(Variable("a"), "+", Variable("b"))))`,
		},
		{
			"empty parentheses",
			"()",
			[]Diagnostic{{ExpectedExpression, Span{1, 2}}},
			`File(Paren("(", Error(), ")"))`,
		},
		{
			"missing conditional colon",
			"a ? b",
			[]Diagnostic{{ExpectedConditionalColon, Span{5, 5}}},
			`File(Conditional(Variable("a"), "?", Variable("b")))`,
		},
		{
			"missing conditional arms",
			"a ? : ",
			[]Diagnostic{{ExpectedExpression, Span{4, 5}}, {ExpectedExpression, Span{6, 6}}},
			`File(Conditional(Variable("a"), "?", Error(), ":", Error()))`,
		},
		{
			"missing function name",
			"a::()",
			[]Diagnostic{{ExpectedFunctionName, Span{3, 4}}, {UnexpectedToken, Span{3, 4}}},
			`File(Call("a", "::"), Error("(", ")"))`,
		},
		{
			"missing function open paren",
			"a::b",
			[]Diagnostic{{ExpectedOpeningParen, Span{4, 4}}},
			`File(Call("a", "::", "b"))`,
		},
		{
			"missing function close paren",
			"f(a, b",
			[]Diagnostic{{ExpectedClosingParen, Span{6, 6}}},
			`File(Call("f", "(", Variable("a"), ",", Variable("b")))`,
		},
		{
			"missing argument separator",
			"f(1 2, 3)",
			[]Diagnostic{{ExpectedArgumentSeparator, Span{4, 5}}},
			`File(Call("f", "(", Literal("1"), Error("2"), ",", Literal("3"), ")"))`,
		},
		{
			"argument recovery keeps interior trivia out of the error",
			"f(1 /*c*/ 2 /*d*/ 3)",
			[]Diagnostic{{ExpectedArgumentSeparator, Span{10, 11}}},
			`File(Call("f", "(", Literal("1"), Error("2", "3"), ")"))`,
		},
		{
			"argument recovery stops at the bracket closer",
			"f(1 2]",
			[]Diagnostic{{ExpectedArgumentSeparator, Span{4, 5}}, {ExpectedClosingParen, Span{5, 6}}, {UnexpectedToken, Span{5, 6}}},
			`File(Call("f", "(", Literal("1"), Error("2")), Error("]"))`,
		},
		{
			"expanded argument must be final",
			"f(a..., b)",
			[]Diagnostic{{ExpectedClosingParen, Span{6, 7}}, {UnexpectedToken, Span{6, 7}}},
			`File(Call("f", "(", Variable("a"), "..."), Error(",", "b", ")"))`,
		},
		{
			"line break cannot continue unparenthesized binary",
			"a\n+ b",
			[]Diagnostic{{UnexpectedToken, Span{2, 3}}},
			`File(Variable("a"), Error("+", "b"))`,
		},
		{
			"trailing material",
			"a b",
			[]Diagnostic{{UnexpectedToken, Span{2, 3}}},
			`File(Variable("a"), Error("b"))`,
		},
		{
			"trailing material keeps trailing trivia at file level",
			"a b # c\n",
			[]Diagnostic{{UnexpectedToken, Span{2, 3}}},
			`File(Variable("a"), Error("b"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}

// assertDiagnosticsAndShape pins the exact diagnostics, including spans, and
// the recovered tree so that cascading errors and recovery regressions surface.
func assertDiagnosticsAndShape(t *testing.T, source string, diagnostics []Diagnostic, shape string) {
	t.Helper()
	file := parseExpressionSource([]byte(source))
	assertExpressionPartition(t, []byte(source), file)
	if !reflect.DeepEqual(file.diagnostics, diagnostics) {
		t.Errorf("diagnostics = %+v\nwant %+v", file.diagnostics, diagnostics)
	}
	if got := expressionShape(file, file.root.Element()); got != shape {
		t.Errorf("shape:\n%s\nwant:\n%s", got, shape)
	}
}

func TestUnterminatedExpressionLeavesTrailingTrivia(t *testing.T) {
	// Quoted and heredoc bodies absorb whitespace into TemplateText, so only
	// bracketed constructs can be followed by config-level trivia at EOF.
	for _, source := range []string{"[for ", "{for # c\n", "{for a in xs : a => [1,\n\n", "[for a in xs : \"x\" /* c */\n"} {
		t.Run(source, func(t *testing.T) {
			file := parseExpressionSource([]byte(source))
			assertExpressionPartition(t, []byte(source), file)
			if len(file.diagnostics) == 0 {
				t.Fatal("unterminated expression must have a diagnostic")
			}
			// The expression is the first child; trivia after its last real token
			// must follow it at File level, exactly as after a parenthesized error.
			node, _ := file.root.Child(0).Node()
			last, _ := node.Child(node.ChildCount() - 1).Token()
			if isTrivia(last.Kind()) || file.root.ChildCount() < 3 {
				t.Fatalf("trailing trivia absorbed: %v ends with %v, file has %d children", node.Kind(), last.Kind(), file.root.ChildCount())
			}
		})
	}
}

func TestExpressionRecoveryPreservesFollowingArgument(t *testing.T) {
	file := parseExpressionSource([]byte("f(1 2, 3)"))
	want := `File(Call("f", "(", Literal("1"), Error("2"), ",", Literal("3"), ")"))`
	if got := expressionShape(file, file.root.Element()); got != want {
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

// Inputs the fuzzer found interesting are checked in under testdata/fuzz/FuzzExpression
// and run as part of the ordinary test suite, alongside the seeds below.
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
		"[a\n+ b, [c],]",
		"[1 f(2, 3), 4]",
		"{a=1\nb=[2, {c:3}]}[key]",
		"{a=1 bad # next\n b=2}",
		"[for k,v in xs : k+v if v]",
		"{for v in xs : v.k => v... if v}",
		"[for x, : [1, 2]]",
		`"hello ${~ {a=1}.a ~} end"`,
		`"${a bad(}tail"`,
		"<<-END\n  ${a}\n  END\n",
		`"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`,
		`"%{if a}%{for x in xs}x%{else}y%{endif}"`,
		"\"%{if a #tail\n",
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

// assertExpressionPartition checks the invariants every expression tree must
// satisfy beyond byte-level losslessness: leaves are exactly the lexer's tokens,
// only File may begin or end with trivia, and a tree containing Error nodes has
// at least one diagnostic. Diagnostics without Error nodes remain legitimate,
// for example a missing closer or an unrepresentable number literal. Exact
// Error-to-diagnostic relationships belong to focused recovery tests.
func assertExpressionPartition(t *testing.T, source []byte, file syntaxFile) {
	t.Helper()
	assertFilePartition(t, source, file)
	var tokens []SyntaxToken
	errors := 0
	stack := []SyntaxElement{file.root.Element()}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if element, ok := current.Node(); ok {
			if element.Kind() == Error {
				errors++
			}
			count := element.ChildCount()
			if element.Kind() != File && count > 0 {
				if token, ok := element.Child(0).Token(); ok && isTrivia(token.Kind()) {
					t.Fatalf("%v begins with %v trivia: %s", element.Kind(), token.Kind(), expressionShape(file, element.Element()))
				}
				if token, ok := element.Child(count - 1).Token(); ok && isTrivia(token.Kind()) {
					t.Fatalf("%v ends with %v trivia: %s", element.Kind(), token.Kind(), expressionShape(file, element.Element()))
				}
			}
			for i := count - 1; i >= 0; i-- {
				stack = append(stack, element.Child(i))
			}
		} else if element, ok := current.Token(); ok {
			tokens = append(tokens, SyntaxToken{kind: element.Kind(), span: element.Span()})
		}
	}
	if want := lex(source).Tokens; !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tree leaves differ from lexer tokens:\n%+v\nwant:\n%+v", tokens, want)
	}
	if errors > 0 && len(file.diagnostics) == 0 {
		t.Fatalf("%d Error nodes without any diagnostic: %s", errors, expressionShape(file, file.root.Element()))
	}
}

// expressionShape omits trivia only for readable grammar assertions. Separate
// partition and placement assertions verify every token, including all trivia.
func expressionShape(file syntaxFile, current SyntaxElement) string {
	if element, ok := current.Token(); ok {
		if isTrivia(element.Kind()) || element.Kind() == EOF {
			return ""
		}
		span := element.Span()
		return strconv.Quote(file.source[span.Start:span.End])
	} else if element, ok := current.Node(); ok {
		names := map[NodeKind]string{
			File: "File", Error: "Error", LiteralExpression: "Literal",
			VariableExpression: "Variable", ParenthesizedExpression: "Paren",
			UnaryExpression: "Unary", BinaryExpression: "Binary",
			ConditionalExpression: "Conditional", FunctionCallExpression: "Call",
			TraversalExpression: "Traversal", AttributeAccess: "Attribute",
			IndexAccess: "Index", LegacyIndexAccess: "LegacyIndex",
			AttributeSplat: "AttributeSplat", FullSplat: "FullSplat",
			TupleExpression:  "Tuple",
			ObjectExpression: "Object", ObjectItem: "Item",
			ForExpression:      "For",
			TemplateExpression: "Template", TemplateInterpolation: "Interpolation",
			TemplateDirective: "Directive", TemplateIf: "TemplateIf", TemplateFor: "TemplateFor",
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
			"tuple recursion",
			func(count int) string { return strings.Repeat("[", count) + "a" + strings.Repeat("]", count) },
		},
		{
			"object recursion",
			func(count int) string { return strings.Repeat("{x=", count) + "a" + strings.Repeat("}", count) },
		},
		{
			"for-expression recursion",
			func(count int) string {
				return strings.Repeat("[for x in xs : ", count) + "a" + strings.Repeat("]", count)
			},
		},
		{
			"template interpolation recursion",
			func(count int) string { return strings.Repeat(`"${`, count) + "a" + strings.Repeat(`}"`, count) },
		},
		{
			"template directive condition recursion",
			func(count int) string {
				return strings.Repeat(`"%{if `, count) + "a" + strings.Repeat(`}%{endif}"`, count)
			},
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
