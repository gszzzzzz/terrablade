package syntax

import "testing"

func TestCallRecoveryBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"nested call comma is not an outer argument separator",
			"f(1 g(2,3),4)",
			[]Diagnostic{
				{ExpectedArgumentSeparator, Span{4, 5}},
			},
			`File(Call("f", "(", Literal("1"), Error("g", "(", "2", ",", "3", ")"), ",", Literal("4"), ")"))`,
		},
		{
			"missing call closer leaves interpolation closer intact",
			`"${f(a}"`,
			[]Diagnostic{
				{ExpectedClosingParen, Span{6, 7}},
			},
			`File(Template("\"", Interpolation("${", Call("f", "(", Variable("a")), "}"), "\""))`,
		},
		{
			"argument recovery preserves strip marker and interpolation closer",
			`"${f(a b ~} tail"`,
			[]Diagnostic{
				{ExpectedArgumentSeparator, Span{7, 8}},
				{ExpectedClosingParen, Span{9, 10}},
			},
			`File(Template("\"", Interpolation("${", Call("f", "(", Variable("a"), Error("b")), "~", "}"), " tail", "\""))`,
		},
		{
			"missing call closer preserves heredoc body and marker",
			"<<END\n${f(a}\ntail\nEND\n",
			[]Diagnostic{
				{ExpectedClosingParen, Span{11, 12}},
			},
			`File(Template("<<", "END", Interpolation("${", Call("f", "(", Variable("a")), "}"), "\ntail\n", "END"))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}
