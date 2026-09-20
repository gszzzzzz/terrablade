package lowering_test

import (
	"testing"

	"github.com/gszzzzzz/terrablade/internal/reference"
)

func TestTemplateLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"empty quote", `""`, 1, `""`},
		{"literal escapes", `"a\n\u0041$${x}%%{if}"`, 8, `"a\n\u0041$${x}%%{if}"`},

		// Sequences flatten their contents and keep strip markers.
		{"interpolation spaces", `"hello ${ a+b } end"`, 80, `"hello ${a + b} end"`},
		{"strip markers", `"before ${~ a + b ~} after"`, 80, `"before ${~a + b~} after"`},
		{"multiline expression reflows", lines(`"prefix ${`, "  a + b", `}"`), 80, `"prefix ${a + b}"`},
		{"interpolation overflows", `"prefix ${alpha + beta}"`, 12, `"prefix ${alpha + beta}"`},
		{
			name:   "nested groups stay flat",
			source: `"prefix ${f([alpha,beta], ready ? foo.bar[0] : baz)}"`,
			width:  1,
			want:   `"prefix ${f([alpha, beta], ready ? foo.bar[0] : baz)}"`,
		},
		{"template overrides source vertical object", lines(`"prefix ${{`, " a=1", `}}"`), 1, `"prefix ${ { a = 1 } }"`},
		{
			name:   "template overrides nested source vertical object",
			source: lines(`"prefix ${f([{`, " a={", " b=2", "}", `}])}"`),
			width:  1,
			want:   `"prefix ${f([{ a = { b = 2 } }])}"`,
		},
		{"template overrides source vertical empty object", lines(`"prefix ${{`, `}}"`), 1, `"prefix ${ {} }"`},
		{
			name:   "template object keeps comment line",
			source: lines(`"prefix ${{`, " a=1 # keep", `}}"`),
			width:  1,
			want:   lines(`"prefix ${ { a = 1 # keep`, `  } }"`),
		},
		{
			name:   "template object keeps heredoc line",
			source: lines(`"prefix ${{`, " a=<<E", "x", "E", `}}"`),
			width:  1,
			want:   lines(`"prefix ${ { a = <<E`, "x", "E", `  } }"`),
		},
		{
			name:   "directive overrides source vertical object",
			source: lines(`"%{if {`, " a=1", `}}yes%{endif}"`),
			width:  1,
			want:   `"%{if { a = 1 } }yes%{endif}"`,
		},
		{
			name:   "heredoc overrides source vertical object",
			source: lines("<<-E", "  ${{", " a=1", "}}", "  E", ""),
			width:  1,
			want:   lines("<<-E", "  ${ { a = 1 } }", "  E"),
		},
		{"object strip boundaries", `"prefix ${~{a=1}~}"`, 1, `"prefix ${~ { a = 1 } ~}"`},
		{"object comments supply boundaries", `"prefix ${~/*a*/{a=1}/*b*/~}"`, 1, `"prefix ${~/*a*/ { a = 1 } /*b*/~}"`},
		{"conditional ending in object", `"prefix ${true?{}:{}}"`, 1, `"prefix ${true ? {} : {} }"`},
		{"object for boundaries", `"prefix ${{for k,v in x:k=>v}}"`, 1, `"prefix ${ { for k, v in x : k => v } }"`},
		{"interpolation comments", lines(`"prefix ${a # why`, ` + b}"`), 1, lines(`"prefix ${a # why`, `  + b}"`)},
		{"strip interpolation overflows", `"prefix ${~alpha + beta~}"`, 1, `"prefix ${~alpha + beta~}"`},
		{"directive overflows", `"%{if alpha && beta}yes%{endif}"`, 1, `"%{if alpha && beta}yes%{endif}"`},
		{
			name:   "multiline directive reflows",
			source: lines(`"%{~if`, " alpha && beta", `~}yes%{~endif~}"`),
			width:  1,
			want:   `"%{~if alpha && beta~}yes%{~endif~}"`,
		},

		// Directives.
		{"directives", `"%{ if a }yes%{ else }no%{ endif }"`, 80, `"%{if a}yes%{else}no%{endif}"`},
		{"directive strip", `" a %{~ if a ~} b %{~ endif ~} c "`, 80, `" a %{~if a~} b %{~endif~} c "`},
		{"for directive", `"%{for k,v in xs}${k}:${v}%{endfor}"`, 80, `"%{for k, v in xs}${k}:${v}%{endfor}"`},
		{
			name:   "directive binding comma precedes comment",
			source: lines(`"%{for k # binding`, `,v in xs}${v}%{endfor}"`),
			width:  80,
			want:   lines(`"%{for k, # binding`, `  v in xs}${v}%{endfor}"`),
		},
		{
			name:   "nested directive",
			source: `"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`,
			width:  80,
			want:   `"%{if a}%{for x in xs}${x}%{endfor}%{else}none%{endif}"`,
		},
		{"nested quoted interpolation", `"${"inner ${ a }"}"`, 80, `"inner ${a}"`},
		{"quoted object key", `{"a"=1}`, 80, `{ "a" = 1 }`},

		// Heredocs keep literal lines and their marker's newline.
		{"empty heredoc", lines("<<E", "E", ""), 80, lines("<<E", "E")},
		{

			// Escaped: tabs and CRLF are the literal heredoc bytes under test.
			name:   "heredoc literal indentation",
			source: "<<-E\r\n  alpha\r\n\r\n \tbeta\r\n  E \t\r\n",
			width:  8,
			want:   "<<-E\n  alpha\n\n \tbeta\n  E \t",
		},
		{"heredoc interpolation", lines("<<E", "  ${ a+b } text", "E", ""), 80, lines("<<E", "  ${a + b} text", "E")},
		{
			name:   "heredoc interpolation reflow",
			source: lines("<<-E", "  ${", " a + b", "} end", "E", ""),
			width:  80,
			want:   lines("<<-E", "  ${a + b} end", "E"),
		},
		{
			name:   "heredoc interpolation overflows",
			source: lines("<<-E", "  ${alpha + beta}", "E", ""),
			width:  12,
			want:   lines("<<-E", "  ${alpha + beta}", "E"),
		},
		{
			name:   "indented heredoc directive overflows",
			source: lines("<<-E", "  %{if alpha && beta}${f([alpha,beta])}%{endif}", "  E", ""),
			width:  1,
			want:   lines("<<-E", "  %{if alpha && beta}${f([alpha, beta])}%{endif}", "  E"),
		},
		{
			name:   "heredoc directives",
			source: lines("<<E", "%{for x in xs}", "${ x }", "%{endfor}", "E", ""),
			width:  80,
			want:   lines("<<E", "%{for x in xs}", "${x}", "%{endfor}", "E"),
		},

		// A heredoc marker acts as the separator before a closer or comma.
		{"heredoc in tuple", lines("[<<E", "x", "E", "]"), 80, lines("[", "  <<E", "x", "E", "]")},
		{"heredoc tuple source trailing comma", lines("[<<E", "x", "E", ",]"), 80, lines("[", "  <<E", "x", "E", "]")},
		{"heredoc final call argument", lines("f(<<E", "x", "E", ")"), 80, lines("f(", "  <<E", "x", "E", ")")},
		{"heredoc call source trailing comma", lines("f(<<E", "x", "E", ",)"), 80, lines("f(", "  <<E", "x", "E", ")")},
		{
			name:   "heredoc tuple middle keeps comma",
			source: lines("[<<E", "x", "E", ",1]"),
			width:  80,
			want:   lines("[", "  <<E", "x", "E", "  ,", "  1,", "]"),
		},
		{
			name:   "heredoc trailing comma preserves comment",
			source: lines("[<<E", "x", "E", ", # keep", "]"),
			width:  80,
			want:   lines("[", "  <<E", "x", "E", "  # keep", "]"),
		},
		{"heredoc in call", lines("f(<<E", "x", "E", ",1)"), 80, lines("f(", "  <<E", "x", "E", "  ,", "  1,", ")")},
		{"heredoc expanded argument", lines("f(<<E", "x", "E", "...)"), 80, lines("f(", "  <<E", "x", "E", "  ...", ")")},
		{"heredoc in index", lines("foo[<<E", "x", "E", "]"), 80, lines("foo[", "  <<E", "x", "E", "]")},
		{"heredoc before traversal", lines("(<<E", "x", "E", "[0])"), 80, lines("(", "  <<E", "x", "E", "  [0]", ")")},
		{
			name:   "heredoc in for collection",
			source: lines("[for x in <<E", "x", "E", ":x]"),
			width:  80,
			want:   lines("[", "  for x in <<E", "x", "E", "  :", "  x", "]"),
		},
		{
			name:   "heredoc for projection before condition",
			source: lines("[for x in xs:<<E", "x", "E", "if x]"),
			width:  80,
			want:   lines("[", "  for x in xs :", "  <<E", "x", "E", "  if x", "]"),
		},
		{
			name:   "heredoc for key before arrow",
			source: lines("{for x in xs:<<E", "x", "E", "=>x}"),
			width:  80,
			want:   lines("{", "  for x in xs :", "  <<E", "x", "E", "  => x", "}"),
		},
		{
			name:   "heredoc in object has no trailing comma",
			source: lines("{x=<<E", "x", "E", "}"),
			width:  80,
			want:   lines("{", "  x = <<E", "x", "E", "}"),
		},
		{
			name:   "heredoc separates object entries",
			source: lines("{x=<<E", "x", "E", "y=2}"),
			width:  80,
			want:   lines("{", "  x = <<E", "x", "E", "  y = 2,", "}"),
		},
		{
			name:   "heredoc object blank line",
			source: lines("{x=<<E", "x", "E", "", "", "y=2}"),
			width:  80,
			want:   lines("{", "  x = <<E", "x", "E", "", "  y = 2,", "}"),
		},
		{
			name:   "heredoc object standalone comment",
			source: lines("{x=<<E", "x", "E", "# next", "y=2}"),
			width:  80,
			want:   lines("{", "  x = <<E", "x", "E", "  # next", "  y = 2,", "}"),
		},
		{
			name:   "parenthesized heredoc object value keeps comma",
			source: lines("{x=(<<E", "x", "E", ")}"),
			width:  80,
			want:   lines("{", "  x = (<<E", "x", "E", "  ),", "}"),
		},
		{"heredoc before operator", lines("(<<E", "x", "E", "+other)"), 80, lines("(", "  <<E", "x", "E", "  + other", ")")},
		{"heredoc interpolation closer", lines(`"prefix ${<<E`, "x", "E", `}"`), 80, lines(`"prefix ${<<E`, "x", "E", `}"`)},
		{
			name:   "heredoc interpolation strip closer",
			source: lines(`"prefix ${~<<E`, "x", "E", `~}"`),
			width:  80,
			want:   lines(`"prefix ${~<<E`, "x", "E", `~}"`),
		},
		{
			name:   "heredoc directive closer",
			source: lines(`"%{if <<E`, "x", "E", `}yes%{endif}"`),
			width:  80,
			want:   lines(`"%{if <<E`, "x", "E", `}yes%{endif}"`),
		},

		// Escaped below: these cases are about the exact bytes, not layout.
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
	reference.CLI(t)
	for _, source := range []string{
		`"hello ${ a+b } end"`, `"before ${~ a + b ~} after"`,
		`"%{ if a }yes%{ else }no%{ endif }"`,
		`" a %{~ if a ~} b %{~ endif ~} c "`,
		`"%{for k,v in xs}${k}:${v}%{endfor}"`,
		lines("<<E", "${ a+b } end", "E", ""),
		`"prefix ${f([alpha,beta], ready ? foo.bar[0] : baz)}"`,
		`"%{if alpha && beta}yes%{endif}"`,
		lines("<<-E", "  ${alpha + beta}", "  E", ""),
		`"prefix ${~{a=1}~}"`, `"prefix ${~/*a*/{a=1}/*b*/~}"`,
		`"prefix ${true?{}:{}}"`, `"prefix ${{for k,v in x:k=>v}}"`,
		`"prefix %{~if {}~}yes%{endif}"`,
	} {
		// Boundary spacing is the point here, but the check is the same one
		// every other reference test makes: fmt must change nothing.
		assertReferenceFormat(t, "value = "+render(t, source, 1)+"\n")
	}
}
