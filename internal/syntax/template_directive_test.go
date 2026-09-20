package syntax

import (
	"strings"
	"testing"
)

func TestTemplateDirectiveShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"empty if body has no synthetic literal",
			`"%{if a}%{endif}"`,
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), Directive("%{", "endif", "}")), "\""))`,
		},
		{
			"if else boundaries remain explicit",
			`"before %{if a}yes%{else}no%{endif} after"`,
			`File(Template("\"", "before ", TemplateIf(Directive("%{", "if", Variable("a"), "}"), "yes", Directive("%{", "else", "}"), "no", Directive("%{", "endif", "}")), " after", "\""))`,
		},
		{
			"for bindings and interpolation",
			`"%{for k, v in xs}${k}:${v}%{endfor}"`,
			`File(Template("\"", TemplateFor(Directive("%{", "for", "k", ",", "v", "in", Variable("xs"), "}"), Interpolation("${", Variable("k"), "}"), ":", Interpolation("${", Variable("v"), "}"), Directive("%{", "endfor", "}")), "\""))`,
		},
		{
			"nested scopes preserve pairing",
			`"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`,
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), TemplateFor(Directive("%{", "for", "x", "in", Variable("xs"), "}"), Interpolation("${", Variable("x"), "}"), Directive("%{", "endfor", "}")), Directive("%{", "else", "}"), "none", Directive("%{", "endif", "}")), "\""))`,
		},
		{
			"nested if restores the outer else target",
			`"%{if a}%{if b}x%{endif}%{else}y%{endif}"`,
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), TemplateIf(Directive("%{", "if", Variable("b"), "}"), "x", Directive("%{", "endif", "}")), Directive("%{", "else", "}"), "y", Directive("%{", "endif", "}")), "\""))`,
		},

		{
			"strip markers and literal whitespace are preserved",
			`" a %{~ if a ~} b %{~ else ~} c %{~ endif ~} d "`,
			`File(Template("\"", " a ", TemplateIf(Directive("%{", "~", "if", Variable("a"), "~", "}"), " b ", Directive("%{", "~", "else", "~", "}"), " c ", Directive("%{", "~", "endif", "~", "}")), " d ", "\""))`,
		},
		{
			"directive conditions allow multiline expressions",
			"\"%{if\n a # condition\n&& b\n}x%{endif}\"",
			`File(Template("\"", TemplateIf(Directive("%{", "if", Binary(Variable("a"), "&&", Variable("b")), "}"), "x", Directive("%{", "endif", "}")), "\""))`,
		},
		{
			"heredoc directives retain newlines as literal text",
			"<<END\n%{for x in xs}\n${x}\n%{endfor}\nEND\n",
			`File(Template("<<", "END", TemplateFor(Directive("%{", "for", "x", "in", Variable("xs"), "}"), "\n", Interpolation("${", Variable("x"), "}"), "\n", Directive("%{", "endfor", "}")), "\n", "END"))`,
		},
		{
			"interpolated template has independent directive scopes",
			`"%{if a}${"%{if b}x%{endif}"}%{endif}"`,
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), Interpolation("${", Template("\"", TemplateIf(Directive("%{", "if", Variable("b"), "}"), "x", Directive("%{", "endif", "}")), "\""), "}"), Directive("%{", "endif", "}")), "\""))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, nil, test.shape)
		})
	}
}

func TestTemplateDirectiveDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"missing keyword",
			`"%{ }"`,
			[]Diagnostic{
				{ExpectedTemplateDirective, Span{4, 5}},
			},
			`File(Template("\"", Directive("%{", "}"), "\""))`,
		},
		{
			"unknown keyword preserves following literal",
			`"a%{while x}b"`,
			[]Diagnostic{
				{UnknownTemplateDirective, Span{4, 9}},
			},
			`File(Template("\"", "a", Directive("%{", "while", Error("x"), "}"), "b", "\""))`,
		},
		{
			"missing condition still pairs with endif",
			`"%{if}a%{endif}"`,
			[]Diagnostic{
				{ExpectedExpression, Span{5, 6}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Error(), "}"), "a", Directive("%{", "endif", "}")), "\""))`,
		},
		{
			"missing for binding still pairs with endfor",
			`"%{for}a%{endfor}"`,
			[]Diagnostic{
				{ExpectedForVariable, Span{6, 7}},
			},
			`File(Template("\"", TemplateFor(Directive("%{", "for", "}"), "a", Directive("%{", "endfor", "}")), "\""))`,
		},
		{
			"missing in recovers to directive end",
			`"%{for x xs}a%{endfor}"`,
			[]Diagnostic{
				{ExpectedForIn, Span{9, 11}},
			},
			`File(Template("\"", TemplateFor(Directive("%{", "for", "x", Error("xs"), "}"), "a", Directive("%{", "endfor", "}")), "\""))`,
		},
		{
			"extra header characters",
			`"%{if a b}x%{endif}"`,
			[]Diagnostic{
				{ExpectedTemplateSequenceEnd, Span{8, 9}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), Error("b"), "}"), "x", Directive("%{", "endif", "}")), "\""))`,
		},
		{
			"else cannot carry an expression",
			`"%{if a}%{else b}%{endif}"`,
			[]Diagnostic{
				{ExpectedTemplateSequenceEnd, Span{15, 16}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), Directive("%{", "else", Error("b"), "}"), Directive("%{", "endif", "}")), "\""))`,
		},

		{
			"unmatched else",
			`"%{else}"`,
			[]Diagnostic{
				{UnexpectedTemplateDirective, Span{1, 8}},
			},
			`File(Template("\"", Directive("%{", "else", "}"), "\""))`,
		},
		{
			"duplicate else",
			`"%{if a}%{else}%{else}%{endif}"`,
			[]Diagnostic{
				{UnexpectedTemplateDirective, Span{15, 22}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), Directive("%{", "else", "}"), Directive("%{", "else", "}"), Directive("%{", "endif", "}")), "\""))`,
		},
		{
			"wrong ending cannot close an if",
			`"%{if a}x%{endfor}"`,
			[]Diagnostic{
				{UnexpectedTemplateDirective, Span{9, 18}},
				{ExpectedTemplateEndIf, Span{18, 19}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), "x", Directive("%{", "endfor", "}")), "\""))`,
		},
		{
			"outer ending survives an unfinished inner scope",
			`"%{if a}%{for x in xs}x%{endif}"`,
			[]Diagnostic{
				{ExpectedTemplateEndFor, Span{23, 31}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), TemplateFor(Directive("%{", "for", "x", "in", Variable("xs"), "}"), "x"), Directive("%{", "endif", "}")), "\""))`,
		},
		{
			"outer else survives an unfinished inner scope",
			`"%{if a}%{for x in xs}x%{else}y%{endif}"`,
			[]Diagnostic{
				{ExpectedTemplateEndFor, Span{23, 30}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), TemplateFor(Directive("%{", "for", "x", "in", Variable("xs"), "}"), "x"), Directive("%{", "else", "}"), "y", Directive("%{", "endif", "}")), "\""))`,
		},

		{
			"missing endif preserves quote closer",
			`"%{if a}x"`,
			[]Diagnostic{
				{ExpectedTemplateEndIf, Span{9, 10}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a"), "}"), "x"), "\""))`,
		},
		{
			"missing endfor preserves empty body",
			`"%{for x in xs}"`,
			[]Diagnostic{
				{ExpectedTemplateEndFor, Span{15, 16}},
			},
			`File(Template("\"", TemplateFor(Directive("%{", "for", "x", "in", Variable("xs"), "}")), "\""))`,
		},
		{
			"unterminated header leaves trailing trivia outside",
			"\"%{if a #tail\n",
			[]Diagnostic{
				{UnterminatedQuotedTemplate, Span{0, 1}},
				{UnterminatedTemplateSequence, Span{1, 3}},
				{ExpectedTemplateEndIf, Span{14, 14}},
			},
			`File(Template("\"", TemplateIf(Directive("%{", "if", Variable("a")))))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}

func TestTemplateDirectiveNestingIsIterative(t *testing.T) {
	const depth = maxRecursiveExpressionDepth * 8
	for _, test := range []struct {
		name, source string
	}{
		{
			"nested if",
			`"` + strings.Repeat("%{if a}", depth) + "x" + strings.Repeat("%{endif}", depth) + `"`,
		},
		{
			"alternating if and for",
			`"` + strings.Repeat("%{if a}%{for x in xs}", depth) + "x" + strings.Repeat("%{endfor}%{endif}", depth) + `"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertTreeInvariants(t, []byte(test.source), file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("iterative directive nesting rejected: %+v", file.diagnostics)
			}
		})
	}
}

func TestUnmatchedDirectiveEndingsPreserveOpenScopes(t *testing.T) {
	const depth = maxRecursiveExpressionDepth * 8
	source := `"` + strings.Repeat("%{if a}", depth) + strings.Repeat("%{endfor}", depth) + strings.Repeat("%{endif}", depth) + `"`
	file := parseExpressionSource([]byte(source))
	assertTreeInvariants(t, []byte(source), file)
	if len(file.diagnostics) != depth {
		t.Fatalf("got %d diagnostics, want one for each unmatched endfor", len(file.diagnostics))
	}
	for _, diagnostic := range file.diagnostics {
		if diagnostic.Kind != UnexpectedTemplateDirective {
			t.Fatalf("unmatched endfor changed surrounding if pairing: %+v", diagnostic)
		}
	}
}
