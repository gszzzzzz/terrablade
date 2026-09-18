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

func TestExpressionNormalization(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"variable", `"${a}"`, `a`},
		{"recursive wrappers", `"${"${"${a}"}"}"`, `a`},
		{"strip markers", `"${~a~}"`, `a`},
		{"binary", `"${a + b}"`, `a + b`},
		{"unary precedence", `-"${a + b}"`, `-(a + b)`},
		{"binary precedence", `"${a + b}" * c`, `(a + b) * c`},
		{"binary right association", `a - "${b - c}"`, `a - (b - c)`},
		{"binary left association", `"${a - b}" - c`, `a - b - c`},
		{"stronger operand", `a + "${b * c}"`, `a + b * c`},
		{"conditional condition", `"${a ? b : c}" ? d : e`, `(a ? b : c) ? d : e`},
		{"conditional arms", `a ? "${b ? c : d}" : "${e ? f : g}"`, `a ? b ? c : d : e ? f : g`},
		{"traversal precedence", `"${a + b}".name`, `(a + b).name`},
		{"traversal concatenation", `"${a.b}".name`, `a.b.name`},
		{"full splat scope", `"${a[*].b}"[0]`, `(a[*].b)[0]`},
		{"attribute splat scope", `"${a.*.b}".name`, `(a.*.b).name`},
		{"object key", `{ "${a}" = 1 }`, `{ (a) = 1 }`},
		{"literal object key", `{ "${true}" = 1 }`, `{ (true) = 1 }`},
		{"parenthesized object key", `{ "${(a)}" = 1 }`, `{ (a) = 1 }`},
		{"for identifier in tuple", `["${for}"]`, `[(for)]`},
		{"collections and calls", `f(["${a}", "${b}"], { k = "${c}" })`, `f([a, b], { k = c })`},
		{"for collection", `[for x in "${xs}" : "${x}" if "${x.enabled}"]`, `[for x in xs : x if x.enabled]`},
		{"general template remains", `" ${a}"`, `" ${a}"`},
		{"escaped interpolation remains", `"$${a}"`, `"$${a}"`},
		{"directive remains", `"%{if true}${a}%{endif}"`, `"%{if true}${a}%{endif}"`},
		{"nested general template", `"${"prefix ${"${a}"}"}"`, `"prefix ${a}"`},
		{"delimiter comments", `"${/*lead*/ a /*tail*/}"`, `(/*lead*/ a /*tail*/)`},
		{"strip comments", `"${~/*lead*/ a /*tail*/~}"`, `(/*lead*/ a /*tail*/)`},
		{"leading line comment", "\"${# lead\na}\"", "( # lead\n  a)"},
		{"trailing line comment", "\"${a # tail\n}\"", "(a # tail\n)"},
		{"line comments at both edges", "\"${# lead\na # tail\n}\"", "(   # lead\n  a # tail\n)"},
		{"duplicate comments", `"${/*same*/ "${/*same*/ a /*same*/}" /*same*/}"`, `(/*same*/ (/*same*/ a /*same*/) /*same*/)`},
		{"numeric index", `foo.0.bar`, `foo[0].bar`},
		{"consecutive numeric indices", `foo.0 .0`, `foo[0][0]`},
		{"exponent spelling", `foo.1E+2.bar`, `foo[1E+2].bar`},
		{"leading zero spelling", `foo.001.bar`, `foo[001].bar`},
		{"full splat index", `foo[*].0.bar`, `foo[*][0].bar`},
		{"attribute splat exception", `foo.*.0.bar`, `foo.*.0.bar`},
		{"nested splat exception", `foo[*].*.0.bar`, `foo[*].*.0.bar`},
		{"outside attribute splat", `foo.*.0[0].1`, `foo.*.0[0][1]`},
		{"index comment", `foo./*index*/0.bar`, `foo[/*index*/ 0].bar`},
		{"normalization composition", `"${foo.0}".bar`, `foo[0].bar`},
		{"numeric root boundary", `"${1}".e2`, `1 .e2`},
		{"unwrapped traversal comment", "\"${foo # step\n.bar}\"", "(\n  foo # step\n  .bar\n)"},
		{"unwrapped unary comment", "\"${! # why\ntrue}\"", "(! # why\n  true)"},
		{"unwrapped heredoc traversal", "\"${<<E\nx\nE\n[0]}\"", "(\n  <<E\nx\nE\n  [0]\n)"},
		{"numeric index line comment", "f(foo.// index\n0)", "f(\n  foo[\n    // index\n    0\n  ],\n)"},
		{"commented traversal in object", "{a=\"${foo # step\n.bar}\"}", "{\n  a = (\n    foo # step\n    .bar\n  ),\n}"},
		{"commented unary in call", "f(\"${! # why\ntrue}\")", "f(\n  ! # why\n  true,\n)"},
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
	source := "resource x y {\n a=\"${foo.0.bar}\"\n longer=\"${/*keep*/ a # tail\n}\"\n\n obj={\"${a}\"=\"${b}\"}\n}\n"
	want := "resource \"x\" \"y\" {\n  a = foo[0].bar\n  longer = (/*keep*/ a # tail\n  )\n\n  obj = { (a) = b }\n}\n"
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

// The upstream evaluator independently checks values and types. Strict equality
// catches object-key reinterpretation, reassociation, and shifted splat scope;
// reparsing alone would accept all three classes of semantic mistake.
func TestNormalizationOpenTofuSemantics(t *testing.T) {
	if os.Getenv("TERRABLADE_COMPARE_TOFU") != "1" {
		t.Skip("set TERRABLADE_COMPARE_TOFU=1 to evaluate with an installed OpenTofu")
	}
	var comparisons []string
	for _, source := range []string{
		`"${1}"`, `"${true}"`, `"${null}"`, `"${[1, 2]}"`, `"${{a=1}}"`,
		`"${"${"${1}"}"}"`, `"${~[1, 2]~}"`,
		`-"${1 + 2}"`, `"${1 + 2}" * 3`, `10 - "${3 - 2}"`,
		`"${10 - 3}" - 2`, `"${true ? false : true}" ? 1 : 2`,
		`true ? "${false ? 1 : 2}" : 3`,
		`{ "${"chosen"}" = 1 }`, `{ "${true}" = 1 }`,
		`[for key in ["chosen"] : { "${key}" = 1 }]`,
		`[for for in [1] : ["${for}"]]`,
		`"${[{a=1}].0}".a`, `"${[{a=1}, {a=2}][*].a}"[0]`,
		`"${[[1], [2]].*.0}"[0]`, `[[{a=1}],[{a=2}]].*.0.a`,
		`[[{a=1}],[{a=2}]][*].0.a`, `[[1],[2]].0 .0`,
		`[1, 2].01`, `[1, 2].1E+0`, `length("${[1, 2]}")`,
		`[for x in "${[1, 2]}" : "${x + 1}"]`,
		`{for k,v in "${{a=1}}" : "${k}" => "${v}"}`,
		`"prefix ${"${1}"}"`, `"${/*a*/ 1 /*b*/}"`,
	} {
		output := render(t, source, 1000)
		comparisons = append(comparisons, "(("+source+") == ("+output+"))")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "tofu", "console", "-no-color")
	command.Dir = t.TempDir()
	command.Stdin = strings.NewReader("alltrue([" + strings.Join(comparisons, ",") + "])\n")
	output, err := command.CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "true" {
		t.Fatalf("OpenTofu semantic comparison failed: %v\n%s", err, output)
	}
}

func TestNormalizationOpenTofuFormatting(t *testing.T) {
	if os.Getenv("TERRABLADE_COMPARE_TOFU") != "1" {
		t.Skip("set TERRABLADE_COMPARE_TOFU=1 to compare with an installed OpenTofu")
	}
	for _, source := range []string{
		`"${foo.0.bar}"`, `"${"${a}"}"`, `a - "${b - c}"`, `-"${a+b}"`,
		`{"${a}"="${b}"}`, `"${x[*].a}".0`, `foo.*.0`, `foo[*].0`,
		`"${~/*lead*/ a /*tail*/~}"`,
		"\"${# lead\na # tail\n}\"", "\"${foo # c\n.bar}\"",
		"f(foo.// c\n0)", "\"${{\na=1\n}}\"",
	} {
		for _, width := range []int{16, 80} {
			output := renderFile(t, "value = "+source+"\n", width)
			assertOpenTofu(t, output)
		}
	}
}
