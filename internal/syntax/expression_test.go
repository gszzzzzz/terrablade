package syntax

import (
	"bytes"
	"reflect"
	"slices"
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
			`File(Traversal(Call("provider", "::", "aws", "::", "f", "(", Variable("x"), ")"), AttrAccess(".", "id")))`,
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
			assertTreeInvariants(t, []byte(test.source), file)
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

func TestUnterminatedExpressionLeavesTrailingTrivia(t *testing.T) {
	// Quoted and heredoc bodies absorb whitespace into TemplateText, so only
	// bracketed constructs can be followed by config-level trivia at EOF.
	for _, source := range []string{"[for ", "{for # c\n", "{for a in xs : a => [1,\n\n", "[for a in xs : \"x\" /* c */\n"} {
		t.Run(source, func(t *testing.T) {
			file := parseExpressionSource([]byte(source))
			assertTreeInvariants(t, []byte(source), file)
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
	assertTreeInvariants(t, source, file)
	for _, diagnostic := range lex(source).Diagnostics {
		if !slices.Contains(file.diagnostics, diagnostic) {
			t.Fatalf("lost lexical diagnostic %+v", diagnostic)
		}
	}
}

// A discovered regression belongs in testdata/fuzz/FuzzExpression, where the ordinary
// test suite runs it alongside the seeds below. See that directory's README for
// what earns a checked-in entry.
func FuzzExpression(f *testing.F) {
	for _, source := range []string{
		"",
		"f(1 2, 3)",
		"f(1 g(2,3),4)",
		`"${f(a}"`,
		"f(1 <<END\n${g(2,3)}\n%{if ok}x%{endif}\nEND\n,4)",
		"f(1 \"${<<END\nx,y\nEND\n}\",4)",
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
		// Leave the fuzzer-owned input unchanged so persisted discoveries keep
		// their original bytes; only our caller-owned clone may be cleared.
		input := bytes.Clone(source)
		file := parseExpressionSource(input)
		assertTreeInvariants(t, source, file)
		if !bytes.Equal(input, source) {
			t.Fatal("parser mutated source")
		}
		if second := parseExpressionSource(input); !reflect.DeepEqual(file, second) {
			t.Fatal("parser is not deterministic")
		}
		clear(input)
		assertTreeInvariants(t, source, file)
	})
}

func TestShapeNodeNames(t *testing.T) {
	seen := make(map[string]NodeKind)
	for kind := File; kind < nodeKindCount; kind++ {
		name := shapeNodeNames[kind]
		if name == "" {
			t.Errorf("missing shape name for %v", kind)
		} else if previous, duplicate := seen[name]; duplicate {
			t.Errorf("%v and %v share shape name %q", previous, kind, name)
		}
		seen[name] = kind
	}
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
			assertTreeInvariants(t, []byte(test.source), file)
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
			assertTreeInvariants(t, accepted, file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("boundary input rejected: %+v", file.diagnostics)
			}
			for _, count := range []int{maxRecursiveExpressionDepth, maxRecursiveExpressionDepth * 8} {
				source := []byte(test.make(count))
				file = parseExpressionSource(source)
				assertTreeInvariants(t, source, file)
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
	assertTreeInvariants(t, source, file)
	if len(file.diagnostics) != 0 {
		t.Fatalf("flat binary chain rejected: %+v", file.diagnostics)
	}
}
