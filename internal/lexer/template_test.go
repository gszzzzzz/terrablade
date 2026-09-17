package lexer

import (
	"reflect"
	"strings"
	"testing"
)

func TestQuotedTemplates(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []tokenText
	}{
		{"empty", `""`, []tokenText{{QuoteOpen, `"`}, {QuoteClose, `"`}}},
		{
			"literal",
			"\"# // /* \uFEFF 😀\"",
			[]tokenText{
				{QuoteOpen, `"`},
				{TemplateText, "# // /* \uFEFF 😀"},
				{QuoteClose, `"`},
			},
		},
		{
			"escapes",
			`"\n\r\t\"\\\u03A9\U0001f600"`,
			[]tokenText{
				{QuoteOpen, `"`},
				{TemplateText, `\n\r\t\"\\\u03A9\U0001f600`},
				{QuoteClose, `"`},
			},
		},
		{
			"escaped introducers",
			`"$${x} %%{if} $$${x} %%%{if}"`,
			[]tokenText{
				{QuoteOpen, `"`},
				{TemplateText, `$${x} %%{if} $$${x} %%%{if}`},
				{QuoteClose, `"`},
			},
		},
		{
			"interpolation",
			`"a${~ x ~}b"`,
			[]tokenText{
				{QuoteOpen, `"`},
				{TemplateText, "a"},
				{InterpolationOpen, "${"},
				{StripMarker, "~"},
				{Whitespace, " "},
				{Identifier, "x"},
				{Whitespace, " "},
				{StripMarker, "~"},
				{TemplateSequenceEnd, "}"},
				{TemplateText, "b"},
				{QuoteClose, `"`},
			},
		},
		{
			"nested object",
			`"${{a={b=1}}}"`,
			[]tokenText{
				{QuoteOpen, `"`},
				{InterpolationOpen, "${"},
				{OpenBrace, "{"},
				{Identifier, "a"},
				{Equal, "="},
				{OpenBrace, "{"},
				{Identifier, "b"},
				{Equal, "="},
				{Number, "1"},
				{CloseBrace, "}"},
				{CloseBrace, "}"},
				{TemplateSequenceEnd, "}"},
				{QuoteClose, `"`},
			},
		},
		{
			"nested quoted",
			`"${"${x}"}"`,
			[]tokenText{
				{QuoteOpen, `"`},
				{InterpolationOpen, "${"},
				{QuoteOpen, `"`},
				{InterpolationOpen, "${"},
				{Identifier, "x"},
				{TemplateSequenceEnd, "}"},
				{QuoteClose, `"`},
				{TemplateSequenceEnd, "}"},
				{QuoteClose, `"`},
			},
		},
		{
			"directives",
			`"%{~if x}y%{endif~}"`,
			[]tokenText{
				{QuoteOpen, `"`},
				{DirectiveOpen, "%{"},
				{StripMarker, "~"},
				{Identifier, "if"},
				{Whitespace, " "},
				{Identifier, "x"},
				{TemplateSequenceEnd, "}"},
				{TemplateText, "y"},
				{DirectiveOpen, "%{"},
				{Identifier, "endif"},
				{StripMarker, "~"},
				{TemplateSequenceEnd, "}"},
				{QuoteClose, `"`},
			},
		},
		{
			"expression comments",
			"\"${/* } */x# }\r\n}\"",
			[]tokenText{
				{QuoteOpen, `"`},
				{InterpolationOpen, "${"},
				{BlockComment, "/* } */"},
				{Identifier, "x"},
				{LineComment, "# }"},
				{Newline, "\r\n"},
				{TemplateSequenceEnd, "}"},
				{QuoteClose, `"`},
			},
		},
		{
			"expression BOM",
			"\"\uFEFF${\uFEFFx}\"",
			[]tokenText{
				{QuoteOpen, `"`},
				{TemplateText, "\uFEFF"},
				{InterpolationOpen, "${"},
				{BOM, "\uFEFF"},
				{Identifier, "x"},
				{TemplateSequenceEnd, "}"},
				{QuoteClose, `"`},
			},
		},
		{
			"returns to config",
			`"x"+2`,
			[]tokenText{
				{QuoteOpen, `"`},
				{TemplateText, "x"},
				{QuoteClose, `"`},
				{Plus, "+"},
				{Number, "2"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) { assertTokens(t, test.source, test.want) })
	}
}

func TestQuotedTemplateErrors(t *testing.T) {
	for _, test := range []struct {
		name, source string
		kinds        []DiagnosticKind
	}{
		{"unclosed quote", `"abc`, []DiagnosticKind{UnterminatedQuotedTemplate}},
		{
			"unclosed sequence",
			`"${x`,
			[]DiagnosticKind{UnterminatedQuotedTemplate, UnterminatedTemplateSequence},
		},
		{"unknown escape", `"\q"`, []DiagnosticKind{InvalidEscape}},
		{"truncated escape", "\"abc\\", []DiagnosticKind{UnterminatedQuotedTemplate, InvalidEscape}},
		{"short unicode", `"\u12"`, []DiagnosticKind{InvalidEscape}},
		{"unicode EOF", `"\U123`, []DiagnosticKind{UnterminatedQuotedTemplate, InvalidEscape}},
		{"surrogate", `"\uD800"`, []DiagnosticKind{InvalidEscape}},
		{"out of range", `"\U00110000"`, []DiagnosticKind{InvalidEscape}},
		{"uint32 maximum", `"\UFFFFFFFF"`, []DiagnosticKind{InvalidEscape}},
		{"newline", "\"a\nb\r\nc\"", []DiagnosticKind{NewlineInQuotedTemplate, NewlineInQuotedTemplate}},
		{"invalid UTF8", "\"\xff\"", []DiagnosticKind{InvalidUTF8}},
		{"invalid opening strip", `"${ ~ x}"`, []DiagnosticKind{InvalidCharacter}},
		{"invalid closing strip", `"${x~ }"`, []DiagnosticKind{InvalidCharacter}},
		{"strip inside object", `"${{a=1~}}"`, []DiagnosticKind{InvalidCharacter}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Lex([]byte(test.source))
			assertPartition(t, []byte(test.source), result)
			var kinds []DiagnosticKind
			for _, diagnostic := range result.Diagnostics {
				kinds = append(kinds, diagnostic.Kind)
			}
			if !reflect.DeepEqual(kinds, test.kinds) {
				t.Errorf("diagnostics = %+v, want kinds %v", result.Diagnostics, test.kinds)
			}
		})
	}
}

func TestDeepTemplateNesting(t *testing.T) {
	const depth = 10000
	source := []byte(strings.Repeat(`"${`, depth) + `1` + strings.Repeat(`}"`, depth))
	result := Lex(source)
	assertPartition(t, source, result)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Tokens) != depth*4+2 {
		t.Fatalf("unexpected token count: %d", len(result.Tokens))
	}
}

func TestTemplateDiagnosticSpansAndRecovery(t *testing.T) {
	for _, test := range []struct {
		source string
		want   []Diagnostic
	}{
		{
			`"${"`,
			[]Diagnostic{
				{UnterminatedQuotedTemplate, Span{0, 1}},
				{UnterminatedTemplateSequence, Span{1, 3}},
				{UnterminatedQuotedTemplate, Span{3, 4}},
			},
		},
		{"\"a\r\nb\"x", []Diagnostic{{NewlineInQuotedTemplate, Span{2, 4}}}},
		{"\"a\rb\"x", []Diagnostic{{NewlineInQuotedTemplate, Span{2, 3}}}},
		{
			"\"a\r\r\nb\"x",
			[]Diagnostic{
				{NewlineInQuotedTemplate, Span{2, 3}},
				{NewlineInQuotedTemplate, Span{3, 5}},
			},
		},
		{
			"\"a\r",
			[]Diagnostic{
				{UnterminatedQuotedTemplate, Span{0, 1}},
				{NewlineInQuotedTemplate, Span{2, 3}},
			},
		},
		{`"\uD800"x`, []Diagnostic{{InvalidEscape, Span{1, 7}}}},
		{
			"<<-E\n${",
			[]Diagnostic{
				{UnterminatedHeredoc, Span{0, 3}},
				{UnterminatedTemplateSequence, Span{5, 7}},
			},
		},
	} {
		t.Run(test.source, func(t *testing.T) {
			result := Lex([]byte(test.source))
			assertPartition(t, []byte(test.source), result)
			if !reflect.DeepEqual(result.Diagnostics, test.want) {
				t.Errorf("diagnostics = %+v, want %+v", result.Diagnostics, test.want)
			}
			if strings.HasSuffix(test.source, "x") {
				last := result.Tokens[len(result.Tokens)-2]
				if last.Kind != Identifier || last.Span != (Span{len(test.source) - 1, len(test.source)}) {
					t.Errorf("failed to resume config mode: %+v", last)
				}
			}
		})
	}
}

func assertTokens(t *testing.T, text string, want []tokenText) {
	t.Helper()
	source := []byte(text)
	result := Lex(source)
	assertPartition(t, source, result)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	var got []tokenText
	for _, token := range result.Tokens[:len(result.Tokens)-1] {
		got = append(got, tokenText{token.Kind, string(source[token.Span.Start:token.Span.End])})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tokens = %#v\nwant %#v", got, want)
	}
}
