package lowering_test

import "testing"

func TestObjectLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"empty", "{}", 1, "{}"},
		{"flat", "{foo:1,bar=2,}", 80, "{ foo = 1, bar = 2 }"},
		{"newline separators", "{foo=1\nbar=2}", 80, "{ foo = 1, bar = 2 }"},
		{"broken", "{foo=1,bar=2}", 14, "{\n  foo = 1,\n  bar = 2,\n}"},
		{"blank line", "{foo=1\n\n\nbar=2}", 14, "{\n  foo = 1,\n\n  bar = 2,\n}"},
		{"inline comment and implicit comma", "{foo=1 # note\nbar=2}", 80, "{\n  foo = 1, # note\n  bar = 2,\n}"},
		{"comments around colon", "{foo/*key*/:/*value*/1}", 80, "{ foo /*key*/ = /*value*/ 1 }"},
		{"computed key remains parenthesized", "{(foo)=1}", 80, "{ (foo) = 1 }"},
		{"arbitrary key expression", "{foo + bar=1}", 80, "{ foo + bar = 1 }"},
		{"key expression breaks safely", "{alpha + beta = 1}", 14, "{\n  (\n    alpha\n    + beta\n  ) = 1,\n}"},
		{"value expression breaks safely", "{key = alpha + beta}", 14, "{\n  key = (\n    alpha\n    + beta\n  ),\n}"},
		{"object inside safe tuple still protects value", "[{key = alpha + beta}]", 14, "[\n  {\n    key = (\n      alpha\n      + beta\n    ),\n  },\n]"},
		{"contextual literal key", "{forx=1, true=false}", 80, "{ forx = 1, true = false }"},
		{"empty comment", "{/*empty*/}", 80, "{/*empty*/}"},
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
