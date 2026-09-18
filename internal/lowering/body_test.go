package lowering_test

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"terrablade/internal/document"
	"terrablade/internal/lowering"
	"terrablade/internal/syntax"
)

func TestFileLayouts(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"empty", "", ""},
		{"outer whitespace", " \n\t\r\n", ""},
		{"BOM only", "\ufeff\n\n", ""},
		{"BOM and attribute", "\ufeffa=1", "a = 1\n"},
		{"final newline", "a=1", "a = 1\n"},
		{"outer padding", "\n\na=1\n\n\n", "a = 1\n"},
		{"attribute gap collapses", "a=1\n\n\nb=2\n", "a = 1\nb = 2\n"},
		{"empty block", "empty { \n\n }", "empty {}\n"},
		{"short block expands", "short { a=1 }", "short {\n  a = 1\n}\n"},
		{"block boundaries", "a=1\nb {}\nc=2\nd {}\ne {}", "a = 1\n\nb {}\n\nc = 2\n\nd {}\n\ne {}\n"},
		{"nested block boundaries", "outer {\n a=1\n inner { x=2 }\n b=3\n}", "outer {\n  a = 1\n\n  inner {\n    x = 2\n  }\n\n  b = 3\n}\n"},
		{"labels", `resource aws_instance "web\u0020server" {}`, "resource \"aws_instance\" \"web\\u0020server\" {}\n"},
		{"header comments", `block /*type*/ bare /*label*/ "quoted" /*brace*/ {}`, "block \"bare\" \"quoted\" /*brace*/ {}\n"},
		{"unlabeled header comment", `block /*type*/ {}`, "block /*type*/ {}\n"},
		{"multiline label trivia removed", "block /*type\n end*/ \"a\" {}", "block \"a\" {}\n"},
		{"multiline brace trivia retained", "block \"a\" /*brace\n end*/ {}", "block \"a\" /*brace\n end*/ {}\n"},
		{"attribute comments", "a /*key*/=/*value*/ 1 /*tail*/ # end\n", "a /*key*/ = /*value*/ 1 /*tail*/ # end\n"},
		{"leading and trailing comments", "\n\n# lead\na=1\n# end\n\n", "# lead\na = 1\n# end\n"},
		{"only comments", "\n# one\n\n\n# two\n\n", "# one\n\n# two\n"},
		{"comment sections", "a=1\n\n\n# section\n\n\nb=2\n", "a = 1\n\n# section\n\nb = 2\n"},
		{"same-line comment run section", "a=1\n/*first*/ /*second*/ # third\n\n\nb=2", "a = 1\n/*first*/ /*second*/ # third\n\nb = 2\n"},
		{"same-line block comments section", "a=1\n/*first*/ /*second*/\n\n\nb=2", "a = 1\n/*first*/ /*second*/\n\nb = 2\n"},
		{"comment prefix is not independent", "a=1\n\n/*prefix*/ b=2", "a            = 1\n/*prefix*/ b = 2\n"},
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

func TestBodyAlignment(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
		width              int
	}{
		{"attributes", "a=1\nlong=2\nz=3", "a    = 1\nlong = 2\nz    = 3\n", 80},
		{"removed blank line joins groups", "a=1\n\nlong=2", "a    = 1\nlong = 2\n", 80},
		{"standalone comment splits groups", "a=1\n# note\nlong=2\nz=3", "a = 1\n# note\nlong = 2\nz    = 3\n", 80},
		{"inline comments", "a=1 # first\nlong=222 # second\nz=3", "a    = 1   # first\nlong = 222 # second\nz    = 3\n", 80},
		{"inline prefix", "/* lead */ a=1\nlong=2", "/* lead */ a = 1\nlong         = 2\n", 80},
		{"name comment", "a /* name */=1\nlong=2", "a /* name */ = 1\nlong         = 2\n", 80},
		{"unicode grapheme columns", "한글=1\naaa=2\né=3", "한글  = 1\naaa = 2\né   = 3\n", 80},
		{"heredoc stays in group", "a=1\nlong=<<E\nx\nE\nz=3", "a    = 1\nlong = <<E\nx\nE\nz    = 3\n", 80},
		{"wrapped tuple splits groups", "a=1\nlong=[alpha,beta]\nz=2", "a = 1\nlong = [\n  alpha,\n  beta,\n]\nz = 2\n", 16},
		{"flat tuple joins groups", "a=1\nlong=[alpha,beta]\nz=2", "a    = 1\nlong = [alpha, beta]\nz    = 2\n", 80},
		{"operator parentheses split groups", "a=1\nlong=alpha+beta\nz=2", "a = 1\nlong = (\n  alpha\n  + beta\n)\nz = 2\n", 16},
		{"nested scope", "b {\n a=1\n longer=2\n}\nx=3", "b {\n  a      = 1\n  longer = 2\n}\n\nx = 3\n", 80},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := renderFile(t, test.source, test.width)
			if got != test.want {
				t.Fatalf("rendered %q, want %q", got, test.want)
			}
			if again := renderFile(t, got, test.width); again != got {
				t.Fatalf("not idempotent: %q => %q", got, again)
			}
			assertFileContent(t, test.source, got)
		})
	}
}

func TestBodyOpenTofuCompatibility(t *testing.T) {
	if os.Getenv("TERRABLADE_COMPARE_TOFU") != "1" {
		t.Skip("set TERRABLADE_COMPARE_TOFU=1 to compare with an installed OpenTofu")
	}
	for _, source := range []string{
		"a=1\nlong=2\nz=3",
		"a=1 # first\nlong=222 # second\nz=3",
		"a=1 // first\nlong=222 // second\nz=3",
		"a=1\n# note\nlong=2\nz=3",
		"a /* name */=1\nlong=2",
		"/* lead */ a=1\nlong=2",
		"a /* multi\nline */=1\nlong_name=2",
		"한글=1\naaa=2\né=3",
		"a=1\nlong=<<E\nx\nE\nz=3",
		"a=1\nlong=[alpha,beta]\nz=2",
		"a=1\nlong=alpha+beta\nz=2",
		"outer label {\n a=1\n inner { z=3 }\n longer=2\n}\nx=3",
		"a=1\n# next\nb {}\n\n\n# attributes\nx=3\nyyyy=4",
		"b { # open\n a=1 # value\n}\n",
		"b {\n a=<<-E\n  x\n  E\n}\n",
		"b { /*body*/ }\n",
		"\ufeffa=1\nlong=2\n",
		`block /*type*/ bare /*between*/ "quoted" /*brace*/ {}`,
		`block /*type*/ {}`,
		"a=1\n/*first*/ /*second*/ # third\n\nb=2",
		"value={\na=1 # first\nlonger=222 # second\n}\n",
		"a=1\nvalue={ a=1, longer=2 }\nz=3",
		"a=1\nvalue={\nx=1\nlonger=2\n}\nz=3",
		"value=[{a=1},{longer=2}]",
	} {
		for _, width := range []int{16, 80} {
			output := renderFile(t, source, width)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			command := exec.CommandContext(ctx, "tofu", "fmt", "-no-color", "-")
			command.Stdin = strings.NewReader(output)
			formatted, err := command.CombinedOutput()
			cancel()
			if err != nil {
				t.Fatalf("OpenTofu failed: %v\n%s", err, formatted)
			}
			if string(formatted) != output {
				t.Errorf("OpenTofu changed body formatting:\n%q\n=>\n%q", output, formatted)
			}
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
				lastLabelEnd := -1
				if node.Kind() == syntax.Block {
					for i := range node.ChildCount() {
						if child, ok := node.Child(i).Node(); ok && child.Kind() == syntax.BlockLabel {
							lastLabelEnd = child.Span().End
						}
					}
				}
				for i := node.ChildCount() - 1; i >= 0; i-- {
					if token, ok := node.Child(i).Token(); ok && token.Span().Start < lastLabelEnd && (token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment) {
						continue
					}
					stack = append(stack, node.Child(i))
				}
			} else if token, ok := current.Token(); ok && token.Kind() != syntax.Newline && token.Kind() != syntax.Whitespace && token.Kind() != syntax.BOM {
				parts = append(parts, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
			}
		}
		return parts
	}
	if left, right := content(before), content(after); !reflect.DeepEqual(left, right) {
		t.Fatalf("syntax or comment content changed:\n%q\n=>\n%q", left, right)
	}
}
