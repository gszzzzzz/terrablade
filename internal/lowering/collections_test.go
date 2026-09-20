package lowering_test

import "testing"

func TestObjectLayouts(t *testing.T) {
	for _, test := range []struct {
		name, source string
		width        int
		want         string
	}{
		{"empty", "{}", 1, "{}"},
		{"source vertical empty", lines("{", "", "}"), 80, lines("{", "}")},
		{"source vertical empty comment", lines("{", "/*empty*/}"), 80, lines("{", "  /*empty*/", "}")},
		{"source vertical single entry", lines("{", "foo=1}"), 80, lines("{", "  foo = 1,", "}")},

		// Escaped: the CRLF bytes are what this case checks.
		{"source vertical CRLF", "{\r\nfoo=1,bar=2\r\n}", 80, lines("{", "  foo = 1,", "  bar = 2,", "}")},
		{
			name:   "source vertical blank lines",
			source: lines("{", "", "foo=1", "", "", "bar=2}"),
			width:  80,
			want:   lines("{", "  foo = 1,", "", "  bar = 2,", "}"),
		},
		{
			name:   "source vertical comments",
			source: lines("{", "# first", "foo=1 # trailing", "}"),
			width:  80,
			want:   lines("{", "  # first", "  foo = 1, # trailing", "}"),
		},
		{
			name:   "source vertical after opening comment",
			source: lines("{/*first*/", "foo=1}"),
			width:  80,
			want:   lines("{", "  /*first*/", "  foo = 1,", "}"),
		},
		{"tuple newline still flattens", lines("[", "1,2", "]"), 80, "[1, 2]"},

		// Flat and broken entry layout, and the shared assignment column.
		{"flat", "{foo:1,bar=2,}", 80, "{ foo = 1, bar = 2 }"},
		{"flat entries do not align", "{a=1,longer=2}", 80, "{ a = 1, longer = 2 }"},
		{"vertical entries align", lines("{", "a=1", "longer=2", "}"), 80, lines("{", "  a      = 1,", "  longer = 2,", "}")},
		{
			name:   "flat objects on separate rows do not align",
			source: "[{a=1}, {longer=2}]",
			width:  18,
			want:   lines("[", "  { a = 1 },", "  { longer = 2 },", "]"),
		},
		{
			name:   "nested object columns",
			source: lines("{", "a={", "x=1", "long=2", "}", "b=3", "}"),
			width:  80,
			want:   lines("{", "  a = {", "    x    = 1,", "    long = 2,", "  },", "  b = 3,", "}"),
		},
		{
			name:   "entry comments align",
			source: lines("{", "a=1 # first", "longer=222 # second", "}"),
			width:  80,
			want:   lines("{", "  a      = 1,   # first", "  longer = 222, # second", "}"),
		},
		{"newline separators", lines("{foo=1", "bar=2}"), 80, "{ foo = 1, bar = 2 }"},
		{"broken", "{foo=1,bar=2}", 14, lines("{", "  foo = 1,", "  bar = 2,", "}")},
		{"blank line", lines("{foo=1", "", "", "bar=2}"), 14, lines("{", "  foo = 1,", "", "  bar = 2,", "}")},
		{
			name:   "inline comment and implicit comma",
			source: lines("{foo=1 # note", "bar=2}"),
			width:  80,
			want:   lines("{", "  foo = 1, # note", "  bar = 2,", "}"),
		},

		// Keys: separators, computed keys, and contextual identifiers.
		{"comments around colon", "{foo/*key*/:/*value*/1}", 80, "{ foo /*key*/ = /*value*/ 1 }"},
		{"computed key remains parenthesized", "{(foo)=1}", 80, "{ (foo) = 1 }"},
		{"arbitrary key expression", "{foo + bar=1}", 80, "{ foo + bar = 1 }"},
		{
			name:   "key expression breaks safely",
			source: "{alpha + beta = 1}",
			width:  14,
			want:   lines("{", "  (", "    alpha", "    + beta", "  ) = 1,", "}"),
		},
		{
			name:   "value expression breaks safely",
			source: "{key = alpha + beta}",
			width:  14,
			want:   lines("{", "  key = (", "    alpha", "    + beta", "  ),", "}"),
		},
		{
			name:   "object inside safe tuple still protects value",
			source: "[{key = alpha + beta}]",
			width:  14,
			want:   lines("[", "  {", "    key = (", "      alpha", "      + beta", "    ),", "  },", "]"),
		},
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
		{
			name:   "object for opening newline reflows",
			source: lines("{", "for k,v in xs:k=>v... if v", "}"),
			width:  80,
			want:   "{ for k, v in xs : k => v... if v }",
		},

		// Broken clauses each start their own continuation line.
		{
			name:   "tuple broken",
			source: "[for value in values : value.id if value.enabled]",
			width:  30,
			want:   lines("[", "  for value in values :", "  value.id", "  if value.enabled", "]"),
		},
		{
			name:   "object broken",
			source: "{for k,v in values:k=>v... if v.enabled}",
			width:  28,
			want:   lines("{", "  for k, v in values :", "  k => v...", "  if v.enabled", "}"),
		},
		{"contextual bindings", "[for in in if:if if in]", 80, "[for in in if : if if in]"},
		{"source lines reflow", lines("[", "for x", "in xs", ":", "x", "if x", "]"), 80, "[for x in xs : x if x]"},
		{
			name:   "conditional collection",
			source: "[for x in ready ? first : second : x]",
			width:  24,
			want:   lines("[", "  for x in ready", "  ? first", "  : second :", "  x", "]"),
		},
		{"projection operator", "[for x in xs:alpha + beta]", 18, lines("[", "  for x in xs :", "  alpha + beta", "]")},
		{
			name:   "projection arrow breaks",
			source: "{for x in xs:long_key=>long_value}",
			width:  18,
			want:   lines("{", "  for x in xs :", "  long_key", "  => long_value", "}"),
		},
		{
			name:   "projection line comment",
			source: lines("[for x in xs: # projection", "x]"),
			width:  80,
			want:   lines("[", "  for x in xs : # projection", "  x", "]"),
		},

		// Clause comments.
		{"header comments", "[for /*binding*/ x in /*collection*/ xs:x]", 80, "[for /*binding*/ x in /*collection*/ xs : x]"},
		{
			name:   "binding comma precedes comment",
			source: lines("[for k # binding", ",v in xs:k]"),
			width:  80,
			want:   lines("[", "  for k, # binding", "  v in xs :", "  k", "]"),
		},
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
