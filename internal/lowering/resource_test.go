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

func TestDeepOperationChains(t *testing.T) {
	const count = 20000
	for _, source := range []string{
		strings.Repeat("a + ", count) + "a",
		"root" + strings.Repeat(".attribute", count),
		"f(" + strings.Repeat("a + ", count) + "a)",
		"f(root" + strings.Repeat(".attribute[0]", count) + ")",
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

// Compare the canonical token/operation shape without invoking the normalizer.
// Pure quoted wrappers and ordinary legacy indices have equivalent spellings;
// all comments, literal bytes, operator nesting, and splat projection scopes must
// survive. Every significant node has an end marker so a suffix cannot silently
// move inside an operation or splat. Only parentheses and traversal containers
// are transparent: their canonical children fully describe the expression.
func expressionTokens(result syntax.Result, node syntax.SyntaxNode) []string {
	var tokens []string
	type entry struct {
		element             syntax.SyntaxElement
		objectSeparator     bool
		wrapper             bool
		legacyIndex         bool
		attributeProjection bool
		suffix              string
	}
	stack := []entry{{element: node.Element()}}
	for len(stack) != 0 {
		current := stack[len(stack)-1]
		element := current.element
		stack = stack[:len(stack)-1]
		if current.suffix != "" {
			tokens = append(tokens, current.suffix)
			continue
		}
		if node, ok := element.Node(); ok {
			wrapper := current.wrapper
			if node.Kind() == syntax.TemplateExpression && node.ChildCount() == 3 {
				open, _ := node.Child(0).Token()
				middle, _ := node.Child(1).Node()
				wrapper = open.Kind() == syntax.QuoteOpen && middle.Kind() == syntax.TemplateInterpolation
			}
			legacy := node.Kind() == syntax.LegacyIndexAccess && !current.attributeProjection
			if legacy {
				tokens = append(tokens, "node:IndexAccess", "[")
				stack = append(stack, entry{suffix: "end:IndexAccess"}, entry{suffix: "]"})
			} else if !wrapper && node.Kind() != syntax.ParenthesizedExpression && node.Kind() != syntax.TraversalExpression {
				tokens = append(tokens, "node:"+node.Kind().String())
				stack = append(stack, entry{suffix: "end:" + node.Kind().String()})
			}
			for i := node.ChildCount() - 1; i >= 0; i-- {
				if token, ok := node.Child(i).Token(); ok && node.Kind() == syntax.ParenthesizedExpression && (token.Kind() == syntax.OpenParen || token.Kind() == syntax.CloseParen) {
					continue
				}
				child, _ := node.Child(i).Node()
				stack = append(stack, entry{
					element: node.Child(i), objectSeparator: node.Kind() == syntax.ObjectItem,
					wrapper:     wrapper && (child.Kind() == syntax.TemplateInterpolation || child.Kind() == syntax.InvalidNode),
					legacyIndex: legacy, attributeProjection: node.Kind() == syntax.AttributeSplat,
				})
			}
			continue
		}
		token, _ := element.Token()
		if current.wrapper && (token.Kind() == syntax.QuoteOpen || token.Kind() == syntax.QuoteClose || token.Kind() == syntax.InterpolationOpen || token.Kind() == syntax.TemplateSequenceEnd || token.Kind() == syntax.StripMarker) {
			continue
		}
		if current.legacyIndex {
			if token.Kind() == syntax.Dot {
				continue
			}
			if token.Kind() == syntax.Number {
				tokens = append(tokens, "node:LiteralExpression")
			}
		}
		switch token.Kind() {
		case syntax.Whitespace, syntax.Newline, syntax.Comma:
			continue
		}
		if current.objectSeparator && token.Kind() == syntax.Colon {
			tokens = append(tokens, "=")
			continue
		}
		tokens = append(tokens, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
		if current.legacyIndex && token.Kind() == syntax.Number {
			tokens = append(tokens, "end:LiteralExpression")
		}
	}
	return tokens
}

func TestExpressionContentOraclePreservesScopes(t *testing.T) {
	for _, pair := range [][2]string{
		{`(a[*].b)[0]`, `a[*].b[0]`},
		{`(a.*.b).c`, `a.*.b.c`},
		{`(a[*][*].b)[0]`, `a[*][*].b[0]`},
		{`(a + b).c`, `a + b.c`},
		{`(-a).b`, `-a.b`},
	} {
		left, leftNode := parse(t, pair[0])
		right, rightNode := parse(t, pair[1])
		if reflect.DeepEqual(expressionTokens(left, leftNode), expressionTokens(right, rightNode)) {
			t.Errorf("oracle lost scope: %q and %q", pair[0], pair[1])
		}
	}
	for _, pair := range [][2]string{
		{`"${a[*].b}".0`, `(a[*].b)[0]`},
		{`"${a.*.b}".c`, `(a.*.b).c`},
		{`a[*].0.b`, `a[*][0].b`},
		{`"${a + b}".c`, `(a + b).c`},
	} {
		left, leftNode := parse(t, pair[0])
		right, rightNode := parse(t, pair[1])
		if !reflect.DeepEqual(expressionTokens(left, leftNode), expressionTokens(right, rightNode)) {
			t.Errorf("oracle rejected canonical equivalence: %q and %q", pair[0], pair[1])
		}
	}
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
