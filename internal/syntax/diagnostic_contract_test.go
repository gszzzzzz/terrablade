package syntax_test

import (
	"testing"

	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestDiagnosticMessagesFromParse(t *testing.T) {
	for _, test := range []struct {
		source string
		kind   syntax.DiagnosticKind
		want   string
	}{
		{"a=\xff", syntax.InvalidUTF8, "Invalid UTF-8 encoding."},
		{"a=@", syntax.InvalidCharacter, "Invalid character."},
		{"/*", syntax.UnterminatedBlockComment, "Unterminated block comment; expected '*/'."},
		{`a="x`, syntax.UnterminatedQuotedTemplate, "Unterminated quoted template; expected a closing quote."},
		{`a="${x`, syntax.UnterminatedTemplateSequence, "Unterminated template interpolation or directive; expected '}'."},
		{`a="\q"`, syntax.InvalidEscape, "Invalid escape sequence."},
		{
			"a=\"x\ny\"",
			syntax.NewlineInQuotedTemplate,
			"A quoted template cannot contain a literal newline; use a heredoc instead.",
		},
		{"a=<<E\nx\n", syntax.UnterminatedHeredoc, "Unterminated heredoc; expected a closing marker on its own line."},
		{"a=", syntax.ExpectedExpression, "Expected an expression."},
		{"a=(x", syntax.ExpectedClosingParen, "Expected ')'."},
		{"a=[x", syntax.ExpectedClosingBracket, "Expected ']'."},
		{"a=x?y", syntax.ExpectedConditionalColon, "Expected ':' between the true and false expressions of a conditional."},
		{"a=f(x y)", syntax.ExpectedArgumentSeparator, "Expected ',' between function arguments."},
		{"a=x.", syntax.ExpectedAttributeName, "Expected an attribute name after '.'."},
		{"a=ns::()", syntax.ExpectedFunctionName, "Expected a function name after '::'."},
		{"a=ns::f", syntax.ExpectedOpeningParen, "Expected '(' to start function arguments."},
		{"a=x.0.1", syntax.InvalidLegacyIndex, "A legacy dot index cannot contain a decimal point."},
		{"a=x.*.*", syntax.NestedAttributeSplat, "An attribute splat cannot contain another '.*' step."},
		{"a=1.0.2", syntax.InvalidNumber, "Invalid or unrepresentable number literal."},
		{"a=[x y]", syntax.ExpectedTupleSeparator, "Expected ',' between tuple elements."},
		{"b {", syntax.ExpectedClosingBrace, "Expected '}'."},
		{"a={x y}", syntax.ExpectedObjectValueSeparator, "Expected '=' or ':' between an object key and its value."},
		{"a={x=1 y=2}", syntax.ExpectedObjectItemSeparator, "Expected a comma or newline between object items."},
		{"a=[for : x]", syntax.ExpectedForVariable, "Expected an iteration variable name."},
		{`a="%{for }x%{endfor}"`, syntax.ExpectedForVariable, "Expected an iteration variable name."},
		{"a=[for x xs:x]", syntax.ExpectedForIn, "Expected 'in' after the iteration variable names."},
		{`a="%{for x xs}x%{endfor}"`, syntax.ExpectedForIn, "Expected 'in' after the iteration variable names."},
		{"a=[for x in xs]", syntax.ExpectedForColon, "Expected ':' after the collection in a for expression."},
		{
			"a={for x in xs:x}",
			syntax.ExpectedForArrow,
			"Expected '=>' between the key and value of an object for expression.",
		},
		{"a=[for x in xs:x=>x]", syntax.UnexpectedForKey, "A tuple for expression cannot produce a key."},
		{
			"a=[for x in xs:x...]",
			syntax.UnexpectedForGrouping,
			"Grouping with '...' is allowed only in an object for expression.",
		},
		{`a="${x y}"`, syntax.ExpectedTemplateSequenceEnd, "Expected '}' to close the template sequence."},
		{`a="%{}"`, syntax.ExpectedTemplateDirective, "Expected a template directive name."},
		{
			`a="%{unknown}"`,
			syntax.UnknownTemplateDirective,
			"Unknown template directive; expected 'if', 'else', 'endif', 'for', or 'endfor'.",
		},
		{
			`a="%{else}"`,
			syntax.UnexpectedTemplateDirective,
			"The template directive has no matching open scope or repeats an 'else'.",
		},
		{`a="%{if x}"`, syntax.ExpectedTemplateEndIf, "Expected an 'endif' template directive."},
		{`a="%{for x in xs}"`, syntax.ExpectedTemplateEndFor, "Expected an 'endfor' template directive."},
		{"1", syntax.ExpectedBodyItem, "Expected an attribute or block name."},
		{"a\n", syntax.ExpectedAttributeOrBlock, "Expected '=' for an attribute, or a label or '{' for a block."},
		{"a=1 b=2", syntax.ExpectedBodyItemSeparator, "Expected a newline after the attribute or block."},
		{"b label", syntax.ExpectedBlockOpeningBrace, "Expected '{' to start the block body."},
		{
			`b "${x}" {}`,
			syntax.ExpectedLiteralBlockLabel,
			"Block labels must be literal strings without interpolation or directives.",
		},
		{
			"b { c {} }",
			syntax.ExpectedSingleLineAttribute,
			"Expected '=' after the attribute name in a single-line block; nested blocks require a multiline body.",
		},
		{
			"b { a }",
			syntax.ExpectedSingleLineAttribute,
			"Expected '=' after the attribute name in a single-line block; nested blocks require a multiline body.",
		},
		{"b { a=1\n}", syntax.ExpectedSingleLineBlockEnd, "Expected '}' on the same line after the block's attribute."},
		{"a=1\na=2\n", syntax.DuplicateAttribute, "An attribute with this name is already defined in the same body."},
	} {
		t.Run(test.kind.String()+"/"+test.source, func(t *testing.T) {
			result := syntax.Parse([]byte(test.source))
			for _, diagnostic := range result.Diagnostics() {
				if diagnostic.Kind == test.kind {
					if got := diagnostic.Kind.Message(); got != test.want {
						t.Fatalf("Message() = %q, want %q", got, test.want)
					}
					return
				}
			}
			t.Fatalf("Parse(%q) did not produce %s: %+v", test.source, test.kind, result.Diagnostics())
		})
	}
	// A decimal-point restriction is narrower than an integer-only restriction.
	if got := syntax.Parse([]byte("a=x.1e-1")).Diagnostics(); len(got) != 0 {
		t.Fatalf("a fractional exponent in a legacy dot index must remain valid: %+v", got)
	}
}

func TestUnexpectedTokenMessage(t *testing.T) {
	// This category covers both a retained parse tail and the template parser's
	// lexer-invariant defense, not one particular expected token or delimiter.
	const want = "Unexpected token; expected a valid continuation or the end of the current construct."
	if got := syntax.UnexpectedToken.Message(); got != want {
		t.Fatalf("UnexpectedToken.Message() = %q, want %q", got, want)
	}
}

func TestDiagnosticMessageAllocations(t *testing.T) {
	// All values, including unknown kinds, must return static prose.
	length := 0
	allocations := testing.AllocsPerRun(100, func() {
		for value := 0; value <= 255; value++ {
			length += len(syntax.DiagnosticKind(value).Message())
		}
	})
	if length == 0 || allocations != 0 {
		t.Fatalf("Message returned %d bytes with %g allocations", length, allocations)
	}
}
