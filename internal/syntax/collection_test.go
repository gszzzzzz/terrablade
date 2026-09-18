package syntax

import "testing"

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
				{UnsupportedExpression, Span{0, 1}},
			},
			`File(Error("[", "for", ",", "x", "]"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}
