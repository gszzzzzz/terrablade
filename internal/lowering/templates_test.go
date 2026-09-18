package lowering_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestTemplateLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"empty quote", `""`, 1, `""`},
		{"literal escapes", `"a\n\u0041$${x}%%{if}"`, 8, `"a\n\u0041$${x}%%{if}"`},
		{"interpolation spaces", `"hello ${ a+b } end"`, 80, `"hello ${a + b} end"`},
		{"strip markers", `"before ${~ a + b ~} after"`, 80, `"before ${~a + b~} after"`},
		{"multiline expression reflows", "\"${\n  a + b\n}\"", 80, `"${a + b}"`},
		{"interpolation breaks safely", `"${alpha + beta}"`, 12, "\"${\n  alpha\n    + beta\n}\""},
		{"interpolation comments", "\"${a # why\n + b}\"", 80, "\"${\n  a # why\n    + b\n}\""},
		{"directives", `"%{ if a }yes%{ else }no%{ endif }"`, 80, `"%{if a}yes%{else}no%{endif}"`},
		{"directive strip", `" a %{~ if a ~} b %{~ endif ~} c "`, 80, `" a %{~if a~} b %{~endif~} c "`},
		{"for directive", `"%{for k,v in xs}${k}:${v}%{endfor}"`, 80, `"%{for k, v in xs}${k}:${v}%{endfor}"`},
		{"directive binding comma precedes comment", "\"%{for k # binding\n,v in xs}${v}%{endfor}\"", 80, "\"%{\n  for k, # binding\n  v in xs\n}${v}%{endfor}\""},
		{"nested directive", `"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`, 80, `"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`},
		{"nested quoted interpolation", `"${"inner ${ a }"}"`, 80, `"${"inner ${a}"}"`},
		{"quoted object key", `{"a"=1}`, 80, `{ "a" = 1 }`},
		{"empty heredoc", "<<E\nE\n", 80, "<<E\nE"},
		{"heredoc literal indentation", "<<-E\r\n  alpha\r\n\r\n \tbeta\r\n  E \t\r\n", 8, "<<-E\n  alpha\n\n \tbeta\n  E \t"},
		{"heredoc interpolation", "<<E\n  ${ a+b } text\nE\n", 80, "<<E\n  ${a + b} text\nE"},
		{"heredoc interpolation reflow", "<<-E\n  ${\n a + b\n} end\nE\n", 80, "<<-E\n  ${a + b} end\nE"},
		{"heredoc interpolation wrap", "<<-E\n  ${alpha + beta}\nE\n", 12, "<<-E\n  ${\n  alpha\n    + beta\n}\nE"},
		{"heredoc directives", "<<E\n%{for x in xs}\n${ x }\n%{endfor}\nE\n", 80, "<<E\n%{for x in xs}\n${x}\n%{endfor}\nE"},
		{"heredoc in tuple", "[<<E\nx\nE\n]", 80, "[\n  <<E\nx\nE\n  ,\n]"},
		{"heredoc in call", "f(<<E\nx\nE\n,1)", 80, "f(\n  <<E\nx\nE\n  ,\n  1,\n)"},
		{"heredoc expanded argument", "f(<<E\nx\nE\n...)", 80, "f(\n  <<E\nx\nE\n  ...\n)"},
		{"heredoc in index", "foo[<<E\nx\nE\n]", 80, "(\n  foo[\n    <<E\nx\nE\n  ]\n)"},
		{"heredoc before traversal", "(<<E\nx\nE\n[0])", 80, "(\n  <<E\nx\nE\n  [0]\n)"},
		{"heredoc in for collection", "[for x in <<E\nx\nE\n:x]", 80, "[\n  for x in <<E\nx\nE\n  :\n  x\n]"},
		{"heredoc for projection before condition", "[for x in xs:<<E\nx\nE\nif x]", 80, "[\n  for x in xs :\n  <<E\nx\nE\n  if x\n]"},
		{"heredoc for key before arrow", "{for x in xs:<<E\nx\nE\n=>x}", 80, "{\n  for x in xs :\n  <<E\nx\nE\n    => x\n}"},
		{"heredoc in object has no trailing comma", "{x=<<E\nx\nE\n}", 80, "{\n  x = <<E\nx\nE\n}"},
		{"heredoc separates object entries", "{x=<<E\nx\nE\ny=2}", 80, "{\n  x = <<E\nx\nE\n  y = 2,\n}"},
		{"heredoc object blank line", "{x=<<E\nx\nE\n\n\ny=2}", 80, "{\n  x = <<E\nx\nE\n\n  y = 2,\n}"},
		{"heredoc object standalone comment", "{x=<<E\nx\nE\n# next\ny=2}", 80, "{\n  x = <<E\nx\nE\n  # next\n  y = 2,\n}"},
		{"parenthesized heredoc object value keeps comma", "{x=(<<E\nx\nE\n)}", 80, "{\n  x = (<<E\nx\nE\n  ),\n}"},
		{"heredoc before operator", "(<<E\nx\nE\n+other)", 80, "(\n  <<E\nx\nE\n  + other\n)"},
		{"heredoc interpolation closer", "\"${<<E\nx\nE\n}\"", 80, "\"${\n  <<E\nx\nE\n}\""},
		{"heredoc interpolation strip closer", "\"${~<<E\nx\nE\n~}\"", 80, "\"${~\n  <<E\nx\nE\n~}\""},
		{"heredoc directive closer", "\"%{if <<E\nx\nE\n}yes%{endif}\"", 80, "\"%{\n  if <<E\nx\nE\n}yes%{endif}\""},
		{"lone CR before literal CRLF", "<<E\nx\r\r\nE\n", 80, "<<E\nx\r\r\nE"},
		{"lone CR before marker CRLF", "<<E\nx\nE\r\r\n", 80, "<<E\nx\nE\r\r"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := render(t, test.source, test.width)
			if got != test.want {
				t.Fatalf("rendered %q, want %q", got, test.want)
			}
			if again := render(t, got, test.width); again != got {
				t.Fatalf("not idempotent: %q => %q", got, again)
			}
		})
	}
}

func TestTemplateOpenTofuBoundaryCompatibility(t *testing.T) {
	if os.Getenv("TERRABLADE_COMPARE_TOFU") != "1" {
		t.Skip("set TERRABLADE_COMPARE_TOFU=1 to compare with an installed OpenTofu")
	}
	for _, source := range []string{
		`"hello ${ a+b } end"`, `"before ${~ a + b ~} after"`,
		`"%{ if a }yes%{ else }no%{ endif }"`,
		`" a %{~ if a ~} b %{~ endif ~} c "`,
		`"%{for k,v in xs}${k}:${v}%{endfor}"`,
		"<<E\n${ a+b } end\nE\n",
	} {
		output := "value = " + render(t, source, 80) + "\n"
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		command := exec.CommandContext(ctx, "tofu", "fmt", "-no-color", "-")
		command.Stdin = strings.NewReader(output)
		formatted, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("OpenTofu failed: %v\n%s", err, formatted)
		}
		if string(formatted) != output {
			t.Fatalf("OpenTofu changed boundary spacing:\n%q\n=>\n%q", output, formatted)
		}
	}
}
