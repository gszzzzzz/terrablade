package syntax

import (
	"reflect"
	"testing"
)

func TestParserTriviaLookahead(t *testing.T) {
	for _, test := range []struct {
		name    string
		source  string
		context newlineContext
		kind    TokenKind
	}{
		{
			"horizontal and multiline block comment",
			" \t/*\n comment */1",
			newlineTerminates,
			Number,
		},
		{
			"line comment terminates line expression",
			" # comment\n1",
			newlineTerminates,
			LineComment,
		},
		{
			"newline terminates line expression",
			" \n1",
			newlineTerminates,
			Newline,
		},
		{
			"delimited context accepts newlines and comments",
			" # comment\r\n1",
			newlineTransparent,
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
	p.consumeUntil(&root, p.look(newlineTransparent))
	expression := p.begin()
	p.consumeLookahead(&expression, newlineTerminates)
	root.node(expression.finish(LiteralExpression))
	if p.peek(newlineTerminates) != LineComment {
		t.Fatal("line comment must remain outside the expression")
	}
	p.consumeUntil(&root, len(p.tokens)-1)
	// Even repeated reads at EOF do not duplicate the sentinel.
	p.consumeLookahead(&root, newlineTerminates)
	p.consumeLookahead(&root, newlineTerminates)
	file := p.file(root)
	assertTreeInvariants(t, source, file)
	node, _ := file.root.Child(2).Node()
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
			"collection trivia stays between elements and items",
			"{ /*open*/ a /*key*/ = /*value*/ [ /*element*/ 1 /*end*/ ] /*separator*/ , /*item*/ b=2 /*close*/ }",
			map[string]NodeKind{
				"/*open*/":      ObjectExpression,
				"/*key*/":       ObjectItem,
				"/*value*/":     ObjectItem,
				"/*element*/":   TupleExpression,
				"/*end*/":       TupleExpression,
				"/*separator*/": ObjectExpression,
				"/*item*/":      ObjectExpression,
				"/*close*/":     ObjectExpression,
			},
		},
		{
			"template sequence comments remain inside header nodes",
			`"%{ /*before*/ if /*condition*/ a /*end*/ }${ /*value*/ x /*close*/ }%{ /*after*/ endif }"`,
			map[string]NodeKind{
				"/*before*/":    TemplateDirective,
				"/*condition*/": TemplateDirective,
				"/*end*/":       TemplateDirective,
				"/*value*/":     TemplateInterpolation,
				"/*close*/":     TemplateInterpolation,
				"/*after*/":     TemplateDirective,
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
			assertTreeInvariants(t, []byte(test.source), file)
			if len(file.diagnostics) != 0 {
				t.Fatalf("unexpected diagnostics: %+v", file.diagnostics)
			}
			containers := make(map[string]NodeKind)
			var visit func(SyntaxNode)
			visit = func(node SyntaxNode) {
				for i := range node.ChildCount() {
					element := node.Child(i)
					if child, ok := element.Node(); ok {
						visit(child)
					} else if child, ok := element.Token(); ok {
						if child.Kind() == BlockComment || child.Kind() == LineComment {
							containers[file.source[child.span.Start:child.span.End]] = node.Kind()
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

func TestLimitStopsGrammarAndRetainsUnparsedTokens(t *testing.T) {
	source := []byte("a + \xff # tail\n")
	p := newParser(source)
	root := p.begin()
	variable := p.begin()
	p.consumeLookahead(&variable, newlineTerminates)
	root.node(variable.finish(VariableExpression))
	position := p.pos
	limitSpan := p.tokens[p.look(newlineTerminates)].span
	p.haltAtLimit(limitSpan)
	p.haltAtLimit(limitSpan)

	if p.current().kind != EOF || p.peek(newlineTerminates) != EOF || p.peek(newlineTransparent) != EOF {
		t.Fatal("all grammar cursor views must appear exhausted after a limit")
	}
	// A production need not know about the halt flag to stop consuming/reporting.
	p.consumeUntil(&root, len(p.tokens)-1)
	p.consumeLookahead(&root, newlineTransparent)
	p.report(ExpectedExpression, limitSpan)
	p.call(&root, newlineTerminates)
	p.steps(&root, newlineTerminates, allTraversalSteps)
	p.recoverUntil(&root, newlineTransparent, itemBoundaries)
	p.skipConstruct(&root)
	p.tuple(&root)
	p.object(&root)
	p.recoverUntil(&root, newlineTerminates, itemBoundaries)
	p.recoverUntil(&root, newlineTransparent, expressionBoundaries)
	p.recoverUntil(&root, newlineTransparent, templateBoundaries)
	if p.pos != position {
		t.Fatal("grammar consumed tokens after shutdown")
	}

	file := p.file(root)
	assertTreeInvariants(t, source, file)
	if node, ok := file.root.Child(2).Node(); !ok || node.Kind() != ErrorNode {
		t.Fatal("unparsed non-trivia must remain in a file-level ErrorNode")
	}
	want := []Diagnostic{
		{
			Kind: NestingLimitExceeded,
			Span: limitSpan,
		},
		{
			Kind: InvalidUTF8,
			Span: Span{Start: 4, End: 5},
		},
	}
	if !reflect.DeepEqual(file.diagnostics, want) {
		t.Fatalf("diagnostics = %+v, want one limit and original lexical error: %+v", file.diagnostics, want)
	}
}
