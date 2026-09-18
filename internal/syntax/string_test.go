package syntax

import (
	"fmt"
	"strings"
	"testing"
)

func TestTokenKindString(t *testing.T) {
	want := []string{
		"Invalid",
		"EOF",
		"BOM",
		"Whitespace",
		"Newline",
		"LineComment",
		"BlockComment",
		"Identifier",
		"Number",
		"OpenBrace",
		"CloseBrace",
		"OpenBracket",
		"CloseBracket",
		"OpenParen",
		"CloseParen",
		"Plus",
		"Minus",
		"Star",
		"Slash",
		"Percent",
		"And",
		"Or",
		"Bang",
		"Equal",
		"EqualEqual",
		"NotEqual",
		"Less",
		"LessEqual",
		"Greater",
		"GreaterEqual",
		"Arrow",
		"Colon",
		"DoubleColon",
		"Question",
		"Dot",
		"Ellipsis",
		"Comma",
		"QuoteOpen",
		"QuoteClose",
		"TemplateText",
		"InterpolationOpen",
		"DirectiveOpen",
		"StripMarker",
		"TemplateSequenceEnd",
		"HeredocOpen",
		"HeredocMarker",
		"HeredocEndMarker",
	}
	assertEnumNames(t, want, uint8(tokenKindCount), func(value uint8) string { return TokenKind(value).String() })
	if got := TokenKind(255).String(); got != "TokenKind(255)" {
		t.Fatalf("unknown TokenKind = %q, want %q", got, "TokenKind(255)")
	}
}

func TestNodeKindString(t *testing.T) {
	want := []string{
		"InvalidNode",
		"File",
		"Error",
		"LiteralExpression",
		"VariableExpression",
		"ParenthesizedExpression",
		"UnaryExpression",
		"BinaryExpression",
		"ConditionalExpression",
		"FunctionCallExpression",
		"TraversalExpression",
		"AttributeAccess",
		"IndexAccess",
		"LegacyIndexAccess",
		"AttributeSplat",
		"FullSplat",
		"TupleExpression",
		"ObjectExpression",
		"ObjectItem",
		"ForExpression",
		"TemplateExpression",
		"TemplateInterpolation",
		"TemplateDirective",
		"TemplateIf",
		"TemplateFor",
	}
	assertEnumNames(t, want, uint8(nodeKindCount), func(value uint8) string { return NodeKind(value).String() })
	if got := NodeKind(255).String(); got != "NodeKind(255)" {
		t.Fatalf("unknown NodeKind = %q, want %q", got, "NodeKind(255)")
	}
}

func TestDiagnosticKindString(t *testing.T) {
	want := []string{
		"InvalidUTF8",
		"InvalidCharacter",
		"UnterminatedBlockComment",
		"UnterminatedQuotedTemplate",
		"UnterminatedTemplateSequence",
		"InvalidEscape",
		"NewlineInQuotedTemplate",
		"UnterminatedHeredoc",
		"ExpectedExpression",
		"UnexpectedToken",
		"ExpectedClosingParen",
		"ExpectedClosingBracket",
		"ExpectedConditionalColon",
		"ExpectedArgumentSeparator",
		"ExpectedAttributeName",
		"ExpectedFunctionName",
		"ExpectedOpeningParen",
		"InvalidLegacyIndex",
		"NestedAttributeSplat",
		"NestingLimitExceeded",
		"InvalidNumber",
		"ExpectedTupleSeparator",
		"ExpectedClosingBrace",
		"ExpectedObjectValueSeparator",
		"ExpectedObjectItemSeparator",
		"ExpectedForVariable",
		"ExpectedForIn",
		"ExpectedForColon",
		"ExpectedForArrow",
		"UnexpectedForKey",
		"UnexpectedForGrouping",
		"ExpectedTemplateSequenceEnd",
		"ExpectedTemplateDirective",
		"UnknownTemplateDirective",
		"UnexpectedTemplateDirective",
		"ExpectedTemplateEndIf",
		"ExpectedTemplateEndFor",
	}
	assertEnumNames(t, want, uint8(diagnosticKindCount), func(value uint8) string { return DiagnosticKind(value).String() })
	if got := DiagnosticKind(255).String(); got != "DiagnosticKind(255)" {
		t.Fatalf("unknown DiagnosticKind = %q, want %q", got, "DiagnosticKind(255)")
	}
}

func TestDiagnosticFormattingUsesKindName(t *testing.T) {
	formatted := fmt.Sprintf("%+v", Diagnostic{
		Kind: InvalidNumber,
		Span: Span{Start: 1, End: 2},
	})
	if !strings.Contains(formatted, "Kind:InvalidNumber") {
		t.Fatalf("formatted diagnostic = %q, want readable kind", formatted)
	}
}

func assertEnumNames(t *testing.T, want []string, count uint8, name func(uint8) string) {
	t.Helper()
	if len(want) != int(count) {
		t.Fatalf("test covers %d values, enum defines %d", len(want), count)
	}
	for value, expected := range want {
		if got := name(uint8(value)); got != expected {
			t.Errorf("value %d = %q, want %q", value, got, expected)
		}
	}
}
