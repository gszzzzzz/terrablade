package lowering_test

import (
	"context"
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
		{"multiline expression reflows", "\"prefix ${\n  a + b\n}\"", 80, `"prefix ${a + b}"`},
		{"interpolation overflows", `"prefix ${alpha + beta}"`, 12, `"prefix ${alpha + beta}"`},
		{"nested groups stay flat", `"prefix ${f([alpha,beta], ready ? foo.bar[0] : baz)}"`, 1, `"prefix ${f([alpha, beta], ready ? foo.bar[0] : baz)}"`},
		{"template overrides source vertical object", "\"prefix ${{\n a=1\n}}\"", 1, `"prefix ${ { a = 1 } }"`},
		{"template overrides nested source vertical object", "\"prefix ${f([{\n a={\n b=2\n}\n}])}\"", 1, `"prefix ${f([{ a = { b = 2 } }])}"`},
		{"template overrides source vertical empty object", "\"prefix ${{\n}}\"", 1, `"prefix ${ {} }"`},
		{"template object keeps comment line", "\"prefix ${{\n a=1 # keep\n}}\"", 1, "\"prefix ${ { a = 1 # keep\n  } }\""},
		{"template object keeps heredoc line", "\"prefix ${{\n a=<<E\nx\nE\n}}\"", 1, "\"prefix ${ { a = <<E\nx\nE\n  } }\""},
		{"directive overrides source vertical object", "\"%{if {\n a=1\n}}yes%{endif}\"", 1, `"%{if { a = 1 } }yes%{endif}"`},
		{"heredoc overrides source vertical object", "<<-E\n  ${{\n a=1\n}}\n  E\n", 1, "<<-E\n  ${ { a = 1 } }\n  E"},
		{"object strip boundaries", `"prefix ${~{a=1}~}"`, 1, `"prefix ${~ { a = 1 } ~}"`},
		{"object comments supply boundaries", `"prefix ${~/*a*/{a=1}/*b*/~}"`, 1, `"prefix ${~/*a*/ { a = 1 } /*b*/~}"`},
		{"conditional ending in object", `"prefix ${true?{}:{}}"`, 1, `"prefix ${true ? {} : {} }"`},
		{"object for boundaries", `"prefix ${{for k,v in x:k=>v}}"`, 1, `"prefix ${ { for k, v in x : k => v } }"`},
		{"interpolation comments", "\"prefix ${a # why\n + b}\"", 1, "\"prefix ${a # why\n  + b}\""},
		{"strip interpolation overflows", `"prefix ${~alpha + beta~}"`, 1, `"prefix ${~alpha + beta~}"`},
		{"directive overflows", `"%{if alpha && beta}yes%{endif}"`, 1, `"%{if alpha && beta}yes%{endif}"`},
		{"multiline directive reflows", "\"%{~if\n alpha && beta\n~}yes%{~endif~}\"", 1, `"%{~if alpha && beta~}yes%{~endif~}"`},
		{"directives", `"%{ if a }yes%{ else }no%{ endif }"`, 80, `"%{if a}yes%{else}no%{endif}"`},
		{"directive strip", `" a %{~ if a ~} b %{~ endif ~} c "`, 80, `" a %{~if a~} b %{~endif~} c "`},
		{"for directive", `"%{for k,v in xs}${k}:${v}%{endfor}"`, 80, `"%{for k, v in xs}${k}:${v}%{endfor}"`},
		{"directive binding comma precedes comment", "\"%{for k # binding\n,v in xs}${v}%{endfor}\"", 80, "\"%{for k, # binding\n  v in xs}${v}%{endfor}\""},
		{"nested directive", `"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`, 80, `"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`},
		{"nested quoted interpolation", `"${"inner ${ a }"}"`, 80, `"inner ${a}"`},
		{"quoted object key", `{"a"=1}`, 80, `{ "a" = 1 }`},
		{"empty heredoc", "<<E\nE\n", 80, "<<E\nE"},
		{"heredoc literal indentation", "<<-E\r\n  alpha\r\n\r\n \tbeta\r\n  E \t\r\n", 8, "<<-E\n  alpha\n\n \tbeta\n  E \t"},
		{"heredoc interpolation", "<<E\n  ${ a+b } text\nE\n", 80, "<<E\n  ${a + b} text\nE"},
		{"heredoc interpolation reflow", "<<-E\n  ${\n a + b\n} end\nE\n", 80, "<<-E\n  ${a + b} end\nE"},
		{"heredoc interpolation overflows", "<<-E\n  ${alpha + beta}\nE\n", 12, "<<-E\n  ${alpha + beta}\nE"},
		{"indented heredoc directive overflows", "<<-E\n  %{if alpha && beta}${f([alpha,beta])}%{endif}\n  E\n", 1, "<<-E\n  %{if alpha && beta}${f([alpha, beta])}%{endif}\n  E"},
		{"heredoc directives", "<<E\n%{for x in xs}\n${ x }\n%{endfor}\nE\n", 80, "<<E\n%{for x in xs}\n${x}\n%{endfor}\nE"},
		{"heredoc in tuple", "[<<E\nx\nE\n]", 80, "[\n  <<E\nx\nE\n]"},
		{"heredoc tuple source trailing comma", "[<<E\nx\nE\n,]", 80, "[\n  <<E\nx\nE\n]"},
		{"heredoc final call argument", "f(<<E\nx\nE\n)", 80, "f(\n  <<E\nx\nE\n)"},
		{"heredoc call source trailing comma", "f(<<E\nx\nE\n,)", 80, "f(\n  <<E\nx\nE\n)"},
		{"heredoc tuple middle keeps comma", "[<<E\nx\nE\n,1]", 80, "[\n  <<E\nx\nE\n  ,\n  1,\n]"},
		{"heredoc trailing comma preserves comment", "[<<E\nx\nE\n, # keep\n]", 80, "[\n  <<E\nx\nE\n  # keep\n]"},
		{"heredoc in call", "f(<<E\nx\nE\n,1)", 80, "f(\n  <<E\nx\nE\n  ,\n  1,\n)"},
		{"heredoc expanded argument", "f(<<E\nx\nE\n...)", 80, "f(\n  <<E\nx\nE\n  ...\n)"},
		{"heredoc in index", "foo[<<E\nx\nE\n]", 80, "foo[\n  <<E\nx\nE\n]"},
		{"heredoc before traversal", "(<<E\nx\nE\n[0])", 80, "(\n  <<E\nx\nE\n  [0]\n)"},
		{"heredoc in for collection", "[for x in <<E\nx\nE\n:x]", 80, "[\n  for x in <<E\nx\nE\n  :\n  x\n]"},
		{"heredoc for projection before condition", "[for x in xs:<<E\nx\nE\nif x]", 80, "[\n  for x in xs :\n  <<E\nx\nE\n  if x\n]"},
		{"heredoc for key before arrow", "{for x in xs:<<E\nx\nE\n=>x}", 80, "{\n  for x in xs :\n  <<E\nx\nE\n  => x\n}"},
		{"heredoc in object has no trailing comma", "{x=<<E\nx\nE\n}", 80, "{\n  x = <<E\nx\nE\n}"},
		{"heredoc separates object entries", "{x=<<E\nx\nE\ny=2}", 80, "{\n  x = <<E\nx\nE\n  y = 2,\n}"},
		{"heredoc object blank line", "{x=<<E\nx\nE\n\n\ny=2}", 80, "{\n  x = <<E\nx\nE\n\n  y = 2,\n}"},
		{"heredoc object standalone comment", "{x=<<E\nx\nE\n# next\ny=2}", 80, "{\n  x = <<E\nx\nE\n  # next\n  y = 2,\n}"},
		{"parenthesized heredoc object value keeps comma", "{x=(<<E\nx\nE\n)}", 80, "{\n  x = (<<E\nx\nE\n  ),\n}"},
		{"heredoc before operator", "(<<E\nx\nE\n+other)", 80, "(\n  <<E\nx\nE\n  + other\n)"},
		{"heredoc interpolation closer", "\"prefix ${<<E\nx\nE\n}\"", 80, "\"prefix ${<<E\nx\nE\n}\""},
		{"heredoc interpolation strip closer", "\"prefix ${~<<E\nx\nE\n~}\"", 80, "\"prefix ${~<<E\nx\nE\n~}\""},
		{"heredoc directive closer", "\"%{if <<E\nx\nE\n}yes%{endif}\"", 80, "\"%{if <<E\nx\nE\n}yes%{endif}\""},
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

func TestTemplateReferenceBoundaryCompatibility(t *testing.T) {
	reference := referenceCLI(t)
	for _, source := range []string{
		`"hello ${ a+b } end"`, `"before ${~ a + b ~} after"`,
		`"%{ if a }yes%{ else }no%{ endif }"`,
		`" a %{~ if a ~} b %{~ endif ~} c "`,
		`"%{for k,v in xs}${k}:${v}%{endfor}"`,
		"<<E\n${ a+b } end\nE\n",
		`"prefix ${f([alpha,beta], ready ? foo.bar[0] : baz)}"`,
		`"%{if alpha && beta}yes%{endif}"`,
		"<<-E\n  ${alpha + beta}\n  E\n",
		`"prefix ${~{a=1}~}"`, `"prefix ${~/*a*/{a=1}/*b*/~}"`,
		`"prefix ${true?{}:{}}"`, `"prefix ${{for k,v in x:k=>v}}"`,
		`"prefix %{~if {}~}yes%{endif}"`,
	} {
		output := "value = " + render(t, source, 1) + "\n"
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		command := exec.CommandContext(ctx, reference, "fmt", "-no-color", "-")
		command.Stdin = strings.NewReader(output)
		formatted, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("reference CLI failed: %v\n%s", err, formatted)
		}
		if string(formatted) != output {
			t.Fatalf("reference CLI changed boundary spacing:\n%q\n=>\n%q", output, formatted)
		}
	}
}
