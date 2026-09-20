package syntax

import (
	"strings"
	"testing"
)

func TestTupleShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"empty",
			"[]",
			`File(Tuple("[", "]"))`,
		},
		{
			"empty with trivia",
			"[ # empty\n ]",
			`File(Tuple("[", "]"))`,
		},
		{
			"elements and trailing comma",
			"[1, a + b,]",
			`File(Tuple("[", Literal("1"), ",", Binary(Variable("a"), "+", Variable("b")), ",", "]"))`,
		},
		{
			"newlines continue an element",
			"[a\n+ b, # next\n c\n]",
			`File(Tuple("[", Binary(Variable("a"), "+", Variable("b")), ",", Variable("c"), "]"))`,
		},

		{
			"nested tuple and postfix",
			"[[a], []][0]",
			`File(Traversal(Tuple("[", Tuple("[", Variable("a"), "]"), ",", Tuple("[", "]"), "]"), Index("[", Literal("0"), "]")))`,
		},
		{
			"tuple as function argument",
			"f([a, b], c)",
			`File(Call("f", "(", Tuple("[", Variable("a"), ",", Variable("b"), "]"), ",", Variable("c"), ")"))`,
		},

		{
			"parentheses disambiguate first for",
			"[(for), for]",
			`File(Tuple("[", Paren("(", Variable("for"), ")"), ",", Variable("for"), "]"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, nil, test.shape)
		})
	}
}

func TestTupleDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"newline cannot replace comma",
			"[a\nb]",
			[]Diagnostic{
				{ExpectedTupleSeparator, Span{3, 4}},
			},
			`File(Tuple("[", Variable("a"), Error("b"), "]"))`,
		},
		{
			"missing separator preserves following element",
			"[1 2, 3]",
			[]Diagnostic{
				{ExpectedTupleSeparator, Span{3, 4}},
			},
			`File(Tuple("[", Literal("1"), Error("2"), ",", Literal("3"), "]"))`,
		},
		{
			"recovery skips nested commas",
			"[1 f(2, 3), 4]",
			[]Diagnostic{
				{ExpectedTupleSeparator, Span{3, 4}},
			},
			`File(Tuple("[", Literal("1"), Error("f", "(", "2", ",", "3", ")"), ",", Literal("4"), "]"))`,
		},
		{
			"missing first element",
			"[, 1]",
			[]Diagnostic{
				{ExpectedExpression, Span{1, 2}},
			},
			`File(Tuple("[", Error(), ",", Literal("1"), "]"))`,
		},
		{
			"repeated separator",
			"[1,,2]",
			[]Diagnostic{
				{ExpectedExpression, Span{3, 4}},
			},
			`File(Tuple("[", Literal("1"), ",", Error(), ",", Literal("2"), "]"))`,
		},
		{
			"missing operand",
			"[1 +, 2]",
			[]Diagnostic{
				{ExpectedExpression, Span{4, 5}},
			},
			`File(Tuple("[", Binary(Literal("1"), "+", Error()), ",", Literal("2"), "]"))`,
		},

		{
			"missing closer after trailing trivia",
			"[1, # tail\n",
			[]Diagnostic{
				{ExpectedClosingBracket, Span{11, 11}},
			},
			`File(Tuple("[", Literal("1"), ","))`,
		},
		{
			"missing closer before any element",
			"[ # tail\n",
			[]Diagnostic{
				{ExpectedClosingBracket, Span{9, 9}},
			},
			`File(Tuple("["))`,
		},
		{
			"foreign closer belongs to caller",
			"f([1)",
			[]Diagnostic{
				{ExpectedClosingBracket, Span{4, 5}},
			},
			`File(Call("f", "(", Tuple("[", Literal("1")), ")"))`,
		},

		{
			"expansion is not tuple syntax",
			"[xs...]",
			[]Diagnostic{
				{ExpectedTupleSeparator, Span{3, 6}},
			},
			`File(Tuple("[", Variable("xs"), Error("..."), "]"))`,
		},
		{
			"initial for stays reserved after trivia",
			"[ # lead\n for, x]",
			[]Diagnostic{
				{ExpectedForVariable, Span{13, 14}},
			},
			`File(For("[", "for", Error(",", "x"), "]"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}

func TestObjectShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"empty with trivia",
			"{ # empty\n }",
			`File(Object("{", "}"))`,
		},
		{
			"equals and colon separators",
			"{a = 1, b: 2,}",
			`File(Object("{", Item(Variable("a"), "=", Literal("1")), ",", Item(Variable("b"), ":", Literal("2")), ",", "}"))`,
		},
		{
			"line comments separate items",
			"{a=1 # next\n b=2 // next\r\n c=3\n}",
			`File(Object("{", Item(Variable("a"), "=", Literal("1")), Item(Variable("b"), "=", Literal("2")), Item(Variable("c"), "=", Literal("3")), "}"))`,
		},
		{
			"object newlines remain separators inside a call",
			"f({a=1\nb=2})",
			`File(Call("f", "(", Object("{", Item(Variable("a"), "=", Literal("1")), Item(Variable("b"), "=", Literal("2")), "}"), ")"))`,
		},

		{
			"quoted keys preserve template syntax",
			`{"key"=1}`,
			`File(Object("{", Item(Template("\"", "key", "\""), "=", Literal("1")), "}"))`,
		},
		{
			"parenthesized key is explicit",
			"{(key)=value}",
			`File(Object("{", Item(Paren("(", Variable("key"), ")"), "=", Variable("value")), "}"))`,
		},
		{
			"keywords and numeric keys preserve syntax",
			"{true=1, null=2, 3=4}",
			`File(Object("{", Item(Literal("true"), "=", Literal("1")), ",", Item(Literal("null"), "=", Literal("2")), ",", Item(Literal("3"), "=", Literal("4")), "}"))`,
		},
		{
			"conditional key before colon separator",
			"{a ? b : c : d}",
			`File(Object("{", Item(Conditional(Variable("a"), "?", Variable("b"), ":", Variable("c")), ":", Variable("d")), "}"))`,
		},

		{
			"parentheses allow multiline values",
			"{a=(1\n+2)\nb=3}",
			`File(Object("{", Item(Variable("a"), "=", Paren("(", Binary(Literal("1"), "+", Literal("2")), ")")), Item(Variable("b"), "=", Literal("3")), "}"))`,
		},
		{
			"multiline block comment is inline whitespace",
			"{a=1 /*\n*/ +2}",
			`File(Object("{", Item(Variable("a"), "=", Binary(Literal("1"), "+", Literal("2"))), "}"))`,
		},

		{
			"nested collections and postfix",
			"{a=[1, {b=2}]}[key]",
			`File(Traversal(Object("{", Item(Variable("a"), "=", Tuple("[", Literal("1"), ",", Object("{", Item(Variable("b"), "=", Literal("2")), "}"), "]")), "}"), Index("[", Variable("key"), "]")))`,
		},
		{
			"for is reserved only in the first position",
			"{(for)=1, for=2}",
			`File(Object("{", Item(Paren("(", Variable("for"), ")"), "=", Literal("1")), ",", Item(Variable("for"), "=", Literal("2")), "}"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, nil, test.shape)
		})
	}
}

func TestObjectDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"missing key value separator",
			"{a 1, b=2}",
			[]Diagnostic{
				{ExpectedObjectValueSeparator, Span{3, 4}},
			},
			`File(Object("{", Item(Variable("a")), Error("1"), ",", Item(Variable("b"), "=", Literal("2")), "}"))`,
		},
		{
			"missing item separator",
			"{a=1 b=2, c=3}",
			[]Diagnostic{
				{ExpectedObjectItemSeparator, Span{5, 6}},
			},
			`File(Object("{", Item(Variable("a"), "=", Literal("1")), Error("b", "=", "2"), ",", Item(Variable("c"), "=", Literal("3")), "}"))`,
		},
		{
			"block comment is not an item separator",
			"{a=1 /*\n*/ b=2}",
			[]Diagnostic{
				{ExpectedObjectItemSeparator, Span{11, 12}},
			},
			`File(Object("{", Item(Variable("a"), "=", Literal("1")), Error("b", "=", "2"), "}"))`,
		},
		{
			"nested recovery keeps inner separators",
			"{a=1 f(2, 3), b=4}",
			[]Diagnostic{
				{ExpectedObjectItemSeparator, Span{5, 6}},
			},
			`File(Object("{", Item(Variable("a"), "=", Literal("1")), Error("f", "(", "2", ",", "3", ")"), ",", Item(Variable("b"), "=", Literal("4")), "}"))`,
		},
		{
			"recovery resumes after line comment",
			"{a=1 bad # next\n b=2}",
			[]Diagnostic{
				{ExpectedObjectItemSeparator, Span{5, 8}},
			},
			`File(Object("{", Item(Variable("a"), "=", Literal("1")), Error("bad"), Item(Variable("b"), "=", Literal("2")), "}"))`,
		},

		{
			"missing value before comma",
			"{a=, b=2}",
			[]Diagnostic{
				{ExpectedExpression, Span{3, 4}},
			},
			`File(Object("{", Item(Variable("a"), "=", Error()), ",", Item(Variable("b"), "=", Literal("2")), "}"))`,
		},
		{
			"newline cannot introduce value",
			"{a=\nb=2}",
			[]Diagnostic{
				{ExpectedExpression, Span{3, 4}},
			},
			`File(Object("{", Item(Variable("a"), "=", Error()), Item(Variable("b"), "=", Literal("2")), "}"))`,
		},
		{
			"missing value separator before newline",
			"{a\nb=2}",
			[]Diagnostic{
				{ExpectedObjectValueSeparator, Span{2, 3}},
			},
			`File(Object("{", Item(Variable("a")), Item(Variable("b"), "=", Literal("2")), "}"))`,
		},
		{
			"missing value before closer",
			"{a=}",
			[]Diagnostic{
				{ExpectedExpression, Span{3, 4}},
			},
			`File(Object("{", Item(Variable("a"), "=", Error()), "}"))`,
		},

		{
			"missing closer after item",
			"{a=1 # tail\n",
			[]Diagnostic{
				{ExpectedClosingBrace, Span{12, 12}},
			},
			`File(Object("{", Item(Variable("a"), "=", Literal("1"))))`,
		},
		{
			"missing closer before any item",
			"{ # tail\n",
			[]Diagnostic{
				{ExpectedClosingBrace, Span{9, 9}},
			},
			`File(Object("{"))`,
		},
		{
			"missing nested closers",
			"{a=[1,\n\n",
			[]Diagnostic{
				{ExpectedClosingBracket, Span{8, 8}},
				{ExpectedClosingBrace, Span{8, 8}},
			},
			`File(Object("{", Item(Variable("a"), "=", Tuple("[", Literal("1"), ","))))`,
		},
		{
			"foreign closer belongs to caller",
			"[{a=1]",
			[]Diagnostic{
				{ExpectedClosingBrace, Span{5, 6}},
			},
			`File(Tuple("[", Object("{", Item(Variable("a"), "=", Literal("1"))), "]"))`,
		},

		{
			"first for stays reserved after trivia",
			"{ # lead\n for=1}",
			[]Diagnostic{
				{ExpectedForVariable, Span{13, 14}},
			},
			`File(For("{", "for", Error("=", "1"), "}"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}

func TestFlatCollectionsDoNotHitRecursionLimit(t *testing.T) {
	const items = maxRecursiveExpressionDepth * 8
	for _, test := range []struct {
		name, source string
	}{
		{
			"tuple",
			"[" + strings.Repeat("a,", items) + "]",
		},
		{
			"object",
			"{" + strings.Repeat("a=1,", items) + "}",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertTreeInvariants(t, []byte(test.source), file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("flat collection rejected: %+v", file.diagnostics)
			}
		})
	}
}
