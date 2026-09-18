package lowering_test

import (
	"strings"
	"sync"
	"testing"

	"terrablade/internal/document"
	"terrablade/internal/lowering"
	"terrablade/internal/syntax"
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
		{"empty call", "f( )", 1, "f()"},
		{"namespaced call", "provider :: aws :: f ( x , y , )", 80, "provider::aws::f(x, y)"},
		{"call exact fit", "f(x,y,)", 7, "f(x, y)"},
		{"call breaks", "f(x,y)", 6, "f(\n  x,\n  y,\n)"},
		{"expanded call flat", "f(x,xs ... )", 80, "f(x, xs...)"},
		{"expanded call broken", "f(x,xs ... )", 6, "f(\n  x,\n  xs...\n)"},
		{"expanded call comment suffix is tight", "f(xs /* expand */ ...)", 80, "f(xs /* expand */...)"},
		{"tuple flat", "[1,true,null,]", 80, "[1, true, null]"},
		{"tuple broken", "[1,true,null]", 8, "[\n  1,\n  true,\n  null,\n]"},
		{"empty tuple", "[ ]", 1, "[]"},
		{"nested groups", "f([a,b], [long_name])", 12, "f(\n  [a, b],\n  [\n    long_name,\n  ],\n)"},
		{"nested call stays flat", "f(g(x),h(y))", 10, "f(\n  g(x),\n  h(y),\n)"},
		{"source newlines collapse", "[\n\n1,\n2,\n]", 80, "[1, 2]"},
		{"inline block comment", "f(a/*one*/,/*two*/b)", 80, "f(a, /*one*/ /*two*/ b)"},
		{"line comment", "f(a, # keep\n b)", 80, "f(\n  a, # keep\n  b,\n)"},
		{"slash comment spelling", "[a,// keep\nb]", 80, "[\n  a, // keep\n  b,\n]"},
		{"line comment before comma", "f(a # keep\n,b)", 80, "f(\n  a, # keep\n  b,\n)"},
		{"trailing comment", "[a # keep\n]", 80, "[\n  a, # keep\n]"},
		{"trailing comment after comma", "[a, # keep\n]", 80, "[\n  a, # keep\n]"},
		{"empty with line comment", "[ # empty\n ]", 80, "[\n  # empty\n]"},
		{"multiple comments", "f(/*a*/ /*b*/x)", 80, "f(/*a*/ /*b*/ x)"},
		{"multiline comment literal", "f(a/* first\r\n  second\nthird */, b)", 80, "f(\n  a, /* first\n  second\nthird */\n  b,\n)"},
		{"lone CR comment", "[a/*x\ry*/,b]", 80, "[a, /*x\ry*/ b]"},
		{"lone CR before CRLF remains stable", "f(a/*\r\r\n*/)", 80, "f(\n  a, /*\r\r\n*/\n)"},
		{"unary comments", "! /* note */ false", 80, "! /* note */ false"},
		{"namespace comments", "a/*x*/::/*y*/b/*z*/(x)", 80, "a /*x*/ :: /*y*/ b /*z*/ (x)"},
		{"block comment before closer", "f(a/* tail */)", 80, "f(a /* tail */)"},
		{"parenthesized block comment before closer", "(a/* tail */)", 1, "(a /* tail */)"},
		{"parenthesized opener comment stays compact", "(/* lead */a)", 1, "(/* lead */ a)"},
		{"multiline block comment before closer is tight", "(a /* first\n second */)", 80, "(a /* first\n second */)"},
		{"tuple separator follows trailing block comment", "[a /* c */,b]", 80, "[a, /* c */ b]"},
		{"tuple separator follows trailing line comment", "[a # keep\n,b]", 80, "[\n  a, # keep\n  b,\n]"},
		{"standalone line comment stays standalone", "[a,\n# keep\nb]", 80, "[\n  a,\n  # keep\n  b,\n]"},
		{"opener block comment has no flat padding", "[/* lead */ a]", 80, "[/* lead */ a]"},
		{"opener block comment breaks before comment", "[/* lead */ a]", 8, "[\n  /* lead */ a,\n]"},
		{"empty block comment has no flat padding", "[/* empty */]", 80, "[/* empty */]"},
		{"empty block comment breaks before comment", "[/* empty */]", 8, "[\n  /* empty */\n]"},
		{"blank line collapses flat", "[a,\n\nb]", 80, "[a, b]"},
		{"blank line survives broken tuple", "[alpha,\n\nbeta]", 8, "[\n  alpha,\n\n  beta,\n]"},
		{"multiple blank lines capped", "[alpha,\n\n\n\nbeta]", 8, "[\n  alpha,\n\n  beta,\n]"},
		{"blank lines collapse in call", "f(alpha,\n\nbeta)", 8, "f(\n  alpha,\n  beta,\n)"},
		{"blank before standalone comment", "[a,\n\n# note\nb]", 80, "[\n  a,\n\n  # note\n  b,\n]"},
		{"blank after inline comment", "[a, # note\n\nb]", 80, "[\n  a, # note\n\n  b,\n]"},
		{"blank around standalone comment capped", "[a,\n\n\n# note\n\n\nb]", 80, "[\n  a,\n\n  # note\n\n  b,\n]"},
		{"call comment blank lines collapse", "f(a,\n\n# note\n\nb)", 80, "f(\n  a,\n  # note\n  b,\n)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := render(t, test.source, test.width)
			if got != test.want {
				t.Fatalf("rendered:\n%q\nwant:\n%q", got, test.want)
			}
			if again := render(t, got, test.width); again != got {
				t.Fatalf("not idempotent:\n%q\nthen:\n%q", got, again)
			}
		})
	}
}

func TestExpressionRejectsInvalidInput(t *testing.T) {
	valid, node := parse(t, "x")
	invalid := syntax.Parse([]byte("a = x\nb =\n"))
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

func TestUnsupportedExpressionIsNotPartiallyFormatted(t *testing.T) {
	for _, source := range []string{`"literal"`} {
		result, node := parse(t, source)
		doc, err := lowering.Expression(result, node)
		if err == nil || document.Render(doc, document.Options{}) != "" {
			t.Errorf("%q: expected error and empty Doc, got %v", source, err)
		}
	}
}

func TestEnclosingTriviaIsNotLowered(t *testing.T) {
	result := syntax.Parse([]byte("# before\na = /* before value */ f(x) # after\n"))
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

func TestConcurrentLoweringAndRendering(t *testing.T) {
	source := []byte("a = f([1,2], -3)\n")
	result := syntax.Parse(source)
	node := firstExpression(result)
	for i := range source {
		source[i] = 0
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			doc, err := lowering.Expression(result, node)
			if err != nil {
				t.Error(err)
				return
			}
			if got := document.Render(doc, document.Options{}); got != "f([1, 2], -3)" {
				t.Errorf("render = %q", got)
			}
		})
	}
	wg.Wait()
}

func TestWideExpression(t *testing.T) {
	const count = 10000
	source := "[" + strings.Repeat("f(x),", count) + "]"
	got := render(t, source, 30)
	if strings.Count(got, "f(x),") != count {
		t.Fatal("wide tuple lost entries")
	}
	if again := render(t, got, 30); again != got {
		t.Fatal("wide tuple is not idempotent")
	}
}

func render(t testing.TB, source string, width int) string {
	t.Helper()
	result, node := parse(t, source)
	doc, err := lowering.Expression(result, node)
	if err != nil {
		t.Fatal(err)
	}
	return document.Render(doc, document.Options{PrintWidth: width})
}

func parse(t testing.TB, source string) (syntax.Result, syntax.SyntaxNode) {
	t.Helper()
	result := syntax.Parse([]byte("value = " + source + "\n"))
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("invalid expression %q: %+v", source, diagnostics)
	}
	return result, firstExpression(result)
}

func firstExpression(result syntax.Result) syntax.SyntaxNode {
	root := result.Root()
	for i := 0; i < root.ChildCount(); i++ {
		body, ok := root.Child(i).Node()
		if !ok || body.Kind() != syntax.Body {
			continue
		}
		for j := 0; j < body.ChildCount(); j++ {
			attribute, ok := body.Child(j).Node()
			if !ok || attribute.Kind() != syntax.Attribute {
				continue
			}
			for k := 0; k < attribute.ChildCount(); k++ {
				if value, ok := attribute.Child(k).Node(); ok {
					return value
				}
			}
		}
	}
	return syntax.SyntaxNode{}
}
