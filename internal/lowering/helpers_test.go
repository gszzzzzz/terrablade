// Package lowering_test is split by unit and by the kind of check a file
// makes, so a reader can tell from a file name what it will find:
//
//   - <unit>_test.go holds behavior: layout tables, comment placement,
//     normalization results, and the errors the entry points return.
//   - <unit>_resource_test.go holds limits and scaling: fuzz targets,
//     deep/wide inputs, concurrency, and benchmarks.
//   - reference_test.go holds the reference-CLI oracle: asserting that the
//     terraform or tofu located by internal/reference accepts this package's
//     output unchanged.
//   - helpers_test.go (this file) holds the helpers those files share.
//
// A helper used by exactly one file stays in that file.
package lowering_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// lines joins HCL lines with LF so a case shows its own line structure. A
// trailing "" supplies the final LF that a complete file ends with, and an
// interior "" is a blank line. Cases whose exact bytes are the point (CRLF,
// a lone CR, a BOM, tabs, or HCL escape sequences) keep their escaped
// spelling instead, because lines would hide the bytes under test.
func lines(parts ...string) string {
	return strings.Join(parts, "\n")
}

// render lowers one expression through lowering.Expression and renders it at
// width.
func render(t testing.TB, source string, width int) string {
	t.Helper()
	result, node := parse(t, source)
	doc, err := lowering.Expression(result, node)
	if err != nil {
		t.Fatal(err)
	}
	return document.Render(doc, document.Options{PrintWidth: width})
}

// parse parses source as an attribute value and returns the result together
// with that value's node, which is what lowering.Expression accepts.
func parse(t testing.TB, source string) (syntax.Result, syntax.SyntaxNode) {
	t.Helper()
	result := syntax.Parse([]byte("value = " + source + "\n"))
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("invalid expression %q: %+v", source, diagnostics)
	}
	return result, firstExpression(result)
}

// firstExpression returns the value of the first attribute in result, or a
// zero node when there is none.
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

// renderFile lowers a complete file through lowering.File and renders it at
// width.
func renderFile(t testing.TB, source string, width int) string {
	t.Helper()
	result := syntax.Parse([]byte(source))
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("invalid file %q: %+v", source, diagnostics)
	}
	doc, err := lowering.File(result)
	if err != nil {
		t.Fatal(err)
	}
	return document.Render(doc, document.Options{PrintWidth: width})
}

// assertFileContent fails unless before and after carry the same significant
// syntax and the same comments in the same order.
func assertFileContent(t testing.TB, before, after string) {
	t.Helper()
	type fileContent struct{ syntax, comments []string }
	content := func(source string) fileContent {
		result := syntax.Parse([]byte(source))
		if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
			t.Fatalf("invalid output %q: %+v", source, diagnostics)
		}
		var parts []string
		stack := []syntax.SyntaxElement{result.Root().Element()}
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if node, ok := current.Node(); ok {
				switch node.Kind() {
				case syntax.BlockLabel:
					text := result.Text(node.Span())
					parts = append(parts, "label:"+strings.Trim(text, `"`))
					continue
				case syntax.Attribute:
					parts = append(parts, expressionTokens(result, node)...)
					continue
				}
				parts = append(parts, "node:"+node.Kind().String())
				for i := node.ChildCount() - 1; i >= 0; i-- {
					stack = append(stack, node.Child(i))
				}
			} else if token, ok := current.Token(); ok && token.Kind() != syntax.Newline && token.Kind() != syntax.Whitespace && token.Kind() != syntax.BOM {
				if token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment {
					continue
				}
				parts = append(parts, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
			}
		}
		// Header comments can cross labels, but no comment may disappear,
		// duplicate, or move past another comment anywhere in the whole file.
		var comments []string
		stack = append(stack, result.Root().Element())
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if node, ok := current.Node(); ok {
				for i := node.ChildCount() - 1; i >= 0; i-- {
					stack = append(stack, node.Child(i))
				}
			} else if token, ok := current.Token(); ok && (token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment) {
				comments = append(comments, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
			}
		}
		return fileContent{syntax: parts, comments: comments}
	}
	if left, right := content(before), content(after); !reflect.DeepEqual(left, right) {
		t.Fatalf("syntax or comment content changed:\n%q\n=>\n%q", left, right)
	}
}

// expressionTokens compares the canonical token/operation shape without
// invoking the normalizer. Pure quoted wrappers and ordinary legacy indices
// have equivalent spellings; all comments, literal bytes, operator nesting,
// and splat projection scopes must survive. Every significant node has an end
// marker so a suffix cannot silently move inside an operation or splat. Only
// parentheses and traversal containers are transparent: their canonical
// children fully describe the expression.
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

// wideExpressionEntry is one entry of the shared wide-expression source. It
// is a separate constant so a test can count the entries that survived.
const wideExpressionEntry = "f(x),"

// wideExpression builds a tuple of count identical call entries: the shared
// stress source for wide-expression scaling tests and benchmarks.
func wideExpression(count int) string {
	return "[" + strings.Repeat(wideExpressionEntry, count) + "]"
}

// wideBody builds a file body of count attribute/block pairs: the shared
// stress source for wide-body scaling tests and benchmarks.
func wideBody(count int) string {
	var source strings.Builder
	for i := range count {
		source.WriteString("a")
		source.WriteString(strconv.Itoa(i))
		source.WriteString("=x # value\nb { a=x }\n")
	}
	return source.String()
}
