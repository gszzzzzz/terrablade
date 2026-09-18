package syntax

import "testing"

func TestForExpressionShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"tuple projection",
			"[for x in xs : x.id]",
			`File(For("[", "for", "x", "in", Variable("xs"), ":", Traversal(Variable("x"), AttrAccess(".", "id")), "]"))`,
		},
		{
			"tuple key binding and condition",
			"[for k, v in xs : k + v if v != null]",
			`File(For("[", "for", "k", ",", "v", "in", Variable("xs"), ":", Binary(Variable("k"), "+", Variable("v")), "if", Binary(Variable("v"), "!=", Literal("null")), "]"))`,
		},
		{
			"object grouping and condition",
			"{for k, v in xs : k => v... if v}",
			`File(For("{", "for", "k", ",", "v", "in", Variable("xs"), ":", Variable("k"), "=>", Variable("v"), "...", "if", Variable("v"), "}"))`,
		},
		{
			"newlines are transparent throughout",
			"{\nfor k,\nv in\nxs\n:\nk\n=>\nv\n...\nif\nv\n}",
			`File(For("{", "for", "k", ",", "v", "in", Variable("xs"), ":", Variable("k"), "=>", Variable("v"), "...", "if", Variable("v"), "}"))`,
		},
		{
			"nested projection with postfix",
			"[for x in [a] : {x=x}][0]",
			`File(Traversal(For("[", "for", "x", "in", Tuple("[", Variable("a"), "]"), ":", Object("{", Item(Variable("x"), "=", Variable("x")), "}"), "]"), Index("[", Literal("0"), "]")))`,
		},
		{
			"conditional collection and result",
			"[for x in a ? b : c : x ? y : z]",
			`File(For("[", "for", "x", "in", Conditional(Variable("a"), "?", Variable("b"), ":", Variable("c")), ":", Conditional(Variable("x"), "?", Variable("y"), ":", Variable("z")), "]"))`,
		},
		{
			"keywords are contextual bindings and expressions",
			"[for in in if : if if in]",
			`File(For("[", "for", "in", "in", Variable("if"), ":", Variable("if"), "if", Variable("in"), "]"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, nil, test.shape)
		})
	}
}

func TestForExpressionDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"missing binding",
			"[for : x]",
			[]Diagnostic{
				{ExpectedForVariable, Span{5, 6}},
			},
			`File(For("[", "for", Error(":", "x"), "]"))`,
		},
		{
			"missing second binding",
			"[for x, : x]",
			[]Diagnostic{
				{ExpectedForVariable, Span{8, 9}},
			},
			`File(For("[", "for", "x", ",", Error(":", "x"), "]"))`,
		},
		{
			"missing in",
			"[for x xs : x]",
			[]Diagnostic{
				{ExpectedForIn, Span{7, 9}},
			},
			`File(For("[", "for", "x", Error("xs", ":", "x"), "]"))`,
		},
		{
			"missing collection",
			"[for x in : x]",
			[]Diagnostic{
				{ExpectedExpression, Span{10, 11}},
			},
			`File(For("[", "for", "x", "in", Error(), ":", Variable("x"), "]"))`,
		},
		{
			"missing colon",
			"[for x in xs x]",
			[]Diagnostic{
				{ExpectedForColon, Span{13, 14}},
			},
			`File(For("[", "for", "x", "in", Variable("xs"), Error("x"), "]"))`,
		},
		{
			"missing projection",
			"[for x in xs : ]",
			[]Diagnostic{
				{ExpectedExpression, Span{15, 16}},
			},
			`File(For("[", "for", "x", "in", Variable("xs"), ":", Error(), "]"))`,
		},
		{
			"object requires key arrow",
			"{for x in xs : x}",
			[]Diagnostic{
				{ExpectedForArrow, Span{16, 17}},
			},
			`File(For("{", "for", "x", "in", Variable("xs"), ":", Variable("x"), "}"))`,
		},
		{
			"tuple rejects key and grouping",
			"[for x in xs : x => x...]",
			[]Diagnostic{
				{UnexpectedForKey, Span{17, 19}},
				{UnexpectedForGrouping, Span{21, 24}},
			},
			`File(For("[", "for", "x", "in", Variable("xs"), ":", Variable("x"), "=>", Variable("x"), "...", "]"))`,
		},
		{
			"missing grouped value",
			"{for x in xs : x => ...}",
			[]Diagnostic{
				{ExpectedExpression, Span{20, 23}},
			},
			`File(For("{", "for", "x", "in", Variable("xs"), ":", Variable("x"), "=>", Error(), "...", "}"))`,
		},
		{
			"missing condition",
			"[for x in xs : x if]",
			[]Diagnostic{
				{ExpectedExpression, Span{19, 20}},
			},
			`File(For("[", "for", "x", "in", Variable("xs"), ":", Variable("x"), "if", Error(), "]"))`,
		},
		{
			"trailing comma is not a collection separator",
			"f([for x in xs : x, y], z)",
			[]Diagnostic{
				{ExpectedClosingBracket, Span{18, 19}},
			},
			`File(Call("f", "(", For("[", "for", "x", "in", Variable("xs"), ":", Variable("x"), Error(",", "y"), "]"), ",", Variable("z"), ")"))`,
		},
		{
			"outer closer survives a malformed header",
			"f([for])",
			[]Diagnostic{
				{ExpectedForVariable, Span{6, 7}},
			},
			`File(Call("f", "(", For("[", "for", "]"), ")"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}
