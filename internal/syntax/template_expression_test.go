package syntax

import "testing"

func TestTemplateExpressionShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"empty quoted template has no synthetic body",
			`""`,
			`File(Template("\"", "\""))`,
		},
		{
			"literal spelling and escaped introducers stay raw",
			`"a\n\u0041$${x}%%{if}"`,
			`File(Template("\"", "a\\n\\u0041$${x}%%{if}", "\""))`,
		},
		{
			"interpolation separates literal regions",
			`"hello ${a} end"`,
			`File(Template("\"", "hello ", Interpolation("${", Variable("a"), "}"), " end", "\""))`,
		},
		{
			"strip markers never trim source",
			`" before ${~ a ~} after "`,
			`File(Template("\"", " before ", Interpolation("${", "~", Variable("a"), "~", "}"), " after ", "\""))`,
		},
		{
			"quoted interpolation accepts multiline expressions",
			"\"${\n1 # c\n+ 2\n}\"",
			`File(Template("\"", Interpolation("${", Binary(Literal("1"), "+", Literal("2")), "}"), "\""))`,
		},
		{
			"nested quoted template",
			`"${"inner ${a}"}"`,
			`File(Template("\"", Interpolation("${", Template("\"", "inner ", Interpolation("${", Variable("a"), "}"), "\""), "}"), "\""))`,
		},
		{
			"object and quoted key inside interpolation",
			`"${{"key"=v}.key}"`,
			`File(Template("\"", Interpolation("${", Traversal(Object("{", Item(Template("\"", "key", "\""), "=", Variable("v")), "}"), AttrAccess(".", "key")), "}"), "\""))`,
		},
		{
			"empty heredoc",
			"<<END\nEND\n",
			`File(Template("<<", "END", "END"))`,
		},
		{
			"indented heredoc preserves CRLF and closing whitespace",
			"<<-END\r\n  a\\n\r\n  END \t\r\n",
			`File(Template("<<-", "END", "  a\\n\r\n", "  END \t"))`,
		},
		{
			"heredoc interpolation",
			"<<END\nx ${a} y\nEND\n",
			`File(Template("<<", "END", "x ", Interpolation("${", Variable("a"), "}"), " y\n", "END"))`,
		},
		{
			"heredoc inside a call",
			"f(<<END\na\nEND\n, 1)",
			`File(Call("f", "(", Template("<<", "END", "a\n", "END"), ",", Literal("1"), ")"))`,
		},
		{
			"literal is still an ordinary expression term",
			`["x"][0] == "x"`,
			`File(Binary(Traversal(Tuple("[", Template("\"", "x", "\""), "]"), Index("[", Literal("0"), "]")), "==", Template("\"", "x", "\"")))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, nil, test.shape)
		})
	}
}

func TestTemplateExpressionDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"missing interpolation expression",
			`"${}"`,
			[]Diagnostic{
				{ExpectedExpression, Span{3, 4}},
			},
			`File(Template("\"", Interpolation("${", Error(), "}"), "\""))`,
		},
		{
			"strip marker without expression",
			`"${~ ~}"`,
			[]Diagnostic{
				{ExpectedExpression, Span{5, 6}},
			},
			`File(Template("\"", Interpolation("${", "~", Error(), "~", "}"), "\""))`,
		},
		{
			"extra interpolation tokens do not consume literal tail",
			`"${a b} tail ${c}"`,
			[]Diagnostic{
				{ExpectedTemplateSequenceEnd, Span{5, 6}},
			},
			`File(Template("\"", Interpolation("${", Variable("a"), Error("b"), "}"), " tail ", Interpolation("${", Variable("c"), "}"), "\""))`,
		},
		{
			"stray closer inside interpolation",
			`"${a )}tail"`,
			[]Diagnostic{
				{ExpectedTemplateSequenceEnd, Span{5, 6}},
			},
			`File(Template("\"", Interpolation("${", Variable("a"), Error(")"), "}"), "tail", "\""))`,
		},
		{
			"unfinished nested recovery preserves interpolation closer",
			`"${a bad(}tail"`,
			[]Diagnostic{
				{ExpectedTemplateSequenceEnd, Span{5, 8}},
			},
			`File(Template("\"", Interpolation("${", Variable("a"), Error("bad", "("), "}"), "tail", "\""))`,
		},
		{
			"missing nested expression closer",
			`"${[a}"`,
			[]Diagnostic{
				{ExpectedClosingBracket, Span{5, 6}},
			},
			`File(Template("\"", Interpolation("${", Tuple("[", Variable("a")), "}"), "\""))`,
		},
		{
			"unterminated quote is diagnosed by lexer",
			`"hello`,
			[]Diagnostic{
				{UnterminatedQuotedTemplate, Span{0, 1}},
			},
			`File(Template("\"", "hello"))`,
		},
		{
			"unfinished interpolation leaves trivia outside",
			"\"${a # tail\n",
			[]Diagnostic{
				{UnterminatedQuotedTemplate, Span{0, 1}},
				{UnterminatedTemplateSequence, Span{1, 3}},
			},
			`File(Template("\"", Interpolation("${", Variable("a"))))`,
		},
		{
			"unfinished heredoc header leaves newline outside",
			"<<END\n",
			[]Diagnostic{
				{UnterminatedHeredoc, Span{0, 2}},
			},
			`File(Template("<<", "END"))`,
		},
		{
			"unfinished heredoc text remains literal",
			"<<-END\n  a\n",
			[]Diagnostic{
				{UnterminatedHeredoc, Span{0, 3}},
			},
			`File(Template("<<-", "END", "  a\n"))`,
		},
		{
			"invalid escape stays in literal token",
			`"\q"`,
			[]Diagnostic{
				{InvalidEscape, Span{1, 2}},
			},
			`File(Template("\"", "\\q", "\""))`,
		},
		{
			"literal newline is not interpolation whitespace",
			"\"a\nb\"",
			[]Diagnostic{
				{NewlineInQuotedTemplate, Span{2, 3}},
			},
			`File(Template("\"", "a\nb", "\""))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}
