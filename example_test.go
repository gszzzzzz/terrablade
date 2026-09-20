package terrablade_test

import (
	"errors"
	"fmt"

	"github.com/gszzzzzz/terrablade"
)

func ExampleFormat() {
	source := []byte("resource aws_instance web {ami=\"${var.ami}\"}\n")
	formatted, err := terrablade.Format(source, terrablade.Options{})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Print(string(formatted))
	// Output:
	// resource "aws_instance" "web" {
	//   ami = var.ami
	// }
}

func ExampleParseError() {
	_, err := terrablade.Format([]byte("name ="), terrablade.Options{})
	var parseError *terrablade.ParseError
	if errors.As(err, &parseError) {
		for _, diagnostic := range parseError.Diagnostics() {
			fmt.Printf("%s at %d:%d (bytes %d:%d)\n", diagnostic.Kind,
				diagnostic.Span.Start.Line, diagnostic.Span.Start.Column,
				diagnostic.Span.Start.Offset, diagnostic.Span.End.Offset)
		}
	}
	// Output:
	// ExpectedExpression at 1:7 (bytes 6:6)
}
