package syntax

import (
	"reflect"
	"strings"
	"testing"
)

func TestTraversalShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"incomplete exponent name is an attribute",
			"1.e",
			`File(Traversal(Literal("1"), Attribute(".", "e")))`,
		},
		{
			"ordinary attribute on number expression",
			"1.foo",
			`File(Traversal(Literal("1"), Attribute(".", "foo")))`,
		},
		{
			"hyphenated attribute name is not an exponent",
			"1.e-",
			`File(Traversal(Literal("1"), Attribute(".", "e-")))`,
		},
		{
			"postfix binds inside unary",
			"-foo.bar[0]",
			`File(Unary("-", Traversal(Variable("foo"), Attribute(".", "bar"), Index("[", Literal("0"), "]"))))`,
		},
		{
			"index expression and following attribute",
			"foo[1 + i].true",
			`File(Traversal(Variable("foo"), Index("[", Binary(Literal("1"), "+", Variable("i")), "]"), Attribute(".", "true")))`,
		},
		{
			"legacy dot index",
			"foo.0",
			`File(Traversal(Variable("foo"), LegacyIndex(".", "0")))`,
		},
		{
			"legacy exponent index",
			"foo.0e1",
			`File(Traversal(Variable("foo"), LegacyIndex(".", "0e1")))`,
		},
		{
			"legacy splat index is outside projection",
			"foo.*.bar[0].baz",
			`File(Traversal(Variable("foo"), AttributeSplat(".", "*", Attribute(".", "bar")), Index("[", Literal("0"), "]"), Attribute(".", "baz")))`,
		},
		{
			"full splat index is inside projection",
			"foo[*].bar[0].baz",
			`File(Traversal(Variable("foo"), FullSplat("[", "*", "]", Attribute(".", "bar"), Index("[", Literal("0"), "]"), Attribute(".", "baz"))))`,
		},
		{
			"nested full splats",
			"foo[*][*].bar",
			`File(Traversal(Variable("foo"), FullSplat("[", "*", "]", FullSplat("[", "*", "]", Attribute(".", "bar")))))`,
		},
		{
			"legacy and full splat chaining",
			"foo.*.0[*].bar",
			`File(Traversal(Variable("foo"), AttributeSplat(".", "*", LegacyIndex(".", "0")), FullSplat("[", "*", "]", Attribute(".", "bar"))))`,
		},
		{
			"full splat then legacy splat",
			"foo[*].*.bar",
			`File(Traversal(Variable("foo"), FullSplat("[", "*", "]", AttributeSplat(".", "*", Attribute(".", "bar")))))`,
		},
		{
			"index separates two legacy splats",
			"foo.*.bar[0].*.baz",
			`File(Traversal(Variable("foo"), AttributeSplat(".", "*", Attribute(".", "bar")), Index("[", Literal("0"), "]"), AttributeSplat(".", "*", Attribute(".", "baz"))))`,
		},
		{
			"trivia separates legacy numeric candidates",
			"foo.0 .0",
			`File(Traversal(Variable("foo"), LegacyIndex(".", "0"), LegacyIndex(".", "0")))`,
		},
		{
			"parenthesized full splat allows newlines",
			"(foo[\n*\n].bar)",
			`File(Paren("(", Traversal(Variable("foo"), FullSplat("[", "*", "]", Attribute(".", "bar"))), ")"))`,
		},
		{
			"ordinary index allows newlines",
			"foo[\n0\n]",
			`File(Traversal(Variable("foo"), Index("[", Literal("0"), "]")))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", file.diagnostics)
			}
			if got := expressionShape(file, file.root); got != test.shape {
				t.Fatalf("shape:\n%s\nwant:\n%s", got, test.shape)
			}
		})
	}
}

// Both token boundaries and attribute-value syntax acceptance were compared
// against the HCL v2.25.0 scanner/parser, without adding a runtime dependency.
func TestUpstreamNumericExpressionCompatibility(t *testing.T) {
	for _, test := range []struct {
		source string
		valid  bool
	}{
		{
			"1.",
			false,
		},
		{
			"1.e",
			true,
		},
		{
			"1.e2",
			true,
		},
		{
			"1.e+2",
			true,
		},
		{
			"1.e-2",
			true,
		},
		{
			"1.e2-foo",
			true,
		},
		{
			"1.foo",
			true,
		},
		{
			"1.0.2",
			false,
		},
		{
			"foo.0e1.0",
			false,
		},
		{
			"foo.0 .0",
			true,
		},
		{
			"1.e2foo",
			false,
		},
		{
			"1.e+",
			false,
		},
		{
			"1.e-",
			true,
		},
		{
			"1..",
			false,
		},
		{
			"1...",
			false,
		},
		{
			"f(1...)",
			true,
		},
		{
			"1e2.thing",
			true,
		},
		{
			"1.e2.foo",
			true,
		},
		{
			"1.E2-bar",
			true,
		},
		{
			"1.e2.0",
			false,
		},
		{
			"1e+2.0",
			false,
		},
		{
			"1..e2",
			false,
		},
		{
			"1e1e2foo",
			false,
		},
	} {
		t.Run(test.source, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if valid := len(file.diagnostics) == 0; valid != test.valid {
				t.Fatalf("valid = %v, want %v; diagnostics: %+v", valid, test.valid, file.diagnostics)
			}
			for _, diagnostic := range file.diagnostics {
				if diagnostic.Kind == UnsupportedExpression {
					t.Fatal("numeric compatibility cannot be deferred as unsupported")
				}
			}
		})
	}
}

func TestTraversalDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"missing close bracket",
			"a[0",
			[]Diagnostic{{ExpectedClosingBracket, Span{3, 3}}},
			`File(Traversal(Variable("a"), Index("[", Literal("0"))))`,
		},
		{
			"missing index expression",
			"a[]",
			[]Diagnostic{{ExpectedExpression, Span{2, 3}}},
			`File(Traversal(Variable("a"), Index("[", Error(), "]")))`,
		},
		{
			"missing attribute name",
			"foo.",
			[]Diagnostic{{ExpectedAttributeName, Span{4, 4}}},
			`File(Traversal(Variable("foo"), Error(".")))`,
		},
		{
			"attribute name recovery leaves the rest to the file",
			"foo.(bar)",
			[]Diagnostic{{ExpectedAttributeName, Span{4, 5}}, {UnexpectedToken, Span{4, 5}}},
			`File(Traversal(Variable("foo"), Error(".")), Error("(", "bar", ")"))`,
		},
		{
			"nested attribute splat",
			"foo.*.bar.*.baz",
			[]Diagnostic{{NestedAttributeSplat, Span{10, 11}}},
			`File(Traversal(Variable("foo"), AttributeSplat(".", "*", Attribute(".", "bar"), Error(".", "*"), Attribute(".", "baz"))))`,
		},
		{
			"newline before full splat marker",
			"foo[\n*]",
			[]Diagnostic{{ExpectedExpression, Span{5, 6}}},
			`File(Traversal(Variable("foo"), Index("[", Error("*"), "]")))`,
		},
		{
			"newline before full splat closer",
			"foo[*\n]",
			[]Diagnostic{{ExpectedClosingBracket, Span{5, 6}}, {UnexpectedToken, Span{6, 7}}},
			`File(Traversal(Variable("foo"), FullSplat("[", "*")), Error("]"))`,
		},
		{
			"index diagnostics stay inside the step",
			"foo[1 +].bar",
			[]Diagnostic{{ExpectedExpression, Span{7, 8}}},
			`File(Traversal(Variable("foo"), Index("[", Binary(Literal("1"), "+", Error()), "]"), Attribute(".", "bar")))`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertDiagnosticsAndShape(t, test.source, test.diagnostics, test.shape)
		})
	}
}

// These acceptance results were checked against HCL v2.25.0 ParseConfig with
// each expression used as an attribute value. In particular, upstream permits
// exponents (even non-integer values) after a legacy dot, but rejects a decimal
// point in that number token. Value/index validity is not a syntax decision.
// https://github.com/hashicorp/hcl/blob/v2.25.0/hclsyntax/parser.go
func TestLegacyIndexCompatibility(t *testing.T) {
	for _, test := range []struct {
		source string
		want   []DiagnosticKind
	}{
		{
			"foo.0",
			nil,
		},
		{
			"foo.00",
			nil,
		},
		{
			"foo.0e1",
			nil,
		},
		{
			"foo.1e-1",
			nil,
		},
		{
			"foo.1E+2",
			nil,
		},
		{
			"foo.1e-2147483648",
			nil,
		},
		{
			"foo.0e2147483648",
			nil,
		},
		{
			"foo.*.0e1",
			nil,
		},
		{
			"foo.*.1e-1",
			nil,
		},
		{
			"foo.0.1",
			[]DiagnosticKind{
				InvalidLegacyIndex,
			},
		},
		{
			"foo.1.0",
			[]DiagnosticKind{
				InvalidLegacyIndex,
			},
		},
		{
			"foo.1.0e1",
			[]DiagnosticKind{
				InvalidLegacyIndex,
			},
		},
		{
			"foo.*.0.1",
			[]DiagnosticKind{
				InvalidLegacyIndex,
			},
		},
		{
			"foo.-1",
			[]DiagnosticKind{
				ExpectedAttributeName,
			},
		},
		{
			"foo.1e",
			[]DiagnosticKind{
				UnexpectedToken,
			},
		},
		{
			"foo.1e+",
			[]DiagnosticKind{
				UnexpectedToken,
			},
		},
		{
			"foo.1e2147483648",
			[]DiagnosticKind{
				InvalidNumber,
			},
		},
		{
			"foo.0e1.0",
			[]DiagnosticKind{
				InvalidLegacyIndex,
			},
		},
		{
			"foo.0e1.0e2",
			[]DiagnosticKind{
				InvalidLegacyIndex,
			},
		},
		{
			"foo.0 .0",
			nil,
		},
		{
			"foo.0e1 .0",
			nil,
		},
		{
			"foo.0e1.a",
			nil,
		},
	} {
		t.Run(test.source, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			var got []DiagnosticKind
			for _, diagnostic := range file.diagnostics {
				got = append(got, diagnostic.Kind)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("diagnostics = %v, want %v", got, test.want)
			}
		})
	}
}

func TestFlatTraversalDoesNotUseRecursionPerStep(t *testing.T) {
	source := []byte("foo" + strings.Repeat(".bar", maxRecursiveExpressionDepth*8))
	file := parseExpressionSource(source)
	assertExpressionPartition(t, source, file)
	if len(file.diagnostics) != 0 {
		t.Fatalf("flat traversal hit nesting limit: %+v", file.diagnostics)
	}
}
