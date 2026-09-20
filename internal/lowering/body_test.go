package lowering_test

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestFileLayouts(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"empty", "", ""},
		{"outer whitespace", " \n\t\r\n", ""},
		{"BOM only", "\ufeff\n\n", ""},
		{"BOM and attribute", "\ufeffa=1", "a = 1\n"},
		{"final newline", "a=1", "a = 1\n"},
		{"outer padding", "\n\na=1\n\n\n", "a = 1\n"},
		{"attribute blank gap caps", "a=1\n\n\nb=2\n", "a = 1\n\nb = 2\n"},
		{"empty block", "empty { \n\n }", "empty {}\n"},
		{"short block expands", "short { a=1 }", "short {\n  a = 1\n}\n"},
		{"block boundaries", "a=1\nb {}\nc=2\nd {}\ne {}", "a = 1\n\nb {}\n\nc = 2\n\nd {}\n\ne {}\n"},
		{"nested block boundaries", "outer {\n a=1\n inner { x=2 }\n b=3\n}", "outer {\n  a = 1\n\n  inner {\n    x = 2\n  }\n\n  b = 3\n}\n"},
		{"labels", `resource aws_instance "web\u0020server" {}`, "resource \"aws_instance\" \"web\\u0020server\" {}\n"},
		{"header comments", `block /*type*/ bare /*label*/ "quoted" /*brace*/ {}`, "block \"bare\" \"quoted\" /*type*/ /*label*/ /*brace*/ {}\n"},
		{"unlabeled header comment", `block /*type*/ {}`, "block /*type*/ {}\n"},
		{"multiline label trivia relocated", "block /*type\n end*/ \"a\" {}", "block \"a\" /*type\n end*/ {}\n"},
		{"multiline brace trivia retained", "block \"a\" /*brace\n end*/ {}", "block \"a\" /*brace\n end*/ {}\n"},
		{"repeated header comments", `block /*same*/ first /*same*/ second /*same*/ {}`, "block \"first\" \"second\" /*same*/ /*same*/ /*same*/ {}\n"},
		{"header and surrounding comments", "# lead\nblock /*first*/ bare /*second\nline*/ \"quoted\" /*brace*/ { # open\n # body\n} // end\n", "# lead\nblock \"bare\" \"quoted\" /*first*/ /*second\nline*/ /*brace*/ { # open\n  # body\n} // end\n"},
		{"nested header comments", "outer {\n inner /*type*/ bare /*label*/ { a=1 }\n}", "outer {\n  inner \"bare\" /*type*/ /*label*/ {\n    a = 1\n  }\n}\n"},
		{"attribute comments", "a /*key*/=/*value*/ 1 /*tail*/ # end\n", "a /*key*/ = /*value*/ 1 /*tail*/ # end\n"},
		{"leading and trailing comments", "\n\n# lead\na=1\n# end\n\n", "# lead\na = 1\n# end\n"},
		{"only comments", "\n# one\n\n\n# two\n\n", "# one\n\n# two\n"},
		{"comment sections", "a=1\n\n\n# section\n\n\nb=2\n", "a = 1\n\n# section\n\nb = 2\n"},
		{"same-line comment run section", "a=1\n/*first*/ /*second*/ # third\n\n\nb=2", "a = 1\n/*first*/ /*second*/ # third\n\nb = 2\n"},
		{"same-line block comments section", "a=1\n/*first*/ /*second*/\n\n\nb=2", "a = 1\n/*first*/ /*second*/\n\nb = 2\n"},
		{"comment prefix follows attribute group boundary", "a=1\n\n/*prefix*/ b=2", "a = 1\n\n/*prefix*/ b = 2\n"},
		{"inline comment preserves attribute group boundary", "a=1 # tail\n\n\nb=2", "a = 1 # tail\n\nb = 2\n"},
		{"comment before block", "a=1\n# block\nb {}", "a = 1\n\n# block\nb {}\n"},
		{"comment after block", "a {}\n# next\nb=1", "a {}\n\n# next\nb = 1\n"},
		{"inline comment before block boundary", "a=1 /*tail*/\nb {}", "a = 1 /*tail*/\n\nb {}\n"},
		{"same-line comment run after block boundary", "a {}\n/*first*/ /*second*/\nb=1", "a {}\n\n/*first*/ /*second*/\nb = 1\n"},
		{"comment prefix after block boundary", "a=1\n/*prefix*/ b {}", "a = 1\n\n/*prefix*/ b {}\n"},
		{"opener comment", "b { # open\n a=1\n}", "b { # open\n  a = 1\n}\n"},
		{"opener block comment before attribute", "b { /*open*/ a=1 }", "b { /*open*/\n  a = 1\n}\n"},
		{"opener comment run before attribute", "b { /*first*/ /*second*/ a=1 }", "b { /*first*/ /*second*/\n  a = 1\n}\n"},
		{"opener multiline comment before attribute", "b { /*first\nsecond*/ a=1 }", "b { /*first\nsecond*/\n  a = 1\n}\n"},
		{"body comment prefix stays adjacent", "b {\n /*prefix*/ a=1\n}", "b {\n  /*prefix*/ a = 1\n}\n"},
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

func TestFileRejectsLineCommentsInsideBlockHeader(t *testing.T) {
	// An ordinary newline ends the header, including one following a line
	// comment. File must not move comments to repair already-invalid syntax.
	for _, source := range []string{
		"block # type\n label {}", "block label // label\n {}",
		"block /*first*/ label # last\n {}",
	} {
		result := syntax.Parse([]byte(source))
		if len(result.Diagnostics()) == 0 {
			t.Fatalf("expected invalid block header: %q", source)
		}
		doc, err := lowering.File(result)
		if err == nil || document.Render(doc, document.Options{}) != "" {
			t.Fatalf("expected error and empty document for %q, got %v", source, err)
		}
	}
}

func TestBodyAlignment(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
		width              int
	}{
		{"attributes", "a=1\nlong=2\nz=3", "a    = 1\nlong = 2\nz    = 3\n", 80},
		{"blank line splits groups", "a=1\n\nlong=2", "a = 1\n\nlong = 2\n", 80},
		{"resource attribute groups", "resource x y {\n a=1\n bb=2\n\n\n longer=3\n c=4\n}", "resource \"x\" \"y\" {\n  a  = 1\n  bb = 2\n\n  longer = 3\n  c      = 4\n}\n", 80},
		{"blank line splits comment columns", "a=1 # first\nb=222 # second\n\nlong=3 # third\nx=4 # fourth", "a = 1   # first\nb = 222 # second\n\nlong = 3 # third\nx    = 4 # fourth\n", 80},
		{"heredoc separates attribute groups", "a=1\nlong=<<E\nx\nE\n\n\nb=2\ncc=3", "a    = 1\nlong = <<E\nx\nE\n\nb  = 2\ncc = 3\n", 80},
		{"comment section between attribute groups", "a=1\nlong=2\n\n# group\n\nb=3\ncc=4", "a    = 1\nlong = 2\n\n# group\n\nb  = 3\ncc = 4\n", 80},
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
		"b { /*open*/ a=1 }",
		"b { /*first*/ /*second*/ a=1 }",
		"b { /*first\nsecond*/ a=1 }",
		"b {\n /*prefix*/ a=1\n}",
		"\ufeffa=1\nlong=2\n",
		`block /*type*/ bare /*between*/ "quoted" /*brace*/ {}`,
		`block /*type*/ {}`,
		"block /*type\n end*/ \"a\" {}",
		`block /*same*/ first /*same*/ second /*same*/ {}`,
		"# lead\nblock /*first*/ bare /*second\nline*/ \"quoted\" /*brace*/ { # open\n # body\n} // end\n",
		"outer {\n inner /*type*/ bare /*label*/ { a=1 }\n}",
		"a=1\n/*first*/ /*second*/ # third\n\nb=2",
		"value={\na=1 # first\nlonger=222 # second\n}\n",
		"a=1\nvalue={ a=1, longer=2 }\nz=3",
		"a=1\nvalue={\nx=1\nlonger=2\n}\nz=3",
		"value=[{a=1},{longer=2}]",
		"resource x y {\n a=1\n bb=2\n\n\n longer=3\n c=4\n}",
		"a=1 # first\nb=222 # second\n\nlong=3 # third\nx=4 # fourth",
		"a=1\nlong=<<E\nx\nE\n\n\nb=2\ncc=3",
		"a=1\nlong=2\n\n# group\n\nb=3\ncc=4",
	} {
		for _, width := range []int{16, 80} {
			output := renderFile(t, source, width)
			assertOpenTofu(t, output)
		}
	}
}

func assertOpenTofu(t *testing.T, output string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "tofu", "fmt", "-no-color", "-")
	command.Stdin = strings.NewReader(output)
	formatted, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("OpenTofu failed: %v\n%s", err, formatted)
	}
	if string(formatted) != output {
		t.Errorf("OpenTofu changed canonical formatting:\n%q\n=>\n%q", output, formatted)
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
	type fileContent struct{ syntax, comments []string }
	content := func(source string) fileContent {
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
			} else if token, ok := current.Token(); ok && token.Kind() != syntax.Newline && token.Kind() != syntax.Whitespace && token.Kind() != syntax.BOM {
				if token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment {
					continue
				}
				parts = append(parts, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
			}
		}
		// Header comments can cross labels, but no comment may disappear,
		// duplicate, or move past another comment anywhere in the whole file.
		var comments []string
		stack = append(stack, result.Root().Element())
		for len(stack) > 0 {
			current := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if node, ok := current.Node(); ok {
				for i := node.ChildCount() - 1; i >= 0; i-- {
					stack = append(stack, node.Child(i))
				}
			} else if token, ok := current.Token(); ok && (token.Kind() == syntax.LineComment || token.Kind() == syntax.BlockComment) {
				comments = append(comments, strings.ReplaceAll(result.Text(token.Span()), "\r\n", "\n"))
			}
		}
		return fileContent{syntax: parts, comments: comments}
	}
	if left, right := content(before), content(after); !reflect.DeepEqual(left, right) {
		t.Fatalf("syntax or comment content changed:\n%q\n=>\n%q", left, right)
	}
}
