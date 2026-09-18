package lowering_test

import "testing"

func TestOperationLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"binary spaces", "a+b*c", 80, "a + b * c"},
		{"all binary operators", "a||b&&c==d!=e<f<=g>h>=i+j - k*l/m%n", 100, "a || b && c == d != e < f <= g > h >= i + j - k * l / m % n"},
		{"left associative chain", "alpha + beta - gamma", 15, "(\n  alpha\n  + beta\n  - gamma\n)"},
		{"precedence group fits independently", "alpha + beta * gamma", 17, "(\n  alpha\n  + beta * gamma\n)"},
		{"precedence group breaks independently", "alpha + beta * gamma", 12, "(\n  alpha\n  + beta\n    * gamma\n)"},
		{"existing parentheses share inner group", "(alpha + beta - gamma)", 15, "(\n  alpha\n  + beta\n  - gamma\n)"},
		{"binary inside call already permits newlines", "f(alpha + beta)", 12, "f(\n  alpha\n    + beta,\n)"},
		{"safe chain has one continuation indent", "f(alpha + beta - gamma)", 14, "f(\n  alpha\n    + beta\n    - gamma,\n)"},
		{"binary comment stays inline", "(alpha # why\n + beta)", 80, "(\n  alpha # why\n  + beta\n)"},
		{"binary comment after operator", "(alpha + # why\n beta)", 80, "(\n  alpha\n  + # why\n  beta\n)"},
		{"binary block comment", "a/*x*/+/*y*/b", 80, "a /*x*/ + /*y*/ b"},
		{"parenthesis boundary comments share group", "(/*lead*/alpha+beta/*tail*/)", 16, "(\n  /*lead*/ alpha\n  + beta /*tail*/\n)"},
		{"conditional flat", "ready?yes:no", 80, "ready ? yes : no"},
		{"conditional broken", "ready ? first_value : second_value", 20, "(\n  ready\n  ? first_value\n  : second_value\n)"},
		{"nested conditional associates right", "a?b:c?d:e", 13, "(\n  a\n  ? b\n  : c ? d : e\n)"},
		{"conditional inside tuple", "[ready ? yes : no]", 12, "[\n  ready\n    ? yes\n    : no,\n]"},
		{"attribute traversal flat", "foo . bar . baz", 80, "foo.bar.baz"},
		{"attribute traversal broken", "foo.first_attribute.second_attribute", 24, "(\n  foo\n  .first_attribute\n  .second_attribute\n)"},
		{"call and traversal groups independent", "f(x,y).first_attribute.second_attribute", 30, "(\n  f(x, y)\n  .first_attribute\n  .second_attribute\n)"},
		{"unary traversal preserves precedence", "-foo.first_attribute.second_attribute", 24, "-(\n  foo\n  .first_attribute\n  .second_attribute\n)"},
		{"index expression", "foo[ 1 + i ].true", 80, "foo[1 + i].true"},
		{"index expression breaks safely", "foo[alpha + beta]", 14, "(\n  foo[\n    alpha\n      + beta\n  ]\n)"},
		{"legacy numeric boundary", "foo.0 .0", 80, "foo.0 .0"},
		{"legacy exponent boundary", "foo.0e1 .0", 80, "foo.0e1 .0"},
		{"numeric root exponent attribute", "1 . e2", 80, "1 .e2"},
		{"numeric root ordinary attribute", "1.foo", 80, "1.foo"},
		{"ordinary attribute after legacy index", "aws_instance.foo.0.id", 80, "aws_instance.foo.0.id"},
		{"ordinary attribute after legacy exponent", "foo.1e1.id", 80, "foo.1e1.id"},
		{"incomplete exponent attribute", "foo.0 .e", 80, "foo.0.e"},
		{"incomplete signed exponent attribute", "foo.0 .e-", 80, "foo.0.e-"},
		{"non numeric exponent suffix", "foo.0 .e-foo", 80, "foo.0.e-foo"},
		{"lowercase exponent attribute", "foo.0 .e2", 80, "foo.0 .e2"},
		{"uppercase exponent attribute", "foo.0 .E2", 80, "foo.0 .E2"},
		{"signed exponent attribute", "foo.0 .e-2", 80, "foo.0 .e-2"},
		{"uppercase signed exponent attribute", "foo.0 .E-2", 80, "foo.0 .E-2"},
		{"exponent prefix in longer attribute", "foo.0 .e2suffix", 80, "foo.0 .e2suffix"},
		{"signed exponent prefix in longer attribute", "foo.0 .E-2suffix", 80, "foo.0 .E-2suffix"},
		{"plus is a distinct token", "foo.0 .e+2", 80, "foo.0.e + 2"},
		{"spaced minus is a distinct token", "foo.0 .e - 2", 80, "foo.0.e - 2"},
		{"non ASCII exponent lookalike", "foo.0 .é2", 80, "foo.0.é2"},
		{"non ASCII digit does not extend number", "foo.0 .e٢", 80, "foo.0.e٢"},
		{"numeric root exponent continuation", "0.5 .e2", 80, "0.5 .e2"},
		{"legacy numeric after exponent index", "foo.1e1 .2", 80, "foo.1e1 .2"},
		{"splat after number cannot extend number", "foo.0.*.id", 80, "foo.0.*.id"},
		{"comment before exponent step separates number", "foo.0/*c*/.e2", 80, "foo.0 /*c*/.e2"},
		{"comment inside exponent step separates number", "foo.0./*c*/e2", 80, "foo.0. /*c*/ e2"},
		{"comment inside legacy index separates number", "foo.0./*c*/1", 80, "foo.0. /*c*/ 1"},
		{"legacy splat scope", "foo.*.bar[0].baz", 80, "foo.*.bar[0].baz"},
		{"full splat scope", "foo[*].bar[0].baz", 80, "foo[*].bar[0].baz"},
		{"nested full splats", "foo[*][*].bar", 80, "foo[*][*].bar"},
		{"mixed splats", "foo.*.0[*].bar", 80, "foo.*.0[*].bar"},
		{"full then legacy splat", "foo[*].*.bar", 80, "foo[*].*.bar"},
		{"index between legacy splats", "foo.*.bar[0].*.baz", 80, "foo.*.bar[0].*.baz"},
		{"legacy splat numeric boundary", "foo.*.0 .0", 80, "foo.*.0 .0"},
		{"broken full splat", "foo[*].first_attribute[0].second_attribute", 24, "(\n  foo[*]\n  .first_attribute[0]\n  .second_attribute\n)"},
		{"safe mixed traversal", "f(foo[0].first_attribute[*].second_attribute)", 24, "f(\n  foo[0]\n    .first_attribute[*]\n    .second_attribute,\n)"},
		{"safe traversal in tuple", "[foo.first_attribute.second_attribute]", 24, "[\n  foo\n    .first_attribute\n    .second_attribute,\n]"},
		{"mixed splats retain bracket attachment", "foo.*.first_attribute[0][*].second_attribute", 24, "(\n  foo\n  .*\n  .first_attribute[0][*]\n  .second_attribute\n)"},
		{"traversal comment", "(foo # base\n .bar)", 80, "(\n  foo # base\n  .bar\n)"},
		{"attribute internal comment", "foo./*step*/bar", 80, "foo. /*step*/ bar"},
		{"splat comment", "foo[/*splat*/ *].bar", 80, "foo[/*splat*/ *].bar"},
		{"splat closing comment", "foo[* /*splat*/].bar", 80, "foo[* /*splat*/].bar"},
		{"index comment", "foo[/*index*/ 0 /*tail*/]", 80, "foo[/*index*/ 0 /*tail*/]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := render(t, test.source, test.width)
			if got != test.want {
				t.Fatalf("rendered:\n%q\nwant:\n%q", got, test.want)
			}
			if again := render(t, got, test.width); again != got {
				t.Fatalf("not idempotent:\n%q\nthen:\n%q", got, again)
			}
		})
	}
}
