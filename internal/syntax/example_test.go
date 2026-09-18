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
