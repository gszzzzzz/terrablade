package lowering_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"terrablade/internal/document"
	"terrablade/internal/lowering"
	"terrablade/internal/syntax"
)

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
			t.Skip() // This increment deliberately rejects unsupported forms.
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

func TestDeepOperationChains(t *testing.T) {
	const count = 20000
	for _, source := range []string{
		strings.Repeat("a + ", count) + "a",
		"root" + strings.Repeat(".attribute", count),
	} {
		result, node := parse(t, source)
		output := render(t, source, 30)
		reparsed, next := parse(t, output)
		if !reflect.DeepEqual(expressionTokens(result, node), expressionTokens(reparsed, next)) {
			t.Fatal("deep operation changed expression structure")
		}
		if again := render(t, output, 30); again != output {
			t.Fatal("deep operation is not idempotent")
		}
	}
}

func BenchmarkOperationChains(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000} {
		for name, source := range map[string]string{
			"binary":    strings.Repeat("a + ", count) + "a",
			"traversal": "root" + strings.Repeat(".attribute", count),
		} {
			b.Run(name+"/"+strconv.Itoa(count), func(b *testing.B) {
				result, node := parse(b, source)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					doc, err := lowering.Expression(result, node)
					if err != nil {
						b.Fatal(err)
					}
					document.Render(doc, document.Options{PrintWidth: 30})
				}
			})
		}
	}
}

// Commas, trivia, and parentheses may be canonicalized; all other token spelling
// and the expression tree shape must survive. Ignoring parentheses in the shape
// still detects a precedence change, because the operator tree would differ.
func expressionTokens(result syntax.Result, node syntax.SyntaxNode) []string {
	var tokens []string
	stack := []syntax.SyntaxElement{node.Element()}
	for len(stack) != 0 {
		element := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node, ok := element.Node(); ok {
			if node.Kind() != syntax.ParenthesizedExpression {
				tokens = append(tokens, "node:"+node.Kind().String())
			}
			for i := node.ChildCount() - 1; i >= 0; i-- {
				if token, ok := node.Child(i).Token(); ok && node.Kind() == syntax.ParenthesizedExpression && (token.Kind() == syntax.OpenParen || token.Kind() == syntax.CloseParen) {
					continue
				}
				stack = append(stack, node.Child(i))
			}
			continue
		}
		token, _ := element.Token()
		switch token.Kind() {
		case syntax.Whitespace, syntax.Newline, syntax.Comma:
			continue
		}
		tokens = append(tokens, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
	}
	return tokens
}

func BenchmarkExpression(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			result, node := parse(b, "["+strings.Repeat("f(x),", count)+"]")
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
