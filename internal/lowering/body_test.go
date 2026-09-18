package lowering_test

import (
	"reflect"
	"strings"
	"testing"

	"terrablade/internal/document"
	"terrablade/internal/lowering"
	"terrablade/internal/syntax"
)

func TestFileLayouts(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"empty", "", ""},
		{"outer whitespace", " \n\t\r\n", ""},
		{"BOM only", "\ufeff\n\n", "\ufeff"},
		{"BOM and attribute", "\ufeffa=1", "\ufeffa = 1\n"},
		{"final newline", "a=1", "a = 1\n"},
		{"outer padding", "\n\na=1\n\n\n", "a = 1\n"},
		{"attribute gap collapses", "a=1\n\n\nb=2\n", "a = 1\nb = 2\n"},
		{"empty block", "empty { \n\n }", "empty {}\n"},
		{"short block expands", "short { a=1 }", "short {\n  a = 1\n}\n"},
		{"block boundaries", "a=1\nb {}\nc=2\nd {}\ne {}", "a = 1\n\nb {}\n\nc = 2\n\nd {}\n\ne {}\n"},
		{"nested block boundaries", "outer {\n a=1\n inner { x=2 }\n b=3\n}", "outer {\n  a = 1\n\n  inner {\n    x = 2\n  }\n\n  b = 3\n}\n"},
		{"labels", `resource aws_instance "web\u0020server" {}`, "resource \"aws_instance\" \"web\\u0020server\" {}\n"},
		{"header comments", `block /*type*/ bare /*label*/ "quoted" /*brace*/ {}`, "block /*type*/ \"bare\" /*label*/ \"quoted\" /*brace*/ {}\n"},
		{"attribute comments", "a /*key*/=/*value*/ 1 /*tail*/ # end\n", "a /*key*/ = /*value*/ 1 /*tail*/ # end\n"},
		{"leading and trailing comments", "\n\n# lead\na=1\n# end\n\n", "# lead\na = 1\n# end\n"},
		{"only comments", "\n# one\n\n\n# two\n\n", "# one\n\n# two\n"},
		{"comment sections", "a=1\n\n\n# section\n\n\nb=2\n", "a = 1\n\n# section\n\nb = 2\n"},
		{"inline comment does not preserve empty line", "a=1 # tail\n\n\nb=2", "a = 1 # tail\nb = 2\n"},
		{"comment before block", "a=1\n# block\nb {}", "a = 1\n\n# block\nb {}\n"},
		{"comment after block", "a {}\n# next\nb=1", "a {}\n\n# next\nb = 1\n"},
		{"opener comment", "b { # open\n a=1\n}", "b { # open\n  a = 1\n}\n"},
		{"comment only block", "b {\n # body\n\n}", "b {\n  # body\n}\n"},
		{"inline block comment only", "b { /*body*/ }", "b { /*body*/\n}\n"},
		{"standalone inline block comment", "/*lead*/ a=1\n", "/*lead*/ a = 1\n"},
		{"closing standalone comment", "b { a=1 /*tail*/ }", "b {\n  a = 1 /*tail*/\n}\n"},
		{"heredoc final newline", "a=<<E\nx\nE\n", "a = <<E\nx\nE\n"},
		{"heredoc sibling", "a=<<E\nx\nE\nb=2\n", "a = <<E\nx\nE\nb = 2\n"},
		{"heredoc nested", "b {\n a=<<-E\n  x\n  E\n}\n", "b {\n  a = <<-E\n  x\n  E\n}\n"},
		{"heredoc comment", "a=<<E\nx\nE\n# next\nb=2", "a = <<E\nx\nE\n# next\nb = 2\n"},
		{"heredoc block gap", "a=<<E\nx\nE\nb {}", "a = <<E\nx\nE\n\nb {}\n"},
		{"CRLF", "b {\r\n a=1\r\n}\r\n", "b {\n  a = 1\n}\n"},
		{"multiline comment literal", "b {\n /* first\r\n second */\n a=1\n}", "b {\n  /* first\n second */\n  a = 1\n}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := renderFile(t, test.source, 80)
			if got != test.want {
				t.Fatalf("rendered %q, want %q", got, test.want)
			}
			if again := renderFile(t, got, 80); again != got {
				t.Fatalf("not idempotent: %q => %q", got, again)
			}
			assertFileContent(t, test.source, got)
		})
	}
}

func TestFileRejectsInvalidInput(t *testing.T) {
	for _, result := range []syntax.Result{syntax.Result{}, syntax.Parse([]byte("a=")), syntax.Parse([]byte("good=1\nbad="))} {
		doc, err := lowering.File(result)
		if err == nil || document.Render(doc, document.Options{}) != "" {
			t.Fatalf("expected error and empty document, got %v", err)
		}
	}
}

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

func assertFileContent(t testing.TB, before, after string) {
	t.Helper()
	content := func(source string) []string {
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
			} else if token, ok := current.Token(); ok && token.Kind() != syntax.Newline && token.Kind() != syntax.Whitespace {
				parts = append(parts, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
			}
		}
		return parts
	}
	if left, right := content(before), content(after); !reflect.DeepEqual(left, right) {
		t.Fatalf("syntax or comment content changed:\n%q\n=>\n%q", left, right)
	}
}
