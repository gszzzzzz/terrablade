package syntax

import (
	"reflect"
	"testing"
)

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

func TestExpressionTriviaPlacement(t *testing.T) {
	for _, test := range []struct {
		name, source string
		containers   map[string]NodeKind
	}{
		{
			"file, binary, and argument trivia",
			" # head\n a /*left*/ + /*right*/ f( /*arg*/ b /*tail*/ ) # end\n",
			map[string]NodeKind{
				"# head":    File,
				"/*left*/":  BinaryExpression,
				"/*right*/": BinaryExpression,
				"/*arg*/":   FunctionCallExpression,
				"/*tail*/":  FunctionCallExpression,
				"# end":     File,
			},
		},
		{
			"traversal and step trivia",
			"a /*between*/ . /*name*/ true /*next*/ [ /*key*/ i /*end*/ ]",
			map[string]NodeKind{
				"/*between*/": TraversalExpression,
				"/*name*/":    AttributeAccess,
				"/*next*/":    TraversalExpression,
				"/*key*/":     IndexAccess,
				"/*end*/":     IndexAccess,
			},
		},
		{
			"parenthesized expression trivia",
			"( /*lead*/ a /*left*/ + /*right*/ b /*trail*/ )",
			map[string]NodeKind{
				"/*lead*/":  ParenthesizedExpression,
				"/*left*/":  BinaryExpression,
				"/*right*/": BinaryExpression,
				"/*trail*/": ParenthesizedExpression,
			},
		},
		{
			"full splat trivia",
			"foo /*gap*/ [ /*before*/ * /*after*/ ] /*step*/ . /*name*/ bar",
			map[string]NodeKind{
				"/*gap*/":    TraversalExpression,
				"/*before*/": FullSplat,
				"/*after*/":  FullSplat,
				"/*step*/":   FullSplat,
				"/*name*/":   AttributeAccess,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			file := parseExpressionSource([]byte(test.source))
			assertExpressionPartition(t, []byte(test.source), file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", file.diagnostics)
			}
			containers := make(map[string]NodeKind)
			var visit func(SyntaxNode)
			visit = func(node SyntaxNode) {
				for i := range node.ChildCount() {
					switch child := node.Child(i).(type) {
					case SyntaxNode:
						visit(child)
					case SyntaxToken:
						if child.Kind() == BlockComment || child.Kind() == LineComment {
							containers[file.source[child.span.Start:child.span.End]] = node.Kind()
						}
					}
				}
				if node.Kind() != File {
					for _, token := range lex([]byte(file.source[node.span.Start:node.span.End])).Tokens {
						if token.Span.Start == 0 || token.Span.End == node.span.End-node.span.Start {
							if trivia(token.Kind) {
								t.Fatalf("node %v absorbed outer trivia", node.Kind())
							}
						}
					}
				}
			}
			visit(file.root)
			if !reflect.DeepEqual(containers, test.containers) {
				t.Fatalf("comment containers = %v, want %v", containers, test.containers)
			}
		})
	}
}

func TestLexicalDiagnosticsPrecedeSyntaxDiagnosticsAtSameSpan(t *testing.T) {
	file := parseExpressionSource([]byte("\xff"))
	want := []Diagnostic{
		{
			Kind: InvalidUTF8,
			Span: Span{Start: 0, End: 1},
		},
		{
			Kind: ExpectedExpression,
			Span: Span{Start: 0, End: 1},
		},
	}
	if !reflect.DeepEqual(file.diagnostics, want) {
		t.Fatalf("diagnostics = %+v, want %+v", file.diagnostics, want)
	}
}
