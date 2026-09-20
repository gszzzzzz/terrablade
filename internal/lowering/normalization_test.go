package lowering_test

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/reference"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestExpressionNormalization(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"variable", `"${a}"`, `a`},
		{"recursive wrappers", `"${"${"${a}"}"}"`, `a`},
		{"strip markers", `"${~a~}"`, `a`},
		{"binary", `"${a + b}"`, `a + b`},

		// Precedence and association after a wrapper is removed.
		{"unary precedence", `-"${a + b}"`, `-(a + b)`},
		{"binary precedence", `"${a + b}" * c`, `(a + b) * c`},
		{"binary right association", `a - "${b - c}"`, `a - (b - c)`},
		{"binary left association", `"${a - b}" - c`, `a - b - c`},
		{"stronger operand", `a + "${b * c}"`, `a + b * c`},
		{"conditional binary operand", `a + "${b ? c : d}"`, `a + (b ? c : d)`},
		{"nested unary operand", `-"${-a}"`, `--a`},
		{"unary traversal base", `"${-a}".b`, `(-a).b`},
		{"conditional condition", `"${a ? b : c}" ? d : e`, `(a ? b : c) ? d : e`},
		{"conditional arms", `a ? "${b ? c : d}" : "${e ? f : g}"`, `a ? b ? c : d : e ? f : g`},
		{"traversal precedence", `"${a + b}".name`, `(a + b).name`},
		{"traversal concatenation", `"${a.b}".name`, `a.b.name`},
		{"full splat scope", `"${a[*].b}"[0]`, `(a[*].b)[0]`},
		{"attribute splat scope", `"${a.*.b}".name`, `(a.*.b).name`},

		// Object keys: only an unambiguous literal key loses its parentheses.
		{"object key", `{ "${a}" = 1 }`, `{ (a) = 1 }`},
		{"literal object key", `{ "${true}" = 1 }`, `{ (true) = 1 }`},
		{"quoted literal object key", `{ "${"k"}" = 1 }`, `{ "k" = 1 }`},
		{"nested quoted literal object key", `{ "${"${"k"}"}" = 1 }`, `{ "k" = 1 }`},
		{"empty literal object key", `{ "${""}" = 1 }`, `{ "" = 1 }`},
		{"escaped literal object key", `{ "${"\u006b"}" = 1 }`, `{ "\u006b" = 1 }`},
		{"escaped interpolation object key", `{ "${"$${a}"}" = 1 }`, `{ "$${a}" = 1 }`},
		{"computed template object key", `{ "${"prefix ${a}"}" = 1 }`, `{ ("prefix ${a}") = 1 }`},
		{"commented literal object key", `{ "${/*key*/ "k"}" = 1 }`, `{ (/*key*/ "k") = 1 }`},
		{"parenthesized object key", `{ "${(a)}" = 1 }`, `{ (a) = 1 }`},
		{"for identifier in tuple", `["${for}"]`, `[(for)]`},
		{"collections and calls", `f(["${a}", "${b}"], { k = "${c}" })`, `f([a, b], { k = c })`},
		{"for collection", `[for x in "${xs}" : "${x}" if "${x.enabled}"]`, `[for x in xs : x if x.enabled]`},

		// Templates that are not pure wrappers keep their spelling.
		{"general template remains", `" ${a}"`, `" ${a}"`},
		{"escaped interpolation remains", `"$${a}"`, `"$${a}"`},
		{"directive remains", `"%{if true}${a}%{endif}"`, `"%{if true}${a}%{endif}"`},
		{"nested general template", `"${"prefix ${"${a}"}"}"`, `"prefix ${a}"`},

		// Wrapper-edge comments move inside permanent parentheses.
		{"delimiter comments", `"${/*lead*/ a /*tail*/}"`, `(/*lead*/ a /*tail*/)`},
		{"strip comments", `"${~/*lead*/ a /*tail*/~}"`, `(/*lead*/ a /*tail*/)`},
		{"leading line comment", lines(`"${# lead`, `a}"`), lines("( # lead", "  a)")},
		{"trailing line comment", lines(`"${a # tail`, `}"`), lines("(a # tail", ")")},
		{"line comments at both edges", lines(`"${# lead`, "a # tail", `}"`), lines("(   # lead", "  a # tail", ")")},
		{
			name:   "duplicate comments",
			source: `"${/*same*/ "${/*same*/ a /*same*/}" /*same*/}"`,
			want:   `(/*same*/ (/*same*/ a /*same*/) /*same*/)`,
		},

		// Legacy numeric indices become bracket indices outside .* steps.
		{"numeric index", `foo.0.bar`, `foo[0].bar`},
		{"consecutive numeric indices", `foo.0 .0`, `foo[0][0]`},
		{"exponent spelling", `foo.1E+2.bar`, `foo[1E+2].bar`},
		{"leading zero spelling", `foo.001.bar`, `foo[001].bar`},
		{"full splat index", `foo[*].0.bar`, `foo[*][0].bar`},
		{"attribute splat exception", `foo.*.0.bar`, `foo.*.0.bar`},
		{"nested splat exception", `foo[*].*.0.bar`, `foo[*].*.0.bar`},
		{"outside attribute splat", `foo.*.0[0].1`, `foo.*.0[0][1]`},
		{"index comment", `foo./*index*/0.bar`, `foo[/*index*/ 0].bar`},

		// The two rewrites compose, and exposed lines gain parentheses.
		{"normalization composition", `"${foo.0}".bar`, `foo[0].bar`},
		{"numeric root boundary", `"${1}".e2`, `1 .e2`},
		{"unwrapped traversal comment", lines(`"${foo # step`, `.bar}"`), lines("(", "  foo # step", "  .bar", ")")},
		{"unwrapped unary comment", lines(`"${! # why`, `true}"`), lines("(! # why", "  true)")},
		{"unwrapped heredoc traversal", lines(`"${<<E`, "x", "E", `[0]}"`), lines("(", "  <<E", "x", "E", "  [0]", ")")},
		{
			name:   "numeric index line comment",
			source: lines("f(foo.// index", "0)"),
			want:   lines("f(", "  foo[", "    // index", "    0", "  ],", ")"),
		},
		{
			name:   "commented traversal in object",
			source: lines(`{a="${foo # step`, `.bar}"}`),
			want:   lines("{", "  a = (", "    foo # step", "    .bar", "  ),", "}"),
		},
		{"commented unary in call", lines(`f("${! # why`, `true}")`), lines("f(", "  ! # why", "  true,", ")")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := render(t, test.source, 100); got != test.want {
				t.Fatalf("render = %q, want %q", got, test.want)
			}
			for _, width := range []int{1, 12, 100} {
				output := render(t, test.source, width)
				before, original := parse(t, test.source)
				after, normalized := parse(t, output)
				if !reflect.DeepEqual(expressionTokens(before, original), expressionTokens(after, normalized)) {
					t.Fatalf("canonical syntax or comments changed: %q => %q", test.source, output)
				}
				if again := render(t, output, width); again != output {
					t.Fatalf("width %d not idempotent: %q => %q", width, output, again)
				}
			}
		})
	}
}

func TestNormalizationFileAndCST(t *testing.T) {
	source := lines(
		"resource x y {", ` a="${foo.0.bar}"`, ` longer="${/*keep*/ a # tail`,
		`}"`, "", ` obj={"${a}"="${b}"}`, "}", "",
	)
	want := lines(
		`resource "x" "y" {`, "  a = foo[0].bar", "  longer = (/*keep*/ a # tail",
		"  )", "", "  obj = { (a) = b }", "}", "",
	)
	result := syntax.Parse([]byte(source))
	doc, err := lowering.File(result)
	if err != nil {
		t.Fatal(err)
	}
	if got := document.Render(doc, document.Options{}); got != want {
		t.Fatalf("File output = %q, want %q", got, want)
	}
	if result.Source() != source || result.Text(result.Root().Span()) != source {
		t.Fatal("normalization mutated the lossless CST")
	}
	assertFileContent(t, source, want)
	if next := renderFile(t, want, 80); next != want {
		t.Fatalf("File not idempotent: %q", next)
	}
}

func TestNormalizationHeredocTrailingComments(t *testing.T) {
	for _, source := range []string{
		lines(`a = "${<<E`, "x", "E", `}" # tail`, ""),
		lines(`a = "${<<E`, "x", "E", `}" # tail`, "b=1", ""),
		lines(`a = "${<<E`, "x", "E", `}" /*tail*/`, "b=1", ""),
		lines(`a = "${<<E`, "x", "E", `}" /*first*/ /*second*/ # tail`, "b {}", ""),
		lines("b {", ` a = "${<<E`, "x", "E", `}" # tail`, "}", ""),
	} {
		output := renderFile(t, source, 80)
		assertFileContent(t, source, output)
		if next := renderFile(t, output, 80); next != output {
			t.Fatalf("not idempotent: %q => %q", output, next)
		}
		if reference.Enabled() {
			assertReferenceFormat(t, output)
		}
	}
}

// The upstream evaluator independently checks values and types. Strict
// equality catches object-key reinterpretation, reassociation, and shifted
// splat scope; reparsing alone would accept all three classes of semantic
// mistake.
//
// The two reference tools answer a non-interactive console differently:
// Terraform prints only the last expression's result, while OpenTofu prints
// one result per expression it reads. Every case is therefore folded into a
// single expression whose one answer spells out every case, which both tools
// print the same way, and a failure still names the case that broke. Folding
// into alltrue([...]) instead would cost the same one process but a failure
// could then only say that something changed.
func TestNormalizationReferenceSemantics(t *testing.T) {
	cli := reference.CLI(t)
	sources := []string{
		`"${1}"`, `"${true}"`, `"${null}"`, `"${[1, 2]}"`, `"${{a=1}}"`,
		`"${"${"${1}"}"}"`, `"${~[1, 2]~}"`,
		`-"${1 + 2}"`, `"${1 + 2}" * 3`, `10 - "${3 - 2}"`,
		`"${10 - 3}" - 2`, `"${true ? false : true}" ? 1 : 2`,
		`1 + "${true ? 2 : 3}"`, `-"${-2}"`,
		`true ? "${false ? 1 : 2}" : 3`,
		`{ "${"chosen"}" = 1 }`, `{ "${true}" = 1 }`,
		`{ "${""}" = 1 }`, `{ "${"$${a}"}" = 1 }`, `{ "${"\u006b"}" = 1 }`,
		`[for key in ["chosen"] : { "${"prefix ${key}"}" = 1 }]`,
		`[for key in ["chosen"] : { "${key}" = 1 }]`,
		`[for for in [1] : ["${for}"]]`,
		`"${[{a=1}].0}".a`, `"${[{a=1}, {a=2}][*].a}"[0]`,
		`"${[[1], [2]].*.0}"[0]`, `[[{a=1}],[{a=2}]].*.0.a`,
		`[[{a=1}],[{a=2}]][*].0.a`, `[[1],[2]].0 .0`,
		`[1, 2].01`, `[1, 2].1E+0`, `length("${[1, 2]}")`,
		`[for x in "${[1, 2]}" : "${x + 1}"]`,
		`{for k,v in "${{a=1}}" : "${k}" => "${v}"}`,
		`"prefix ${"${1}"}"`, `"${/*a*/ 1 /*b*/}"`,
	}

	comparisons := make([]string, len(sources))
	for i, source := range sources {
		comparisons[i] = "((" + source + ") == (" + render(t, source, 1000) + "))"
		if strings.ContainsAny(comparisons[i], "\r\n") {
			t.Fatalf("case %q spans lines, so it cannot join the one console expression: %q", source, comparisons[i])
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, cli, "console", "-no-color")
	command.Dir = t.TempDir()
	command.Stdin = strings.NewReader(
		`join(",", [for answer in [` + strings.Join(comparisons, ", ") + `] : tostring(answer)])` + "\n")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf(lines("reference CLI semantic comparison failed: %v", "%s"), err, output)
	}

	answers := strings.Split(strings.Trim(strings.TrimSpace(string(output)), `"`), ",")
	if len(answers) != len(sources) {
		// A diagnostic instead of the joined string, or a console that folded
		// the answer over several lines, would misalign every case, so nothing
		// is reported per case unless the counts line up.
		t.Fatalf(lines("reference CLI answered %d of %d cases:", "%s"), len(answers), len(sources), output)
	}
	for i, answer := range answers {
		if answer != "true" {
			t.Errorf("normalization changed the value of %q: %s is %s", sources[i], comparisons[i], answer)
		}
	}
}

func TestNormalizationReferenceFormatting(t *testing.T) {
	reference.CLI(t)
	for _, source := range []string{
		`"${foo.0.bar}"`, `"${"${a}"}"`, `a - "${b - c}"`, `-"${a+b}"`,
		`{"${a}"="${b}"}`, `"${x[*].a}".0`, `foo.*.0`, `foo[*].0`,
		`{"${"k"}"=1}`, `{"${""}"=1}`, `{"${"$${a}"}"=1}`,
		`"${~/*lead*/ a /*tail*/~}"`,
		lines(`"${# lead`, "a # tail", `}"`), lines(`"${foo # c`, `.bar}"`),
		lines("f(foo.// c", "0)"), lines(`"${{`, "a=1", `}}"`),
	} {
		for _, width := range []int{16, 80} {
			output := renderFile(t, "value = "+source+"\n", width)
			assertReferenceFormat(t, output)
		}
	}
}
