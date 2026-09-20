package syntax

import (
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
	file := Parse([]byte(source))
	assertTreeInvariants(t, []byte(source), file)
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
