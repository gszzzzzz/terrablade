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
		{"precedence group breaks independently", "alpha + beta * gamma", 12, "(\n  alpha\n  + beta\n  * gamma\n)"},
		{"existing parentheses share inner group", "(alpha + beta - gamma)", 15, "(\n  alpha\n  + beta\n  - gamma\n)"},
		{"binary inside call already permits newlines", "f(alpha + beta)", 12, "f(\n  alpha\n  + beta,\n)"},
		{"binary comment stays inline", "(alpha # why\n + beta)", 80, "(\n  alpha # why\n  + beta\n)"},
		{"binary comment after operator", "(alpha + # why\n beta)", 80, "(\n  alpha\n  + # why\n  beta\n)"},
		{"binary block comment", "a/*x*/+/*y*/b", 80, "a /*x*/ + /*y*/ b"},
		{"parenthesis boundary comments share group", "(/*lead*/alpha+beta/*tail*/)", 16, "(\n  /*lead*/ alpha\n  + beta /*tail*/\n)"},
		{"conditional flat", "ready?yes:no", 80, "ready ? yes : no"},
		{"conditional broken", "ready ? first_value : second_value", 20, "(\n  ready\n  ? first_value\n  : second_value\n)"},
		{"nested conditional associates right", "a?b:c?d:e", 13, "(\n  a\n  ? b\n  : c ? d : e\n)"},
		{"conditional inside tuple", "[ready ? yes : no]", 12, "[\n  ready\n  ? yes\n  : no,\n]"},
		{"attribute traversal flat", "foo . bar . baz", 80, "foo.bar.baz"},
		{"attribute traversal broken", "foo.first_attribute.second_attribute", 24, "(\n  foo\n  .first_attribute\n  .second_attribute\n)"},
		{"call and traversal groups independent", "f(x,y).first_attribute.second_attribute", 30, "(\n  f(x, y)\n  .first_attribute\n  .second_attribute\n)"},
		{"unary traversal preserves precedence", "-foo.first_attribute.second_attribute", 24, "-(\n  foo\n  .first_attribute\n  .second_attribute\n)"},
		{"index expression", "foo[ 1 + i ].true", 80, "foo[1 + i].true"},
		{"index expression breaks safely", "foo[alpha + beta]", 14, "(\n  foo\n  [\n    alpha\n    + beta\n  ]\n)"},
		{"legacy numeric boundary", "foo.0 .0", 80, "foo.0 .0"},
		{"legacy exponent boundary", "foo.0e1 .0", 80, "foo.0e1 .0"},
		{"numeric root exponent attribute", "1 . e2", 80, "1 .e2"},
		{"numeric root ordinary attribute", "1.foo", 80, "1 .foo"},
		{"legacy splat scope", "foo.*.bar[0].baz", 80, "foo.*.bar[0].baz"},
		{"full splat scope", "foo[*].bar[0].baz", 80, "foo[*].bar[0].baz"},
		{"nested full splats", "foo[*][*].bar", 80, "foo[*][*].bar"},
		{"mixed splats", "foo.*.0[*].bar", 80, "foo.*.0[*].bar"},
		{"full then legacy splat", "foo[*].*.bar", 80, "foo[*].*.bar"},
		{"index between legacy splats", "foo.*.bar[0].*.baz", 80, "foo.*.bar[0].*.baz"},
		{"legacy splat numeric boundary", "foo.*.0 .0", 80, "foo.*.0 .0"},
		{"broken full splat", "foo[*].first_attribute[0].second_attribute", 24, "(\n  foo\n  [*]\n  .first_attribute\n  [0]\n  .second_attribute\n)"},
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
