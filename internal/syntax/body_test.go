package syntax

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestBodyShapes(t *testing.T) {
	for _, test := range []struct {
		name, source, shape string
	}{
		{
			"empty file",
			"",
			`File
  Body`,
		},
		{
			"attribute with expression",
			"value = a + 1\n",
			`File
  Body
    Attribute
      "value"
      "="
      Binary
        Variable
          "a"
        "+"
        Literal
          "1"`,
		},
		{
			"empty block with literal labels",
			`resource aws_instance "web" {}`,
			`File
  Body
    Block
      "resource"
      Label
        "aws_instance"
      Label
        "\""
        "web"
        "\""
      "{"
      Body
      "}"`,
		},
		{
			"single attribute block",
			"locals { value = 1 }\n",
			`File
  Body
    Block
      "locals"
      "{"
      Body
        Attribute
          "value"
          "="
          Literal
            "1"
      "}"`,
		},
		{
			"nested bodies and following file attribute",
			"outer {\n inner { a = 1 }\n b = 2\n}\nc = 3",
			`File
  Body
    Block
      "outer"
      "{"
      Body
        Block
          "inner"
          "{"
          Body
            Attribute
              "a"
              "="
              Literal
                "1"
          "}"
        Attribute
          "b"
          "="
          Literal
            "2"
      "}"
    Attribute
      "c"
      "="
      Literal
        "3"`,
		},
		{
			"keywords are ordinary body names and labels",
			"true { for = null }\nfalse true {}",
			`File
  Body
    Block
      "true"
      "{"
      Body
        Attribute
          "for"
          "="
          Literal
            "null"
      "}"
    Block
      "false"
      Label
        "true"
      "{"
      Body
      "}"`,
		},
		{
			"leading BOM stays outside the file body",
			"\ufeffa=1",
			`File
  "\ufeff"
  Body
    Attribute
      "a"
      "="
      Literal
        "1"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertBody(t, test.source, nil, test.shape)
		})
	}
}

func TestBodyTriviaOwnership(t *testing.T) {
	source := "# file\nblock /*type*/ label /*label*/ \"x\" /*brace*/ {\n # body\n a /*name*/ = /*value*/ (1 /*expr*/) /*tail*/ # end\n /*close*/ } /*blocktail*/\n"
	file := parseBodySource([]byte(source))
	assertExpressionPartition(t, []byte(source), file)
	if len(file.diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", file.diagnostics)
	}
	want := map[string]NodeKind{
		"# file": Body, "/*type*/": Block, "/*label*/": Block,
		"/*brace*/": Block, "# body": Body, "/*name*/": Attribute,
		"/*value*/": Attribute, "/*expr*/": ParenthesizedExpression,
		"/*tail*/": Body, "# end": Body, "/*close*/": Body,
		"/*blocktail*/": Body,
	}
	stack := []SyntaxNode{file.root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for i := range node.ChildCount() {
			child := node.Child(i)
			if inner, ok := child.Node(); ok {
				stack = append(stack, inner)
			} else if token, ok := child.Token(); ok {
				span := token.Span()
				text := source[span.Start:span.End]
				if kind, exists := want[text]; exists {
					if node.Kind() != kind {
						t.Fatalf("%q belongs to %v, want %v", text, node.Kind(), kind)
					}
					delete(want, text)
				}
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing comments: %+v", want)
	}
}

func assertBody(t *testing.T, source string, diagnostics []Diagnostic, shape string) {
	t.Helper()
	file := parseBodySource([]byte(source))
	assertExpressionPartition(t, []byte(source), file)
	if !reflect.DeepEqual(file.diagnostics, diagnostics) {
		t.Errorf("diagnostics: %+v\nwant: %+v", file.diagnostics, diagnostics)
	}
	if got := bodyShape(file, file.root.Element()); got != shape {
		t.Errorf("tree:\n%s\nwant:\n%s", got, shape)
	}
}

// Vertical grammar snapshots omit trivia only. Partition and ownership tests
// independently pin the token stream, every span, and comment/trivia parents.
func bodyShape(file syntaxFile, root SyntaxElement) string {
	var out strings.Builder
	var visit func(SyntaxElement, int)
	visit = func(element SyntaxElement, depth int) {
		if node, ok := element.Node(); ok {
			out.WriteString(strings.Repeat("  ", depth))
			out.WriteString(shapeNodeNames[node.Kind()])
			out.WriteByte('\n')
			for i := range node.ChildCount() {
				visit(node.Child(i), depth+1)
			}
		} else if token, ok := element.Token(); ok && !isTrivia(token.Kind()) && token.Kind() != EOF {
			out.WriteString(strings.Repeat("  ", depth))
			span := token.Span()
			out.WriteString(strconv.Quote(file.source[span.Start:span.End]))
			out.WriteByte('\n')
		}
	}
	visit(root, 0)
	return strings.TrimSuffix(out.String(), "\n")
}
