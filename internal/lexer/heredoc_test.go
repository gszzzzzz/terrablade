package lexer

import (
	"reflect"
	"strings"
	"testing"
)

func TestHeredocs(t *testing.T) {
	for _, test := range []struct {
		name, source string
		want         []tokenText
	}{
		{
			"empty",
			"<<EOT\nEOT\n",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "EOT"},
				{Newline, "\n"},
				{HeredocEndMarker, "EOT"},
				{Newline, "\n"},
			},
		},
		{
			"literal before expression",
			"<<E\nhello ${x}\nE\n",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "E"},
				{Newline, "\n"},
				{TemplateText, "hello "},
				{InterpolationOpen, "${"},
				{Identifier, "x"},
				{TemplateSequenceEnd, "}"},
				{TemplateText, "\n"},
				{HeredocEndMarker, "E"},
				{Newline, "\n"},
			},
		},
		{
			"raw literal",
			"<<-EOT\r\n # /* \\q \" \uFEFF $${x} %%{if}\r\nEOT\r\n",
			[]tokenText{
				{HeredocOpen, "<<-"},
				{HeredocMarker, "EOT"},
				{Newline, "\r\n"},
				{TemplateText, " # /* \\q \" \uFEFF $${x} %%{if}\r\n"},
				{HeredocEndMarker, "EOT"},
				{Newline, "\r\n"},
			},
		},
		{
			"marker substring",
			"<<EOT\nEOTx\nxEOT\nEOT\n",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "EOT"},
				{Newline, "\n"},
				{TemplateText, "EOTx\nxEOT\n"},
				{HeredocEndMarker, "EOT"},
				{Newline, "\n"},
			},
		},
		{
			"Unicode marker",
			"<<_한-글\nhello\n_한-글\n",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "_한-글"},
				{Newline, "\n"},
				{TemplateText, "hello\n"},
				{HeredocEndMarker, "_한-글"},
				{Newline, "\n"},
			},
		},
		{
			"template sequences",
			"<<E\n%{~if x}${y~}%{endif}\nE\n",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "E"},
				{Newline, "\n"},
				{DirectiveOpen, "%{"},
				{StripMarker, "~"},
				{Identifier, "if"},
				{Whitespace, " "},
				{Identifier, "x"},
				{TemplateSequenceEnd, "}"},
				{InterpolationOpen, "${"},
				{Identifier, "y"},
				{StripMarker, "~"},
				{TemplateSequenceEnd, "}"},
				{DirectiveOpen, "%{"},
				{Identifier, "endif"},
				{TemplateSequenceEnd, "}"},
				{TemplateText, "\n"},
				{HeredocEndMarker, "E"},
				{Newline, "\n"},
			},
		},
		{
			"marker after interpolation is text",
			"<<E\n${x}E\nE\n",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "E"},
				{Newline, "\n"},
				{InterpolationOpen, "${"},
				{Identifier, "x"},
				{TemplateSequenceEnd, "}"},
				{TemplateText, "E\n"},
				{HeredocEndMarker, "E"},
				{Newline, "\n"},
			},
		},
		{
			"nested heredoc",
			"<<E\n${<<E\ninner\nE\n}outer\nE\nx=1",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "E"},
				{Newline, "\n"},
				{InterpolationOpen, "${"},
				{HeredocOpen, "<<"},
				{HeredocMarker, "E"},
				{Newline, "\n"},
				{TemplateText, "inner\n"},
				{HeredocEndMarker, "E"},
				{Newline, "\n"},
				{TemplateSequenceEnd, "}"},
				{TemplateText, "outer\n"},
				{HeredocEndMarker, "E"},
				{Newline, "\n"},
				{Identifier, "x"},
				{Equal, "="},
				{Number, "1"},
			},
		},
		{
			"heredoc inside quote",
			"\"${<<E\nx\nE\n}\"",
			[]tokenText{
				{QuoteOpen, `"`},
				{InterpolationOpen, "${"},
				{HeredocOpen, "<<"},
				{HeredocMarker, "E"},
				{Newline, "\n"},
				{TemplateText, "x\n"},
				{HeredocEndMarker, "E"},
				{Newline, "\n"},
				{TemplateSequenceEnd, "}"},
				{QuoteClose, `"`},
			},
		},
		{
			"quote inside heredoc",
			"<<E\n${\"text\"}\nE\n",
			[]tokenText{
				{HeredocOpen, "<<"},
				{HeredocMarker, "E"},
				{Newline, "\n"},
				{InterpolationOpen, "${"},
				{QuoteOpen, `"`},
				{TemplateText, "text"},
				{QuoteClose, `"`},
				{TemplateSequenceEnd, "}"},
				{TemplateText, "\n"},
				{HeredocEndMarker, "E"},
				{Newline, "\n"},
			},
		},
		{"not a heredoc", "<<-EOT", []tokenText{
			{Less, "<"},
			{Less, "<"},
			{Minus, "-"},
			{Identifier, "EOT"},
		}},
		{
			"spaced opener",
			"<< EOT\n",
			[]tokenText{
				{Less, "<"},
				{Less, "<"},
				{Whitespace, " "},
				{Identifier, "EOT"},
				{Newline, "\n"},
			},
		},
		{
			"trailing opener space",
			"<<EOT \n",
			[]tokenText{
				{Less, "<"},
				{Less, "<"},
				{Identifier, "EOT"},
				{Whitespace, " "},
				{Newline, "\n"},
			},
		},
		{"numeric marker", "<<12\n", []tokenText{
			{Less, "<"},
			{Less, "<"},
			{Number, "12"},
			{Newline, "\n"},
		}},
	} {
		t.Run(test.name, func(t *testing.T) { assertTokens(t, test.source, test.want) })
	}
}

func TestDeepHeredocNesting(t *testing.T) {
	const depth = 5000
	source := []byte(strings.Repeat("<<E\n${", depth) + "1" + strings.Repeat("}\nE\n", depth))
	result := Lex(source)
	assertPartition(t, source, result)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", result.Diagnostics)
	}
	if len(result.Tokens) != depth*8+2 {
		t.Fatalf("unexpected token count: %d", len(result.Tokens))
	}
}

func TestHeredocTerminatorWhitespace(t *testing.T) {
	// Native HCL tooling accepts these for both opener forms, despite the spec's
	// description emphasizing indentation only for <<-. Preserve closing bytes.
	for _, opener := range []string{"<<", "<<-"} {
		for _, closing := range []string{"  EOT", "EOT \t", "\tEOT\t ", "\u00A0EOT\u2003"} {
			for _, newline := range []string{"\n", "\r\n"} {
				source := opener + "EOT" + newline + "text" + newline + closing + newline + "x=1"
				t.Run(source, func(t *testing.T) {
					assertTokens(t, source, []tokenText{
						{HeredocOpen, opener},
						{HeredocMarker, "EOT"},
						{Newline, newline},
						{TemplateText, "text" + newline},
						{HeredocEndMarker, closing},
						{Newline, newline},
						{Identifier, "x"},
						{Equal, "="},
						{Number, "1"},
					})
				})
			}
		}
	}
}

func TestHeredocErrors(t *testing.T) {
	for _, test := range []struct {
		source string
		kinds  []DiagnosticKind
	}{
		{"<<E\n", []DiagnosticKind{UnterminatedHeredoc}},
		{"<<E\nE", []DiagnosticKind{UnterminatedHeredoc}},
		{"<<-E\nE", []DiagnosticKind{UnterminatedHeredoc}},
		{"<<E\n E \t", []DiagnosticKind{UnterminatedHeredoc}},
		{"<<E\n${x", []DiagnosticKind{UnterminatedHeredoc, UnterminatedTemplateSequence}},
		{"<<E\n\xff\nE\n", []DiagnosticKind{InvalidUTF8}},
		{"<<E\n\uFEFFE\n", []DiagnosticKind{UnterminatedHeredoc}},
		{"<<é\ne\u0301\n", []DiagnosticKind{UnterminatedHeredoc}},
	} {
		t.Run(test.source, func(t *testing.T) {
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
