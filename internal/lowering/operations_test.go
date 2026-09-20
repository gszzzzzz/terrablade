package lowering_test

import (
	"testing"

	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/reference"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestOperationLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"binary spaces", "a+b*c", 80, "a + b * c"},
		{
			name:   "all binary operators",
			source: "a||b&&c==d!=e<f<=g>h>=i+j - k*l/m%n",
			width:  100,
			want:   "a || b && c == d != e < f <= g > h >= i + j - k * l / m % n",
		},
		{"left associative chain", "alpha + beta - gamma", 15, lines("(", "  alpha", "  + beta", "  -gamma", ")")},
		{"precedence group fits independently", "alpha + beta * gamma", 17, lines("(", "  alpha", "  + beta * gamma", ")")},
		{
			name:   "precedence group breaks independently",
			source: "alpha + beta * gamma",
			width:  12,
			want:   lines("(", "  alpha", "  + beta", "  * gamma", ")"),
		},
		{
			name:   "existing parentheses share inner group",
			source: "(alpha + beta - gamma)",
			width:  15,
			want:   lines("(", "  alpha", "  + beta", "  -gamma", ")"),
		},
		{"binary inside call already permits newlines", "f(alpha + beta)", 12, lines("f(", "  alpha", "  + beta,", ")")},
		{
			name:   "safe chain shares delimiter indent",
			source: "f(alpha + beta - gamma)",
			width:  14,
			want:   lines("f(", "  alpha", "  + beta", "  -gamma,", ")"),
		},
		{"line leading minus before parentheses", "a - (b - c)", 8, lines("(", "  a", "  -(", "    b", "    -c", "  )", ")")},
		{"line leading minus before comment", "(a - /*c*/ b)", 8, lines("(", "  a", "  -/*c*/ b", ")")},
		{"template mandatory minus line", lines(`"prefix ${a # c`, ` - b}"`), 80, lines(`"prefix ${a # c`, `  -b}"`)},
		{"template comment after minus", lines(`"prefix ${a - # c`, ` b}"`), 80, lines(`"prefix ${a - # c`, `  b}"`)},
		{"binary comment stays inline", lines("(alpha # why", " + beta)"), 80, lines("(", "  alpha # why", "  + beta", ")")},
		{
			name:   "binary comment after operator",
			source: lines("(alpha + # why", " beta)"),
			width:  80,
			want:   lines("(", "  alpha", "  + # why", "  beta", ")"),
		},
		{"binary block comment", "a/*x*/+/*y*/b", 80, "a /*x*/ + /*y*/ b"},
		{
			name:   "parenthesis boundary comments share group",
			source: "(/*lead*/alpha+beta/*tail*/)",
			width:  16,
			want:   lines("(", "  /*lead*/ alpha", "  + beta /*tail*/", ")"),
		},

		// Conditionals associate right and break arm by arm.
		{"conditional flat", "ready?yes:no", 80, "ready ? yes : no"},
		{
			name:   "conditional broken",
			source: "ready ? first_value : second_value",
			width:  20,
			want:   lines("(", "  ready", "  ? first_value", "  : second_value", ")"),
		},
		{"nested conditional associates right", "a?b:c?d:e", 13, lines("(", "  a", "  ? b", "  : c ? d : e", ")")},
		{"conditional inside tuple", "[ready ? yes : no]", 12, lines("[", "  ready", "  ? yes", "  : no,", "]")},

		// Traversal steps stay attached and never break for width.
		{"attribute traversal flat", "foo . bar . baz", 80, "foo.bar.baz"},
		{"attribute traversal overflows", "foo.first_attribute.second_attribute", 24, "foo.first_attribute.second_attribute"},
		{
			name:   "call and traversal groups independent",
			source: "f(x,y).first_attribute.second_attribute",
			width:  30,
			want:   lines("f(", "  x,", "  y,", ").first_attribute.second_attribute"),
		},
		{
			name:   "unary traversal preserves precedence",
			source: "-foo.first_attribute.second_attribute",
			width:  24,
			want:   "-foo.first_attribute.second_attribute",
		},
		{"index expression", "foo[ 1 + i ].true", 80, "foo[1 + i].true"},
		{"index expression breaks safely", "foo[alpha + beta]", 14, lines("foo[", "  alpha + beta", "]")},

		// Number/step boundaries: a space only where the scanner would fuse.
		{"legacy numeric boundary", "foo.0 .0", 80, "foo[0][0]"},
		{"legacy exponent boundary", "foo.0e1 .0", 80, "foo[0e1][0]"},
		{"numeric root exponent attribute", "1 . e2", 80, "1 .e2"},
		{"numeric root ordinary attribute", "1.foo", 80, "1.foo"},
		{"ordinary attribute after legacy index", "aws_instance.foo.0.id", 80, "aws_instance.foo[0].id"},
		{"ordinary attribute after legacy exponent", "foo.1e1.id", 80, "foo[1e1].id"},
		{"incomplete exponent attribute", "foo.0 .e", 80, "foo[0].e"},
		{"incomplete signed exponent attribute", "foo.0 .e-", 80, "foo[0].e-"},
		{"non numeric exponent suffix", "foo.0 .e-foo", 80, "foo[0].e-foo"},
		{"lowercase exponent attribute", "foo.0 .e2", 80, "foo[0].e2"},
		{"uppercase exponent attribute", "foo.0 .E2", 80, "foo[0].E2"},
		{"signed exponent attribute", "foo.0 .e-2", 80, "foo[0].e-2"},
		{"uppercase signed exponent attribute", "foo.0 .E-2", 80, "foo[0].E-2"},
		{"exponent prefix in longer attribute", "foo.0 .e2suffix", 80, "foo[0].e2suffix"},
		{"signed exponent prefix in longer attribute", "foo.0 .E-2suffix", 80, "foo[0].E-2suffix"},
		{"plus is a distinct token", "foo.0 .e+2", 80, "foo[0].e + 2"},
		{"spaced minus is a distinct token", "foo.0 .e - 2", 80, "foo[0].e - 2"},
		{"non ASCII exponent lookalike", "foo.0 .é2", 80, "foo[0].é2"},
		{"non ASCII digit does not extend number", "foo.0 .e٢", 80, "foo[0].e٢"},
		{"numeric root exponent continuation", "0.5 .e2", 80, "0.5 .e2"},
		{"legacy numeric after exponent index", "foo.1e1 .2", 80, "foo[1e1][2]"},
		{"splat after number cannot extend number", "foo.0.*.id", 80, "foo[0].*.id"},
		{"comment before exponent step separates number", "foo.0/*c*/.e2", 80, "foo[0] /*c*/.e2"},
		{"comment inside exponent step separates number", "foo.0./*c*/e2", 80, "foo[0]./*c*/ e2"},
		{"comment inside legacy index separates number", "foo.0./*c*/1", 80, "foo[0][/*c*/ 1]"},

		// Splat projections keep their scope.
		{"legacy splat scope", "foo.*.bar[0].baz", 80, "foo.*.bar[0].baz"},
		{"full splat scope", "foo[*].bar[0].baz", 80, "foo[*].bar[0].baz"},
		{"nested full splats", "foo[*][*].bar", 80, "foo[*][*].bar"},
		{"mixed splats", "foo.*.0[*].bar", 80, "foo.*.0[*].bar"},
		{"full then legacy splat", "foo[*].*.bar", 80, "foo[*].*.bar"},
		{"index between legacy splats", "foo.*.bar[0].*.baz", 80, "foo.*.bar[0].*.baz"},
		{"legacy splat numeric boundary", "foo.*.0 .0", 80, "foo.*.0 .0"},
		{
			name:   "full splat overflows",
			source: "foo[*].first_attribute[0].second_attribute",
			width:  24,
			want:   "foo[*].first_attribute[0].second_attribute",
		},
		{
			name:   "safe mixed traversal",
			source: "f(foo[0].first_attribute[*].second_attribute)",
			width:  24,
			want:   lines("f(", "  foo[0].first_attribute[*].second_attribute,", ")"),
		},
		{
			name:   "safe traversal in tuple",
			source: "[foo.first_attribute.second_attribute]",
			width:  24,
			want:   lines("[", "  foo.first_attribute.second_attribute,", "]"),
		},
		{
			name:   "mixed splats retain bracket attachment",
			source: "foo.*.first_attribute[0][*].second_attribute",
			width:  24,
			want:   "foo.*.first_attribute[0][*].second_attribute",
		},

		// Comments inside traversals supply their own boundary.
		{"traversal comment", lines("(foo # base", " .bar)"), 80, lines("(", "  foo # base", "  .bar", ")")},
		{"attribute internal comment", "foo./*step*/bar", 80, "foo./*step*/ bar"},
		{
			name:   "manual traversal lines collapse",
			source: lines("(foo", " .first_attribute", " .second_attribute)"),
			width:  80,
			want:   "(foo.first_attribute.second_attribute)",
		},
		{
			name:   "mandatory traversal line shares call indent",
			source: lines("f(foo # base", " .bar)"),
			width:  80,
			want:   lines("f(", "  foo # base", "  .bar,", ")"),
		},
		{"splat comment", "foo[/*splat*/ *].bar", 80, "foo[/*splat*/ *].bar"},
		{"splat closing comment", "foo[* /*splat*/].bar", 80, "foo[* /*splat*/].bar"},
		{"index comment", "foo[/*index*/ 0 /*tail*/]", 80, "foo[/*index*/ 0 /*tail*/]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := render(t, test.source, test.width)
			if got != test.want {
				t.Fatalf(lines("rendered:", "%q", "want:", "%q"), got, test.want)
			}
			if again := render(t, got, test.width); again != got {
				t.Fatalf(lines("not idempotent:", "%q", "then:", "%q"), got, again)
			}
		})
	}
}

func TestOperationReferenceCompatibility(t *testing.T) {
	reference.CLI(t)
	for _, source := range []string{
		"foo.first_attribute.second_attribute", "f(x,y).first_attribute.second_attribute",
		"-foo.first_attribute.second_attribute", "foo[index_value].attribute_name",
		"foo[alpha+beta].attribute_name", "foo[*].first_attribute[0].second_attribute",
		"foo.*.first_attribute[0][*].second_attribute", "foo[/*index*/0/*tail*/].bar",
		"aws_instance.foo.0.id", "foo.1e1.id", "foo./*step*/bar",
		lines("(foo", " .first_attribute", " .second_attribute)"),
		"f(foo[0].first_attribute[*].second_attribute)", "[foo.first_attribute.second_attribute]",
		lines("f(foo # base", " .bar)"), lines("[foo # base", " .bar]"), lines("(foo # base", " .bar)"),
		lines("(foo /*base*/", " .bar)"), lines("(foo.", "/*step*/bar)"), "foo[* /*splat*/].bar",
		"f(alpha + beta)", "[ready ? yes : no]", "alpha + beta * gamma",
		"alpha + beta - gamma", "a - (b - c)", "(a - /*c*/ b)",
		"[for x in ready ? first : second : x]", "{for x in xs:long_key=>long_value}",
	} {
		for _, width := range []int{1, 16, 80} {
			output := renderFile(t, "value = "+source+"\n", width)
			assertReferenceFormat(t, output)
			if again := renderFile(t, output, width); again != output {
				t.Fatalf("not idempotent: %q => %q", output, again)
			}
		}
	}
}

// binaryOperators lists every binary operator with the token kind lowering's
// ladder maps it through. The spellings drive the parser probes below.
var binaryOperators = []struct {
	text string
	kind syntax.TokenKind
}{
	{"||", syntax.Or}, {"&&", syntax.And},
	{"==", syntax.EqualEqual}, {"!=", syntax.NotEqual},
	{"<", syntax.Less}, {"<=", syntax.LessEqual},
	{">", syntax.Greater}, {">=", syntax.GreaterEqual},
	{"+", syntax.Plus}, {"-", syntax.Minus},
	{"*", syntax.Star}, {"/", syntax.Slash}, {"%", syntax.Percent},
}

// TestBindingPowersMatchParserPrecedence checks lowering's binding-power
// ladder against the parser's precedence. The parser's own table is
// unexported and lives in another package, so the only honest comparison runs
// through the public parser: each probe parses an expression whose tree shape
// is decided purely by precedence, and the shape is then read back as an
// ordering. If the parser's precedence ever changes, normalization would
// start adding or dropping parentheses without this test, since needsGrouping
// and expressionPower decide that from the ladder alone.
func TestBindingPowersMatchParserPrecedence(t *testing.T) {
	t.Run("binary pairs", func(t *testing.T) {
		for _, first := range binaryOperators {
			for _, second := range binaryOperators {
				name := first.text + " vs " + second.text
				t.Run(name, func(t *testing.T) {
					want := compare(lowering.BinaryPower(first.kind), lowering.BinaryPower(second.kind))
					if got := parserBinaryOrder(t, first.text, second.text); got != want {
						t.Fatalf("parser orders %s as %d, ladder says %d", name, got, want)
					}
				})
			}
		}
	})

	t.Run("unary binds tighter than binary", func(t *testing.T) {
		for _, operator := range binaryOperators {
			// -a op b parses as (-a) op b exactly when unary binds tighter;
			// a looser unary would take the whole binary as its operand.
			root := parseExpression(t, "-a "+operator.text+" b")
			if root.Kind() != syntax.BinaryExpression {
				t.Fatalf("-a %s b parsed as %s, so unary binds no tighter", operator.text, root.Kind())
			}
			if operand := nodeChildren(t, root)[0]; operand.Kind() != syntax.UnaryExpression {
				t.Fatalf("-a %s b has %s on the left, want UnaryExpression", operator.text, operand.Kind())
			}
			if lowering.UnaryPower <= lowering.BinaryPower(operator.kind) {
				t.Fatalf("ladder puts unary at or below %s", operator.text)
			}
		}
	})

	t.Run("traversal binds tighter than unary and binary", func(t *testing.T) {
		for _, operator := range binaryOperators {
			// a op b.c keeps the step on b alone; a looser traversal would
			// take the binary expression as its root.
			root := parseExpression(t, "a "+operator.text+" b.c")
			if root.Kind() != syntax.BinaryExpression {
				t.Fatalf("a %s b.c parsed as %s, so traversal binds no tighter", operator.text, root.Kind())
			}
			if operand := nodeChildren(t, root)[1]; operand.Kind() != syntax.TraversalExpression {
				t.Fatalf("a %s b.c has %s on the right, want TraversalExpression", operator.text, operand.Kind())
			}
			if lowering.TraversalPower <= lowering.BinaryPower(operator.kind) {
				t.Fatalf("ladder puts traversal at or below %s", operator.text)
			}
		}
		root := parseExpression(t, "-a.b")
		if root.Kind() != syntax.UnaryExpression || nodeChildren(t, root)[0].Kind() != syntax.TraversalExpression {
			t.Fatalf("-a.b parsed as %s, want a unary expression over a traversal", root.Kind())
		}
		if lowering.TraversalPower <= lowering.UnaryPower {
			t.Fatal("ladder puts traversal at or below unary")
		}
	})

	t.Run("conditional binds loosest", func(t *testing.T) {
		for _, operator := range binaryOperators {
			// Both arms and the condition swallow a whole binary expression,
			// which is what "the conditional binds loosest" means.
			for _, source := range []string{
				"a " + operator.text + " b ? c : d",
				"a ? b " + operator.text + " c : d",
				"a ? b : c " + operator.text + " d",
			} {
				root := parseExpression(t, source)
				if root.Kind() != syntax.ConditionalExpression {
					t.Fatalf("%q parsed as %s, want ConditionalExpression", source, root.Kind())
				}
			}
			if lowering.ConditionalPower >= lowering.BinaryPower(operator.kind) {
				t.Fatalf("ladder puts conditional at or above %s", operator.text)
			}
		}
		if root := parseExpression(t, "-a ? b : c"); root.Kind() != syntax.ConditionalExpression {
			t.Fatalf("-a ? b : c parsed as %s, want ConditionalExpression", root.Kind())
		}
		if lowering.ConditionalPower >= lowering.UnaryPower {
			t.Fatal("ladder puts conditional at or above unary")
		}
	})

	t.Run("delimited forms are atomic", func(t *testing.T) {
		// A parenthesized operation is an operand of the step that follows
		// it, so nothing binds tighter than a delimited form.
		root := parseExpression(t, "(a + b).c")
		if root.Kind() != syntax.TraversalExpression || nodeChildren(t, root)[0].Kind() != syntax.ParenthesizedExpression {
			t.Fatalf("(a + b).c parsed as %s, want a traversal rooted at a parenthesized expression", root.Kind())
		}
		if lowering.AtomicPower <= lowering.TraversalPower {
			t.Fatal("ladder puts atomic forms at or below traversal")
		}
	})
}

// parserBinaryOrder reports how the parser orders two binary operators: -1
// when first binds more loosely than second, 0 when they share a level, and
// +1 when first binds more tightly.
//
// One probe cannot tell equal from looser, because HCL's binary operators are
// left associative: both "a first b second c" shapes group the left pair. Two
// probes with the operators swapped separate the three cases, since each says
// only "the outer operator binds no more tightly than the inner one".
func parserBinaryOrder(t *testing.T, first, second string) int {
	t.Helper()
	firstAtLeastSecond := groupsLeft(t, "a "+first+" b "+second+" c")
	secondAtLeastFirst := groupsLeft(t, "a "+second+" b "+first+" c")
	switch {
	case firstAtLeastSecond && secondAtLeastFirst:
		return 0
	case firstAtLeastSecond:
		return 1
	case secondAtLeastFirst:
		return -1
	default:
		t.Fatalf("neither %q nor %q groups on the left", first, second)
		return 0
	}
}

// groupsLeft reports whether source parsed as (a op b) op c rather than
// a op (b op c), which is how the parser records that the first operator
// bound at least as tightly as the second.
func groupsLeft(t *testing.T, source string) bool {
	t.Helper()
	root := parseExpression(t, source)
	if root.Kind() != syntax.BinaryExpression {
		t.Fatalf("%q parsed as %s, want BinaryExpression", source, root.Kind())
	}
	operands := nodeChildren(t, root)
	if len(operands) != 2 {
		t.Fatalf("%q has %d operands, want 2", source, len(operands))
	}
	switch {
	case operands[0].Kind() == syntax.BinaryExpression:
		return true
	case operands[1].Kind() == syntax.BinaryExpression:
		return false
	default:
		t.Fatalf("%q nests no binary operand", source)
		return false
	}
}

// parseExpression parses one expression through the public parser and returns
// its root node.
func parseExpression(t *testing.T, source string) syntax.SyntaxNode {
	t.Helper()
	_, node := parse(t, source)
	return node
}

// nodeChildren returns a node's child nodes in source order, skipping tokens.
func nodeChildren(t *testing.T, node syntax.SyntaxNode) []syntax.SyntaxNode {
	t.Helper()
	var children []syntax.SyntaxNode
	for i := range node.ChildCount() {
		if child, ok := node.Child(i).Node(); ok {
			children = append(children, child)
		}
	}
	if len(children) == 0 {
		t.Fatalf("%s has no child nodes", node.Kind())
	}
	return children
}

// compare returns the sign of left - right.
func compare(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}
