package lowering_test

import (
	"testing"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestExpressionLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"number spelling", "012.30E-2", 80, "012.30E-2"},
		{"empty fraction", "1.e+2", 80, "1.e+2"},
		{"keyword literal", "null", 80, "null"},
		{"unicode name", "이름", 1, "이름"},
		{"unary", "! - - 1", 80, "!--1"},
		{"explicit parentheses", "( ( x ) )", 80, "((x))"},
		{"parentheses do not create width breaks", "(long_name)", 8, "(long_name)"},

		// Calls and tuples: flat fits, broken layouts, trailing commas.
		{"empty call", "f( )", 1, "f()"},
		{"namespaced call", "provider :: aws :: f ( x , y , )", 80, "provider::aws::f(x, y)"},
		{"call exact fit", "f(x,y,)", 7, "f(x, y)"},
		{"call breaks", "f(x,y)", 6, lines("f(", "  x,", "  y,", ")")},
		{"expanded call flat", "f(x,xs ... )", 80, "f(x, xs...)"},
		{"expanded call broken", "f(x,xs ... )", 6, lines("f(", "  x,", "  xs...", ")")},
		{"expanded call comment suffix is tight", "f(xs /* expand */ ...)", 80, "f(xs /* expand */...)"},
		{"tuple flat", "[1,true,null,]", 80, "[1, true, null]"},
		{"tuple broken", "[1,true,null]", 8, lines("[", "  1,", "  true,", "  null,", "]")},
		{"empty tuple", "[ ]", 1, "[]"},
		{"nested groups", "f([a,b], [long_name])", 12, lines("f(", "  [a, b],", "  [", "    long_name,", "  ],", ")")},
		{"nested call stays flat", "f(g(x),h(y))", 10, lines("f(", "  g(x),", "  h(y),", ")")},
		{"source newlines collapse", lines("[", "", "1,", "2,", "]"), 80, "[1, 2]"},

		// Comments: source order, inline placement, literal spelling.
		{"inline block comment", "f(a/*one*/,/*two*/b)", 80, "f(a, /*one*/ /*two*/ b)"},
		{"line comment", lines("f(a, # keep", " b)"), 80, lines("f(", "  a, # keep", "  b,", ")")},
		{"slash comment spelling", lines("[a,// keep", "b]"), 80, lines("[", "  a, // keep", "  b,", "]")},
		{"line comment before comma", lines("f(a # keep", ",b)"), 80, lines("f(", "  a, # keep", "  b,", ")")},
		{"trailing comment", lines("[a # keep", "]"), 80, lines("[", "  a, # keep", "]")},
		{"trailing comment after comma", lines("[a, # keep", "]"), 80, lines("[", "  a, # keep", "]")},
		{"empty with line comment", lines("[ # empty", " ]"), 80, lines("[", "  # empty", "]")},
		{"multiple comments", "f(/*a*/ /*b*/x)", 80, "f(/*a*/ /*b*/ x)"},
		{
			name:   "multiline comment literal",
			source: "f(a/* first\r\n  second\nthird */, b)",
			width:  80,
			want:   lines("f(", "  a, /* first", "  second", "third */", "  b,", ")"),
		},

		// Escaped below: a lone CR is literal comment content, not a break.
		{"lone CR comment", "[a/*x\ry*/,b]", 80, "[a, /*x\ry*/ b]"},
		{"lone CR before CRLF remains stable", "f(a/*\r\r\n*/)", 80, "f(\n  a, /*\r\r\n*/\n)"},

		// Comments at the boundaries of each form.
		{"unary comments", "! /* note */ false", 80, "! /* note */ false"},
		{"namespace comments", "a/*x*/::/*y*/b/*z*/(x)", 80, "a /*x*/ :: /*y*/ b /*z*/ (x)"},
		{"block comment before closer", "f(a/* tail */)", 80, "f(a /* tail */)"},
		{"parenthesized block comment before closer", "(a/* tail */)", 1, "(a /* tail */)"},
		{"parenthesized opener comment stays compact", "(/* lead */a)", 1, "(/* lead */ a)"},
		{
			name:   "multiline block comment before closer is tight",
			source: lines("(a /* first", " second */)"),
			width:  80,
			want:   lines("(a /* first", " second */)"),
		},
		{"tuple separator follows trailing block comment", "[a /* c */,b]", 80, "[a, /* c */ b]"},
		{
			name:   "tuple separator follows trailing line comment",
			source: lines("[a # keep", ",b]"),
			width:  80,
			want:   lines("[", "  a, # keep", "  b,", "]"),
		},
		{
			name:   "standalone line comment stays standalone",
			source: lines("[a,", "# keep", "b]"),
			width:  80,
			want:   lines("[", "  a,", "  # keep", "  b,", "]"),
		},
		{"opener block comment has no flat padding", "[/* lead */ a]", 80, "[/* lead */ a]"},
		{"opener block comment breaks before comment", "[/* lead */ a]", 8, lines("[", "  /* lead */ a,", "]")},
		{"empty block comment has no flat padding", "[/* empty */]", 80, "[/* empty */]"},
		{"empty block comment breaks before comment", "[/* empty */]", 8, lines("[", "  /* empty */", "]")},

		// Blank lines survive in broken entry gaps, never in calls.
		{"blank line collapses flat", lines("[a,", "", "b]"), 80, "[a, b]"},
		{"blank line survives broken tuple", lines("[alpha,", "", "beta]"), 8, lines("[", "  alpha,", "", "  beta,", "]")},
		{"multiple blank lines capped", lines("[alpha,", "", "", "", "beta]"), 8, lines("[", "  alpha,", "", "  beta,", "]")},
		{"blank lines collapse in call", lines("f(alpha,", "", "beta)"), 8, lines("f(", "  alpha,", "  beta,", ")")},
		{
			name:   "blank before standalone comment",
			source: lines("[a,", "", "# note", "b]"),
			width:  80,
			want:   lines("[", "  a,", "", "  # note", "  b,", "]"),
		},
		{"blank after inline comment", lines("[a, # note", "", "b]"), 80, lines("[", "  a, # note", "", "  b,", "]")},
		{
			name:   "blank around standalone comment capped",
			source: lines("[a,", "", "", "# note", "", "", "b]"),
			width:  80,
			want:   lines("[", "  a,", "", "  # note", "", "  b,", "]"),
		},
		{
			name:   "call comment blank lines collapse",
			source: lines("f(a,", "", "# note", "", "b)"),
			width:  80,
			want:   lines("f(", "  a,", "  # note", "  b,", ")"),
		},
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

func TestExpressionRejectsInvalidInput(t *testing.T) {
	valid, node := parse(t, "x")
	invalid := syntax.Parse([]byte(lines("a = x", "b =", "")))
	for _, test := range []struct {
		name   string
		result syntax.Result
		node   syntax.SyntaxNode
	}{
		{"zero result and node", syntax.Result{}, syntax.SyntaxNode{}},
		{"zero node", valid, syntax.SyntaxNode{}},
		{"file instead of expression", valid, valid.Root()},
		{"diagnostic elsewhere", invalid, firstExpression(invalid)},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := lowering.Expression(test.result, test.node)
			if err == nil || document.Render(doc, document.Options{}) != "" {
				t.Fatalf("expected error and empty Doc, got %v", err)
			}
		})
	}
	if _, err := lowering.Expression(valid, node); err != nil {
		t.Fatal(err)
	}
}

func TestEnclosingTriviaIsNotLowered(t *testing.T) {
	result := syntax.Parse([]byte(lines("# before", "a = /* before value */ f(x) # after", "")))
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	doc, err := lowering.Expression(result, firstExpression(result))
	if err != nil {
		t.Fatal(err)
	}
	if got := document.Render(doc, document.Options{}); got != "f(x)" {
		t.Fatalf("render = %q", got)
	}
}
