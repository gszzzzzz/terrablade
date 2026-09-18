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

func TestRecoveryKeepsNestedConstructsTogether(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"bracketed commas stay inside malformed argument",
			"f(1 bad[2,3],4)",
			[]Diagnostic{
				{ExpectedArgumentSeparator, Span{4, 7}},
			},
			`File(Call("f", "(", Literal("1"), Error("bad", "[", "2", ",", "3", "]"), ",", Literal("4"), ")"))`,
		},
		{
			"object newlines and commas stay inside malformed item",
			"{a=1 {b=2,\nc=3}, d=4}",
			[]Diagnostic{
				{ExpectedObjectItemSeparator, Span{5, 6}},
			},
			`File(Object("{", Item(Variable("a"), "=", Literal("1")), Error("{", "b", "=", "2", ",", "c", "=", "3", "}"), ",", Item(Variable("d"), "=", Literal("4")), "}"))`,
		},
		{
			"quoted interpolation keeps its nested call delimiters",
			`f(1 "${g(2,3)}",4)`,
			[]Diagnostic{
				{ExpectedArgumentSeparator, Span{4, 5}},
			},
			`File(Call("f", "(", Literal("1"), Error("\"", "${", "g", "(", "2", ",", "3", ")", "}", "\""), ",", Literal("4"), ")"))`,
		},
		{
			"quoted directives and literal commas remain together",
			`f(1 "%{if ok}a,b%{else}c%{endif}",4)`,
			[]Diagnostic{
				{ExpectedArgumentSeparator, Span{4, 5}},
			},
			`File(Call("f", "(", Literal("1"), Error("\"", "%{", "if", "ok", "}", "a,b", "%{", "else", "}", "c", "%{", "endif", "}", "\""), ",", Literal("4"), ")"))`,
		},
		{
			"heredoc with interpolation and directives keeps outer comma",
			"f(1 <<END\n${g(2,3)}\n%{if ok}x%{endif}\nEND\n,4)",
			[]Diagnostic{
				{ExpectedArgumentSeparator, Span{4, 6}},
			},
			`File(Call("f", "(", Literal("1"), Error("<<", "END", "${", "g", "(", "2", ",", "3", ")", "}", "\n", "%{", "if", "ok", "}", "x", "%{", "endif", "}", "\n", "END"), ",", Literal("4"), ")"))`,
		},
		{
			"heredoc nested inside quoted interpolation preserves all closers",
			"f(1 \"${<<END\nx,y\nEND\n}\",4)",
			[]Diagnostic{
				{ExpectedArgumentSeparator, Span{4, 5}},
			},
			`File(Call("f", "(", Literal("1"), Error("\"", "${", "<<", "END", "x,y\n", "END", "}", "\""), ",", Literal("4"), ")"))`,
		},
		{
			"for header recovery skips nested heredoc before its own closer",
			"f([for x xs <<END\nx,y\nEND\n], z)",
			[]Diagnostic{
				{ExpectedForIn, Span{9, 11}},
			},
			`File(Call("f", "(", For("[", "for", "x", Error("xs", "<<", "END", "x,y\n", "END"), "]"), ",", Variable("z"), ")"))`,
		},
		{
			"template tail recovery distinguishes inner and outer interpolation",
			`"${a "${g(2,3)}"}tail"`,
			[]Diagnostic{
				{ExpectedTemplateSequenceEnd, Span{5, 6}},
			},
			`File(Template("\"", Interpolation("${", Variable("a"), Error("\"", "${", "g", "(", "2", ",", "3", ")", "}", "\""), "}"), "tail", "\""))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}
