package syntax

import "testing"

func TestParserTriviaLookahead(t *testing.T) {
	for _, test := range []struct {
		name    string
		source  string
		context expressionContext
		kind    Kind
	}{
		{
			"horizontal and multiline block comment",
			" \t/*\n comment */1",
			lineExpression,
			Number,
		},
		{
			"line comment terminates line expression",
			" # comment\n1",
			lineExpression,
			LineComment,
		},
		{
			"newline terminates line expression",
			" \n1",
			lineExpression,
			Newline,
		},
		{
			"delimited context accepts newlines and comments",
			" # comment\r\n1",
			delimitedExpression,
			Number,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := newParser([]byte(test.source))
			if got := p.peek(test.context); got != test.kind {
				t.Fatalf("lookahead = %v, want %v", got, test.kind)
			}
			if p.pos != 0 {
				t.Fatal("lookahead consumed trivia")
			}
		})
	}
}

func TestParserCursorPreservesTriviaAndEOF(t *testing.T) {
	source := []byte(" /* lead */1 \t# tail\n")
	p := newParser(source)
	root := p.begin()
	p.before(&root, p.look(delimitedExpression))
	expression := p.begin()
	p.take(&expression, lineExpression)
	root.node(p.finish(LiteralExpression, expression))
	if p.peek(lineExpression) != LineComment {
		t.Fatal("line comment must remain outside the expression")
	}
	p.before(&root, len(p.tokens)-1)
	// Even repeated reads at EOF do not duplicate the sentinel.
	p.take(&root, lineExpression)
	p.take(&root, lineExpression)
	file := p.file(root)
	assertFilePartition(t, source, file)
	node := file.root.Child(2).(SyntaxNode)
	if node.Kind() != LiteralExpression || node.Span() != (Span{Start: 11, End: 12}) {
		t.Fatalf("trivia leaked into expression: %+v", node)
	}
}
