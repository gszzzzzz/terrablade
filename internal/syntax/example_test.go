package syntax_test

import (
	"fmt"

	"terrablade/internal/syntax"
)

func ExampleParse() {
	result := syntax.Parse([]byte("answer = 42\n"))
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		fmt.Println(diagnostics)
		return
	}
	body, _ := result.Root().Child(0).Node()
	attribute, _ := body.Child(0).Node()
	name, _ := attribute.Child(0).Token()
	fmt.Println(result.Root().Kind(), attribute.Kind())
	fmt.Println(result.Text(name.Span()))
	// Output:
	// File Attribute
	// answer
}

func ExampleResult_Diagnostics() {
	result := syntax.Parse([]byte("a = 1\na = 2\n"))
	for _, diagnostic := range result.Diagnostics() {
		fmt.Printf("%s at bytes [%d, %d)\n", diagnostic.Kind, diagnostic.Span.Start, diagnostic.Span.End)
	}
	// Output:
	// DuplicateAttribute at bytes [6, 7)
}

func ExampleDiagnosticKind_Message() {
	fmt.Println(syntax.ExpectedExpression.Message())
	fmt.Println(syntax.ExpectedExpression.String())
	// Output:
	// Expected an expression.
	// ExpectedExpression
}

func ExampleResult_Position() {
	result := syntax.Parse([]byte("a = 1\r\na = 2\r\n"))
	// The caller supplies the filename and chooses how to render the error.
	for _, diagnostic := range result.Diagnostics() {
		position := result.Position(diagnostic.Span.Start)
		fmt.Printf("main.tf:%d:%d: %s\n", position.Line, position.Column, diagnostic.Kind.Message())
	}
	// Output:
	// main.tf:2:1: An attribute with this name is already defined in the same body.
}
