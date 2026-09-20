package lowering_test

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// The seed corpus stays in escaped form. A seed is opaque input for the
// fuzzer rather than a layout expectation, and the dense list is easier to
// scan for coverage gaps than one call per line would be.
func FuzzExpression(f *testing.F) {
	for _, source := range []string{
		"f(a,b,)", "[-1,true,null]", "provider::f(xs...)",
		"f(a # comment\n,b)", "f(a, /*x\ry*/ b)", "(/*a*/x/*b*/)",
		"[ # empty\n]", "f([a,b], g(x))", "f(a/*x\r\ny*/,b)",
		"[alpha,\n\nbeta]", "[a,\n\n# note\n\nb]", "(long_name)",
		"[a /* c */,b]", "[/* lead */ a]", "[a # keep\n,b]",
		"alpha + beta - gamma", "ready ? yes : no", "a?b:c?d:e",
		"foo.0 .0", "1 .e2", "f(x,y).first_attribute.second_attribute",
		"foo.*.bar[0].baz", "foo[*][*].bar", "foo[alpha + beta]",
		"(/*lead*/alpha+beta/*tail*/)", "foo.*.0 .0", "foo[* /*c*/].bar",
		"foo.0 .e-2suffix", "foo.0 .E2", "foo.0 .e+2", "foo.0 .e-",
		"aws_instance.foo.0.id", "foo.1e1.id", "foo.0 .e٢",
		"foo.0/*c*/.e2", "foo.0./*c*/e2", "foo.0./*c*/1",
		"f(foo[0].first_attribute[*].second_attribute)",
		"{a:1,b=2}", "{a=1\n\nb=2}", "{alpha + beta=1}", "[{key=alpha+beta}]",
		"[for x in xs:x.id if x.enabled]", "{for k,v in xs:k=>v... if v}", "[for in in if:if if in]",
		`"hello ${ a+b } end"`, `"%{~if a~}x%{endif}"`, `"%{for k,v in xs}${v}%{endfor}"`,
		"<<-E\n  ${a+b}\nE\n", "f(<<E\nx\nE\n,1)", "\"${<<E\nx\nE\n}\"",
		"<<E\nx\r\r\nE\r\r\n",
		"{x=<<E\nx\nE\ny=2}", "{x=<<E\nx\nE\n}", "{x=<<E\nx\nE\n\n# next\ny=2}",
		"{\na=1\n}", "{\n}", "[<<E\nx\nE\n,1]", "f(<<E\nx\nE\n,)",
		"\"${{\na=1\n}}\"", "\"%{if {\na=1\n}}yes%{endif}\"",
		`"${a}"`, `"${"${a}"}"`, `"${~a~}"`, `-"${a+b}"`, `a-"${b-c}"`,
		`{"${a}"="${b}"}`, `"${a[*].b}"[0]`, `"${a.*.b}".c`, `["${for}"]`,
		`"${/*a*/"${/*b*/x/*c*/}"/*d*/}"`, `foo[*].0.bar`, `foo.*.0[0].1`,
		"\"${# lead\na # tail\n}\"", "\"${foo # c\n.bar}\"", "\"${! # c\na}\"",
		"\"${<<E\nx\nE\n[0]}\"", "foo[0]./*c*/1", "f(foo.// c\n1)",
		"f(a, #x\r\r\nb)", "\"${a #x\r\r\n}\"", `{"${"k"}"=1}`,
	} {
		f.Add(source, uint8(20))
	}
	f.Fuzz(func(t *testing.T, source string, width uint8) {
		if len(source) > 8192 {
			t.Skip()
		}
		result := syntax.Parse([]byte("value = " + source + "\n"))
		if len(result.Diagnostics()) != 0 {
			t.Skip()
		}
		node := firstExpression(result)
		doc, err := lowering.Expression(result, node)
		if err != nil {
			t.Fatal(err)
		}
		options := document.Options{PrintWidth: int(width) + 1}
		output := document.Render(doc, options)
		reparsed, next := parse(t, output)
		if before, after := expressionTokens(result, node), expressionTokens(reparsed, next); !reflect.DeepEqual(before, after) {
			t.Fatalf("significant token/comment content changed: %q => %q\n%q => %q", source, output, before, after)
		}
		again, err := lowering.Expression(reparsed, next)
		if err != nil {
			t.Fatal(err)
		}
		if second := document.Render(again, options); second != output {
			t.Fatalf("not idempotent: %q => %q", output, second)
		}
	})
}

func TestWideObjectsAndDeepTemplateScopes(t *testing.T) {
	for _, source := range []string{
		"{" + strings.Repeat("key=1\n", 10000) + "}",
		`"` + strings.Repeat("%{if true}", 20000) + "body" + strings.Repeat("%{endif}", 20000) + `"`,
		// Nested expression containers respect the parser's nesting budget.
		`"${` + strings.Repeat("{\nkey=", 512) + "0" + strings.Repeat("\n}", 512) + `}"`,
	} {
		result, node := parse(t, source)
		output := render(t, source, 40)
		reparsed, next := parse(t, output)
		if !reflect.DeepEqual(expressionTokens(result, node), expressionTokens(reparsed, next)) {
			t.Fatal("large expression changed syntax or literal content")
		}
		if again := render(t, output, 40); again != output {
			t.Fatal("large expression is not idempotent")
		}
	}
}

func TestWideExpression(t *testing.T) {
	const count = 10000
	source := wideExpression(count)
	got := render(t, source, 30)
	if strings.Count(got, wideExpressionEntry) != count {
		t.Fatal("wide tuple lost entries")
	}
	if again := render(t, got, 30); again != got {
		t.Fatal("wide tuple is not idempotent")
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

func BenchmarkExpression(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			result, node := parse(b, wideExpression(count))
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := lowering.Expression(result, node); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
