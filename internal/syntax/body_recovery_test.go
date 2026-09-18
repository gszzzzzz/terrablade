package syntax

import "testing"

func TestBodyRecovery(t *testing.T) {
	for _, test := range []struct {
		name, source string
		diagnostics  []Diagnostic
		shape        string
	}{
		{
			"missing value preserves next attribute",
			"a=\nb=2",
			[]Diagnostic{{ExpectedExpression, Span{2, 3}}},
			`File
  Body
    Attribute
      "a"
      "="
      Error
    Attribute
      "b"
      "="
      Literal
        "2"`,
		},
		{
			"comma separated tail is not another attribute",
			"a=1,b=2\nc=3",
			[]Diagnostic{{ExpectedBodyItemSeparator, Span{3, 4}}},
			`File
  Body
    Attribute
      "a"
      "="
      Literal
        "1"
    Error
      ","
      "b"
      "="
      "2"
    Attribute
      "c"
      "="
      Literal
        "3"`,
		},
		{
			"missing newline leaves containing brace",
			"b {\na=1}\nc=2",
			[]Diagnostic{{ExpectedBodyItemSeparator, Span{7, 8}}},
			`File
  Body
    Block
      "b"
      "{"
      Body
        Attribute
          "a"
          "="
          Literal
            "1"
      "}"
    Attribute
      "c"
      "="
      Literal
        "2"`,
		},
		{
			"broken single line mode recovers at newline",
			"b {a=1\nc=2\n}\nd=3",
			[]Diagnostic{{ExpectedSingleLineBlockEnd, Span{6, 7}}},
			`File
  Body
    Block
      "b"
      "{"
      Body
        Attribute
          "a"
          "="
          Literal
            "1"
        Attribute
          "c"
          "="
          Literal
            "2"
      "}"
    Attribute
      "d"
      "="
      Literal
        "3"`,
		},
		{
			"single line block cannot contain a nested block",
			"b { c {} }\na=1",
			[]Diagnostic{{ExpectedSingleLineAttribute, Span{4, 5}}},
			`File
  Body
    Block
      "b"
      "{"
      Body
        Error
          "c"
          Error
            "{"
            "}"
      "}"
    Attribute
      "a"
      "="
      Literal
        "1"`,
		},
		{
			"incomplete header retains type and label",
			"b x = 1\na=2",
			[]Diagnostic{{ExpectedBlockOpeningBrace, Span{4, 5}}},
			`File
  Body
    Block
      "b"
      Label
        "x"
    Error
      "="
      "1"
    Attribute
      "a"
      "="
      Literal
        "2"`,
		},
		{
			"label interpolation stays raw and later body survives",
			"b \"${x}\" {}\na=1",
			[]Diagnostic{{ExpectedLiteralBlockLabel, Span{3, 5}}},
			`File
  Body
    Block
      "b"
      Label
        "\""
        Error
          "${"
          "x"
          "}"
        "\""
      "{"
      Body
      "}"
    Attribute
      "a"
      "="
      Literal
        "1"`,
		},
		{
			"unfinished label sequence leaves its trailing comment in body",
			"b \"${x #tail",
			[]Diagnostic{
				{UnterminatedQuotedTemplate, Span{2, 3}},
				{UnterminatedTemplateSequence, Span{3, 5}},
				{ExpectedLiteralBlockLabel, Span{3, 5}},
				{ExpectedBlockOpeningBrace, Span{7, 12}},
			},
			`File
  Body
    Block
      "b"
      Label
        "\""
        Error
          "${"
          "x"`,
		},
		{
			"malformed label sequence preserves following label text and block",
			"b \"${f( }tail\" {}\na=1",
			[]Diagnostic{
				{ExpectedLiteralBlockLabel, Span{3, 5}},
				{ExpectedLiteralBlockLabel, Span{8, 9}},
			},
			`File
  Body
    Block
      "b"
      Label
        "\""
        Error
          "${"
          "f"
          "("
        Error
          "}"
        "tail"
        "\""
      "{"
      Body
      "}"
    Attribute
      "a"
      "="
      Literal
        "1"`,
		},
		{
			"malformed inner construct leaves enclosing body closer",
			"b { \n? [1 }\na=2",
			[]Diagnostic{{ExpectedBodyItem, Span{5, 6}}},
			`File
  Body
    Block
      "b"
      "{"
      Body
        Error
          "?"
          "["
          "1"
      "}"
    Attribute
      "a"
      "="
      Literal
        "2"`,
		},
		{
			"stray root brace does not stop subsequent items",
			"}\na=1",
			[]Diagnostic{{ExpectedBodyItem, Span{0, 1}}},
			`File
  Body
    Error
      "}"
    Attribute
      "a"
      "="
      Literal
        "1"`,
		},
		{
			"foreign body closer is consumed to the next newline",
			"] bad\na=1",
			[]Diagnostic{{ExpectedBodyItem, Span{0, 1}}},
			`File
  Body
    Error
      "]"
      "bad"
    Attribute
      "a"
      "="
      Literal
        "1"`,
		},
		{
			"duplicate attributes both survive in source order",
			"a=1\na=2",
			[]Diagnostic{{DuplicateAttribute, Span{4, 5}}},
			`File
  Body
    Attribute
      "a"
      "="
      Literal
        "1"
    Attribute
      "a"
      "="
      Literal
        "2"`,
		},
		{
			"missing value does not consume single line closer",
			"b { a= }\nz=1",
			[]Diagnostic{{ExpectedExpression, Span{7, 8}}},
			`File
  Body
    Block
      "b"
      "{"
      Body
        Attribute
          "a"
          "="
          Error
      "}"
    Attribute
      "z"
      "="
      Literal
        "1"`,
		},
		{
			"unterminated body has an empty body node",
			"b {",
			[]Diagnostic{{ExpectedClosingBrace, Span{3, 3}}},
			`File
  Body
    Block
      "b"
      "{"
      Body`,
		},
		{
			"bare name recovers at newline",
			"b\nx=1",
			[]Diagnostic{{ExpectedAttributeOrBlock, Span{1, 2}}},
			`File
  Body
    Error
      "b"
    Attribute
      "x"
      "="
      Literal
        "1"`,
		},
		{
			"lexical and header diagnostics both survive",
			"b \"x",
			[]Diagnostic{
				{UnterminatedQuotedTemplate, Span{2, 3}},
				{ExpectedBlockOpeningBrace, Span{4, 4}},
			},
			`File
  Body
    Block
      "b"
      Label
        "\""
        "x"`,
		},
		{
			"recovery ignores newlines nested in a call",
			"a=1 bad(\n2,3\n)\nb=2",
			[]Diagnostic{{ExpectedBodyItemSeparator, Span{4, 7}}},
			`File
  Body
    Attribute
      "a"
      "="
      Literal
        "1"
    Error
      "bad"
      "("
      "2"
      ","
      "3"
      ")"
    Attribute
      "b"
      "="
      Literal
        "2"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertBody(t, test.source, test.diagnostics, test.shape)
		})
	}
}
