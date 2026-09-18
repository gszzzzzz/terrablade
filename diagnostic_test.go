package terrablade_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"terrablade"
)

func TestDiagnosticKinds(t *testing.T) {
	// This category is also used by the parser's invariant defenses, rather
	// than a reproducible complete-file grammar error.
	if terrablade.UnexpectedToken != "UnexpectedToken" {
		t.Fatal("UnexpectedToken's public name changed")
	}
	for _, test := range []struct {
		source string
		kind   terrablade.DiagnosticKind
		name   string
	}{
		{"a=\xff", terrablade.InvalidUTF8, "InvalidUTF8"},
		{"a=@", terrablade.InvalidCharacter, "InvalidCharacter"},
		{"/*", terrablade.UnterminatedBlockComment, "UnterminatedBlockComment"},
		{`a="x`, terrablade.UnterminatedQuotedTemplate, "UnterminatedQuotedTemplate"},
		{`a="${x`, terrablade.UnterminatedTemplateSequence, "UnterminatedTemplateSequence"},
		{`a="\q"`, terrablade.InvalidEscape, "InvalidEscape"},
		{"a=\"x\ny\"", terrablade.NewlineInQuotedTemplate, "NewlineInQuotedTemplate"},
		{"a=<<E\nx\n", terrablade.UnterminatedHeredoc, "UnterminatedHeredoc"},
		{"a=", terrablade.ExpectedExpression, "ExpectedExpression"},
		{"a=(x", terrablade.ExpectedClosingParen, "ExpectedClosingParen"},
		{"a=[x", terrablade.ExpectedClosingBracket, "ExpectedClosingBracket"},
		{"a=x?y", terrablade.ExpectedConditionalColon, "ExpectedConditionalColon"},
		{"a=f(x y)", terrablade.ExpectedArgumentSeparator, "ExpectedArgumentSeparator"},
		{"a=x.", terrablade.ExpectedAttributeName, "ExpectedAttributeName"},
		{"a=ns::()", terrablade.ExpectedFunctionName, "ExpectedFunctionName"},
		{"a=ns::f", terrablade.ExpectedOpeningParen, "ExpectedOpeningParen"},
		{"a=x.0.1", terrablade.InvalidLegacyIndex, "InvalidLegacyIndex"},
		{"a=x.*.*", terrablade.NestedAttributeSplat, "NestedAttributeSplat"},
		{"a=" + strings.Repeat("(", 2000) + "x" + strings.Repeat(")", 2000), terrablade.NestingLimitExceeded, "NestingLimitExceeded"},
		{"a=1.0.2", terrablade.InvalidNumber, "InvalidNumber"},
		{"a=[x y]", terrablade.ExpectedTupleSeparator, "ExpectedTupleSeparator"},
		{"b {", terrablade.ExpectedClosingBrace, "ExpectedClosingBrace"},
		{"a={x y}", terrablade.ExpectedObjectValueSeparator, "ExpectedObjectValueSeparator"},
		{"a={x=1 y=2}", terrablade.ExpectedObjectItemSeparator, "ExpectedObjectItemSeparator"},
		{"a=[for : x]", terrablade.ExpectedForVariable, "ExpectedForVariable"},
		{"a=[for x xs:x]", terrablade.ExpectedForIn, "ExpectedForIn"},
		{"a=[for x in xs]", terrablade.ExpectedForColon, "ExpectedForColon"},
		{"a={for x in xs:x}", terrablade.ExpectedForArrow, "ExpectedForArrow"},
		{"a=[for x in xs:x=>x]", terrablade.UnexpectedForKey, "UnexpectedForKey"},
		{"a=[for x in xs:x...]", terrablade.UnexpectedForGrouping, "UnexpectedForGrouping"},
		{`a="${x y}"`, terrablade.ExpectedTemplateSequenceEnd, "ExpectedTemplateSequenceEnd"},
		{`a="%{}"`, terrablade.ExpectedTemplateDirective, "ExpectedTemplateDirective"},
		{`a="%{unknown}"`, terrablade.UnknownTemplateDirective, "UnknownTemplateDirective"},
		{`a="%{else}"`, terrablade.UnexpectedTemplateDirective, "UnexpectedTemplateDirective"},
		{`a="%{if x}"`, terrablade.ExpectedTemplateEndIf, "ExpectedTemplateEndIf"},
		{`a="%{for x in xs}"`, terrablade.ExpectedTemplateEndFor, "ExpectedTemplateEndFor"},
		{"1", terrablade.ExpectedBodyItem, "ExpectedBodyItem"},
		{"a\n", terrablade.ExpectedAttributeOrBlock, "ExpectedAttributeOrBlock"},
		{"a=1 b=2", terrablade.ExpectedBodyItemSeparator, "ExpectedBodyItemSeparator"},
		{"b label", terrablade.ExpectedBlockOpeningBrace, "ExpectedBlockOpeningBrace"},
		{`b "${x}" {}`, terrablade.ExpectedLiteralBlockLabel, "ExpectedLiteralBlockLabel"},
		{"b { c {} }", terrablade.ExpectedSingleLineAttribute, "ExpectedSingleLineAttribute"},
		{"b { a=1\n}", terrablade.ExpectedSingleLineBlockEnd, "ExpectedSingleLineBlockEnd"},
		{"a=1\na=2\n", terrablade.DuplicateAttribute, "DuplicateAttribute"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if string(test.kind) != test.name {
				t.Fatalf("public kind changed: %q => %q", test.name, test.kind)
			}
			diagnostics := parseDiagnostics(t, []byte(test.source))
			for _, diagnostic := range diagnostics {
				if diagnostic.Kind == test.kind {
					if diagnostic.Message == "" || diagnostic.Message == "Unknown diagnostic." {
						t.Fatalf("diagnostic lacks a message: %+v", diagnostic)
					}
					return
				}
			}
			t.Fatalf("did not report %s: %+v", test.kind, diagnostics)
		})
	}
}

func TestDiagnosticPositions(t *testing.T) {
	for _, test := range []struct {
		source string
		line   int
		column int
	}{
		{"a=", 1, 3},
		{"\ufeffa=", 1, 4},
		{"\t\ta=", 1, 5},
		{"/*e\u0301👩‍💻*/a=", 1, 9},
		{"/*🇰🇷🇦*/a=", 1, 9},
		{"/*\u0915\u094d\u0915*/a=", 1, 8},
		{"/*\u0600a*/a=", 1, 8},
		{"/*\r*/a=", 1, 8},
		{"/*\x80\xc0\xaf\xe2\x82*/a=", 1, 12},
		{"# first\r\n\t이름=", 2, 5},
		{"# first\n\n\tname=", 3, 7},
	} {
		t.Run(test.source, func(t *testing.T) {
			diagnostics := parseDiagnostics(t, []byte(test.source))
			want := terrablade.Position{Offset: len(test.source), Line: test.line, Column: test.column}
			for _, diagnostic := range diagnostics {
				if diagnostic.Kind == terrablade.ExpectedExpression {
					if diagnostic.Span != (terrablade.Span{Start: want, End: want}) {
						t.Fatalf("span = %+v, want EOF %+v", diagnostic.Span, want)
					}
					return
				}
			}
			t.Fatalf("missing ExpectedExpression: %+v", diagnostics)
		})
	}
}

func TestDiagnosticClusterInteriors(t *testing.T) {
	// Lexical errors can end inside a grapheme cluster. Byte offsets must stay
	// exact even when the corresponding columns coincide.
	for _, test := range []struct {
		source     string
		start, end terrablade.Position
	}{
		{"👩‍💻", terrablade.Position{Offset: 0, Line: 1, Column: 1}, terrablade.Position{Offset: 4, Line: 1, Column: 1}},
		{"\r\n👩‍💻", terrablade.Position{Offset: 2, Line: 2, Column: 1}, terrablade.Position{Offset: 6, Line: 2, Column: 1}},
		{"\u0600\xff", terrablade.Position{Offset: 2, Line: 1, Column: 2}, terrablade.Position{Offset: 3, Line: 1, Column: 3}},
	} {
		diagnostics := parseDiagnostics(t, []byte(test.source))
		found := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Span.Start.Offset == test.start.Offset && diagnostic.Span.End.Offset == test.end.Offset {
				found = true
				if diagnostic.Span != (terrablade.Span{Start: test.start, End: test.end}) {
					t.Errorf("%q: incorrect cluster location: %+v", test.source, diagnostic)
				}
			}
		}
		if !found {
			t.Fatalf("%q: missing expected span: %+v", test.source, diagnostics)
		}
	}
}

func TestDiagnosticLineEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		diagnostics := parseDiagnostics(t, []byte("a="+ending))
		want := terrablade.Span{
			Start: terrablade.Position{Offset: 2, Line: 1, Column: 3},
			End:   terrablade.Position{Offset: 2 + len(ending), Line: 2, Column: 1},
		}
		if len(diagnostics) != 1 || diagnostics[0].Kind != terrablade.ExpectedExpression || diagnostics[0].Span != want {
			t.Fatalf("%q: diagnostics = %+v, want span %+v", ending, diagnostics, want)
		}
	}
}

func TestDiagnosticOrdering(t *testing.T) {
	source := []byte("a=\xff\nb=\nc=\n")
	want := parseDiagnostics(t, source)
	if want[0].Kind != terrablade.InvalidUTF8 || want[1].Kind != terrablade.ExpectedExpression || want[0].Span.Start.Offset != want[1].Span.Start.Offset {
		t.Fatalf("lexical error must precede syntax errors at same position: %+v", want)
	}
	for i := 1; i < len(want); i++ {
		if want[i-1].Span.Start.Offset > want[i].Span.Start.Offset {
			t.Fatalf("diagnostics are out of source order: %+v", want)
		}
	}
	if got := parseDiagnostics(t, source); !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics are nondeterministic: %+v => %+v", want, got)
	}
}

func parseDiagnostics(t testing.TB, source []byte) []terrablade.Diagnostic {
	t.Helper()
	output, err := terrablade.Format(source, terrablade.Options{})
	var parseError *terrablade.ParseError
	if output != nil || !errors.As(err, &parseError) {
		t.Fatalf("Format(%q) = %q, %v; want nil and ParseError", source, output, err)
	}
	return parseError.Diagnostics()
}
