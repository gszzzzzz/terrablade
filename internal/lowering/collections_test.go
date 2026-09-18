package lowering_test

import "testing"

func TestObjectLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"empty", "{}", 1, "{}"},
		{"source vertical empty", "{\n\n}", 80, "{\n}"},
		{"source vertical empty comment", "{\n/*empty*/}", 80, "{\n  /*empty*/\n}"},
		{"source vertical single entry", "{\nfoo=1}", 80, "{\n  foo = 1,\n}"},
		{"source vertical CRLF", "{\r\nfoo=1,bar=2\r\n}", 80, "{\n  foo = 1,\n  bar = 2,\n}"},
		{"source vertical blank lines", "{\n\nfoo=1\n\n\nbar=2}", 80, "{\n  foo = 1,\n\n  bar = 2,\n}"},
		{"source vertical comments", "{\n# first\nfoo=1 # trailing\n}", 80, "{\n  # first\n  foo = 1, # trailing\n}"},
		{"source vertical after opening comment", "{/*first*/\nfoo=1}", 80, "{\n  /*first*/\n  foo = 1,\n}"},
		{"tuple newline still flattens", "[\n1,2\n]", 80, "[1, 2]"},
		{"flat", "{foo:1,bar=2,}", 80, "{ foo = 1, bar = 2 }"},
		{"flat entries do not align", "{a=1,longer=2}", 80, "{ a = 1, longer = 2 }"},
		{"vertical entries align", "{\na=1\nlonger=2\n}", 80, "{\n  a      = 1,\n  longer = 2,\n}"},
		{"flat objects on separate rows do not align", "[{a=1}, {longer=2}]", 18, "[\n  { a = 1 },\n  { longer = 2 },\n]"},
		{"nested object columns", "{\na={\nx=1\nlong=2\n}\nb=3\n}", 80, "{\n  a = {\n    x    = 1,\n    long = 2,\n  },\n  b = 3,\n}"},
		{"entry comments align", "{\na=1 # first\nlonger=222 # second\n}", 80, "{\n  a      = 1,   # first\n  longer = 222, # second\n}"},
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

func TestForLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"tuple flat", "[for x in xs:x.id]", 80, "[for x in xs : x.id]"},
		{"object flat", "{for k,v in xs:k=>v... if v}", 80, "{ for k, v in xs : k => v... if v }"},
		{"object for opening newline reflows", "{\nfor k,v in xs:k=>v... if v\n}", 80, "{ for k, v in xs : k => v... if v }"},
		{"tuple broken", "[for value in values : value.id if value.enabled]", 30, "[\n  for value in values :\n  value.id\n  if value.enabled\n]"},
		{"object broken", "{for k,v in values:k=>v... if v.enabled}", 28, "{\n  for k, v in values :\n  k => v...\n  if v.enabled\n}"},
		{"contextual bindings", "[for in in if:if if in]", 80, "[for in in if : if if in]"},
		{"source lines reflow", "[\nfor x\nin xs\n:\nx\nif x\n]", 80, "[for x in xs : x if x]"},
		{"conditional collection", "[for x in ready ? first : second : x]", 24, "[\n  for x in ready\n  ? first\n  : second :\n  x\n]"},
		{"projection operator", "[for x in xs:alpha + beta]", 18, "[\n  for x in xs :\n  alpha + beta\n]"},
		{"projection arrow breaks", "{for x in xs:long_key=>long_value}", 18, "{\n  for x in xs :\n  long_key\n  => long_value\n}"},
		{"projection line comment", "[for x in xs: # projection\nx]", 80, "[\n  for x in xs : # projection\n  x\n]"},
		{"header comments", "[for /*binding*/ x in /*collection*/ xs:x]", 80, "[for /*binding*/ x in /*collection*/ xs : x]"},
		{"binding comma precedes comment", "[for k # binding\n,v in xs:k]", 80, "[\n  for k, # binding\n  v in xs :\n  k\n]"},
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
