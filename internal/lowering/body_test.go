package lowering_test

import (
	"testing"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/reference"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

func TestFileLayouts(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		// File boundaries. BOM, tab, and CR cases stay escaped: their exact
		// bytes are what the policy is about.
		{"empty", "", "\n"},
		{"outer whitespace", " \n\t\r\n", "\n"},
		{"BOM only", "\ufeff\n\n", "\n"},
		{"BOM and attribute", "\ufeffa=1", "a = 1\n"},
		{"final newline", "a=1", "a = 1\n"},
		{"outer padding", lines("", "", "a=1", "", "", ""), "a = 1\n"},

		// Item boundaries.
		{"attribute blank gap caps", lines("a=1", "", "", "b=2", ""), lines("a = 1", "", "b = 2", "")},

		// Blocks and labels.
		{"empty block", lines("empty { ", "", " }"), "empty {}\n"},
		{"short block expands", "short { a=1 }", lines("short {", "  a = 1", "}", "")},
		{
			name:   "block boundaries",
			source: lines("a=1", "b {}", "c=2", "d {}", "e {}"),
			want:   lines("a = 1", "", "b {}", "", "c = 2", "", "d {}", "", "e {}", ""),
		},
		{
			name:   "nested block boundaries",
			source: lines("outer {", " a=1", " inner { x=2 }", " b=3", "}"),
			want:   lines("outer {", "  a = 1", "", "  inner {", "    x = 2", "  }", "", "  b = 3", "}", ""),
		},
		{"labels", `resource aws_instance "web\u0020server" {}`, "resource \"aws_instance\" \"web\\u0020server\" {}\n"},

		// Header comments move to the brace in source order.
		{
			name:   "header comments",
			source: `block /*type*/ bare /*label*/ "quoted" /*brace*/ {}`,
			want:   "block \"bare\" \"quoted\" /*type*/ /*label*/ /*brace*/ {}\n",
		},
		{"unlabeled header comment", `block /*type*/ {}`, "block /*type*/ {}\n"},
		{
			name:   "multiline label trivia relocated",
			source: lines("block /*type", ` end*/ "a" {}`),
			want:   lines(`block "a" /*type`, " end*/ {}", ""),
		},
		{
			name:   "multiline brace trivia retained",
			source: lines(`block "a" /*brace`, " end*/ {}"),
			want:   lines(`block "a" /*brace`, " end*/ {}", ""),
		},
		{
			name:   "repeated header comments",
			source: `block /*same*/ first /*same*/ second /*same*/ {}`,
			want:   "block \"first\" \"second\" /*same*/ /*same*/ /*same*/ {}\n",
		},
		{
			name: "header and surrounding comments",
			source: lines(
				"# lead", "block /*first*/ bare /*second",
				`line*/ "quoted" /*brace*/ { # open`, " # body", "} // end", "",
			),
			want: lines(
				"# lead", `block "bare" "quoted" /*first*/ /*second`,
				"line*/ /*brace*/ { # open", "  # body", "} // end", "",
			),
		},
		{
			name:   "nested header comments",
			source: lines("outer {", " inner /*type*/ bare /*label*/ { a=1 }", "}"),
			want:   lines("outer {", `  inner "bare" /*type*/ /*label*/ {`, "    a = 1", "  }", "}", ""),
		},

		// Comment placement and sections inside a body.
		{"attribute comments", "a /*key*/=/*value*/ 1 /*tail*/ # end\n", "a /*key*/ = /*value*/ 1 /*tail*/ # end\n"},
		{
			name:   "leading and trailing comments",
			source: lines("", "", "# lead", "a=1", "# end", "", ""),
			want:   lines("# lead", "a = 1", "# end", ""),
		},
		{"only comments", lines("", "# one", "", "", "# two", "", ""), lines("# one", "", "# two", "")},
		{
			name:   "comment sections",
			source: lines("a=1", "", "", "# section", "", "", "b=2", ""),
			want:   lines("a = 1", "", "# section", "", "b = 2", ""),
		},
		{
			name:   "same-line comment run section",
			source: lines("a=1", "/*first*/ /*second*/ # third", "", "", "b=2"),
			want:   lines("a = 1", "/*first*/ /*second*/ # third", "", "b = 2", ""),
		},
		{
			name:   "same-line block comments section",
			source: lines("a=1", "/*first*/ /*second*/", "", "", "b=2"),
			want:   lines("a = 1", "/*first*/ /*second*/", "", "b = 2", ""),
		},
		{
			name:   "comment prefix follows attribute group boundary",
			source: lines("a=1", "", "/*prefix*/ b=2"),
			want:   lines("a = 1", "", "/*prefix*/ b = 2", ""),
		},
		{
			name:   "inline comment preserves attribute group boundary",
			source: lines("a=1 # tail", "", "", "b=2"),
			want:   lines("a = 1 # tail", "", "b = 2", ""),
		},

		// Comments around the mandatory blank line at a block.
		{"comment before block", lines("a=1", "# block", "b {}"), lines("a = 1", "", "# block", "b {}", "")},
		{"comment after block", lines("a {}", "# next", "b=1"), lines("a {}", "", "# next", "b = 1", "")},
		{"inline comment before block boundary", lines("a=1 /*tail*/", "b {}"), lines("a = 1 /*tail*/", "", "b {}", "")},
		{
			name:   "same-line comment run after block boundary",
			source: lines("a {}", "/*first*/ /*second*/", "b=1"),
			want:   lines("a {}", "", "/*first*/ /*second*/", "b = 1", ""),
		},
		{"comment prefix after block boundary", lines("a=1", "/*prefix*/ b {}"), lines("a = 1", "", "/*prefix*/ b {}", "")},

		// Comments on a nested body's opening brace line.
		{"opener comment", lines("b { # open", " a=1", "}"), lines("b { # open", "  a = 1", "}", "")},
		{"opener block comment before attribute", "b { /*open*/ a=1 }", lines("b { /*open*/", "  a = 1", "}", "")},
		{
			name:   "opener comment run before attribute",
			source: "b { /*first*/ /*second*/ a=1 }",
			want:   lines("b { /*first*/ /*second*/", "  a = 1", "}", ""),
		},
		{
			name:   "opener multiline comment before attribute",
			source: lines("b { /*first", "second*/ a=1 }"),
			want:   lines("b { /*first", "second*/", "  a = 1", "}", ""),
		},
		{
			name:   "body comment prefix stays adjacent",
			source: lines("b {", " /*prefix*/ a=1", "}"),
			want:   lines("b {", "  /*prefix*/ a = 1", "}", ""),
		},
		{"comment only block", lines("b {", " # body", "", "}"), lines("b {", "  # body", "}", "")},
		{"inline block comment only", "b { /*body*/ }", lines("b { /*body*/", "}", "")},
		{"standalone inline block comment", "/*lead*/ a=1\n", "/*lead*/ a = 1\n"},
		{"closing standalone comment", "b { a=1 /*tail*/ }", lines("b {", "  a = 1 /*tail*/", "}", "")},

		// Heredocs: the following separator owns the marker's newline.
		{"heredoc final newline", lines("a=<<E", "x", "E", ""), lines("a = <<E", "x", "E", "")},
		{"heredoc sibling", lines("a=<<E", "x", "E", "b=2", ""), lines("a = <<E", "x", "E", "b = 2", "")},
		{"heredoc nested", lines("b {", " a=<<-E", "  x", "  E", "}", ""), lines("b {", "  a = <<-E", "  x", "  E", "}", "")},
		{"heredoc comment", lines("a=<<E", "x", "E", "# next", "b=2"), lines("a = <<E", "x", "E", "# next", "b = 2", "")},
		{"heredoc block gap", lines("a=<<E", "x", "E", "b {}"), lines("a = <<E", "x", "E", "", "b {}", "")},

		// Escaped below: these cases are about the exact bytes, not layout.
		{"CRLF", "b {\r\n a=1\r\n}\r\n", lines("b {", "  a = 1", "}", "")},
		{
			name:   "multiline comment literal",
			source: "b {\n /* first\r\n second */\n a=1\n}",
			want:   lines("b {", "  /* first", " second */", "  a = 1", "}", ""),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := renderFile(t, test.source, 80)
			if got != test.want {
				t.Fatalf("rendered %q, want %q", got, test.want)
			}
			if again := renderFile(t, got, 80); again != got {
				t.Fatalf("not idempotent: %q => %q", got, again)
			}
			assertFileContent(t, test.source, got)
		})
	}
}

func TestFileRejectsInvalidInput(t *testing.T) {
	for _, result := range []syntax.Result{syntax.Result{}, syntax.Parse([]byte("a=")), syntax.Parse([]byte(lines("good=1", "bad=")))} {
		doc, err := lowering.File(result)
		if err == nil || document.Render(doc, document.Options{}) != "" {
			t.Fatalf("expected error and empty document, got %v", err)
		}
	}
}

func TestFileRejectsLineCommentsInsideBlockHeader(t *testing.T) {
	// An ordinary newline ends the header, including one following a line
	// comment. File must not move comments to repair already-invalid syntax.
	for _, source := range []string{
		lines("block # type", " label {}"), lines("block label // label", " {}"),
		lines("block /*first*/ label # last", " {}"),
	} {
		result := syntax.Parse([]byte(source))
		if len(result.Diagnostics()) == 0 {
			t.Fatalf("expected invalid block header: %q", source)
		}
		doc, err := lowering.File(result)
		if err == nil || document.Render(doc, document.Options{}) != "" {
			t.Fatalf("expected error and empty document for %q, got %v", source, err)
		}
	}
}

func TestBodyAlignment(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
		width              int
	}{

		// Attribute rows share one assignment column.
		{"attributes", lines("a=1", "long=2", "z=3"), lines("a    = 1", "long = 2", "z    = 3", ""), 80},
		{"blank line splits groups", lines("a=1", "", "long=2"), lines("a = 1", "", "long = 2", ""), 80},
		{
			name:   "resource attribute groups",
			source: lines("resource x y {", " a=1", " bb=2", "", "", " longer=3", " c=4", "}"),
			want:   lines(`resource "x" "y" {`, "  a  = 1", "  bb = 2", "", "  longer = 3", "  c      = 4", "}", ""),
			width:  80,
		},
		{
			name:   "blank line splits comment columns",
			source: lines("a=1 # first", "b=222 # second", "", "long=3 # third", "x=4 # fourth"),
			want:   lines("a = 1   # first", "b = 222 # second", "", "long = 3 # third", "x    = 4 # fourth", ""),
			width:  80,
		},

		// Structural values and comments split alignment groups.
		{
			name:   "heredoc separates attribute groups",
			source: lines("a=1", "long=<<E", "x", "E", "", "", "b=2", "cc=3"),
			want:   lines("a    = 1", "long = <<E", "x", "E", "", "b  = 2", "cc = 3", ""),
			width:  80,
		},
		{
			name:   "comment section between attribute groups",
			source: lines("a=1", "long=2", "", "# group", "", "b=3", "cc=4"),
			want:   lines("a    = 1", "long = 2", "", "# group", "", "b  = 3", "cc = 4", ""),
			width:  80,
		},
		{
			name:   "standalone comment splits groups",
			source: lines("a=1", "# note", "long=2", "z=3"),
			want:   lines("a = 1", "# note", "long = 2", "z    = 3", ""),
			width:  80,
		},
		{
			name:   "inline comments",
			source: lines("a=1 # first", "long=222 # second", "z=3"),
			want:   lines("a    = 1   # first", "long = 222 # second", "z    = 3", ""),
			width:  80,
		},
		{"inline prefix", lines("/* lead */ a=1", "long=2"), lines("/* lead */ a = 1", "long         = 2", ""), 80},
		{"name comment", lines("a /* name */=1", "long=2"), lines("a /* name */ = 1", "long         = 2", ""), 80},
		{"unicode grapheme columns", lines("한글=1", "aaa=2", "é=3"), lines("한글  = 1", "aaa = 2", "é   = 3", ""), 80},
		{
			name:   "heredoc stays in group",
			source: lines("a=1", "long=<<E", "x", "E", "z=3"),
			want:   lines("a    = 1", "long = <<E", "x", "E", "z    = 3", ""),
			width:  80,
		},

		// Width-driven breaks decide group membership after layout.
		{
			name:   "wrapped tuple splits groups",
			source: lines("a=1", "long=[alpha,beta]", "z=2"),
			want:   lines("a = 1", "long = [", "  alpha,", "  beta,", "]", "z = 2", ""),
			width:  16,
		},
		{
			name:   "flat tuple joins groups",
			source: lines("a=1", "long=[alpha,beta]", "z=2"),
			want:   lines("a    = 1", "long = [alpha, beta]", "z    = 2", ""),
			width:  80,
		},
		{
			name:   "operator parentheses split groups",
			source: lines("a=1", "long=alpha+beta", "z=2"),
			want:   lines("a = 1", "long = (", "  alpha", "  + beta", ")", "z = 2", ""),
			width:  16,
		},
		{
			name:   "nested scope",
			source: lines("b {", " a=1", " longer=2", "}", "x=3"),
			want:   lines("b {", "  a      = 1", "  longer = 2", "}", "", "x = 3", ""),
			width:  80,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := renderFile(t, test.source, test.width)
			if got != test.want {
				t.Fatalf("rendered %q, want %q", got, test.want)
			}
			if again := renderFile(t, got, test.width); again != got {
				t.Fatalf("not idempotent: %q => %q", got, again)
			}
			assertFileContent(t, test.source, got)
		})
	}
}

func TestBodyReferenceCompatibility(t *testing.T) {
	reference.CLI(t)
	for _, source := range []string{
		lines("a=1", "long=2", "z=3"),
		lines("a=1 # first", "long=222 # second", "z=3"),
		lines("a=1 // first", "long=222 // second", "z=3"),
		lines("a=1", "# note", "long=2", "z=3"),
		lines("a /* name */=1", "long=2"),
		lines("/* lead */ a=1", "long=2"),
		lines("a /* multi", "line */=1", "long_name=2"),
		lines("한글=1", "aaa=2", "é=3"),
		lines("a=1", "long=<<E", "x", "E", "z=3"),
		lines("a=1", "long=[alpha,beta]", "z=2"),
		lines("a=1", "long=alpha+beta", "z=2"),
		lines("outer label {", " a=1", " inner { z=3 }", " longer=2", "}", "x=3"),
		lines("a=1", "# next", "b {}", "", "", "# attributes", "x=3", "yyyy=4"),
		lines("b { # open", " a=1 # value", "}", ""),
		lines("b {", " a=<<-E", "  x", "  E", "}", ""),
		"b { /*body*/ }\n",
		"b { /*open*/ a=1 }",
		"b { /*first*/ /*second*/ a=1 }",
		lines("b { /*first", "second*/ a=1 }"),
		lines("b {", " /*prefix*/ a=1", "}"),
		"\ufeffa=1\nlong=2\n",
		`block /*type*/ bare /*between*/ "quoted" /*brace*/ {}`,
		`block /*type*/ {}`,
		lines("block /*type", ` end*/ "a" {}`),
		`block /*same*/ first /*same*/ second /*same*/ {}`,
		lines("# lead", "block /*first*/ bare /*second", `line*/ "quoted" /*brace*/ { # open`, " # body", "} // end", ""),
		lines("outer {", " inner /*type*/ bare /*label*/ { a=1 }", "}"),
		lines("a=1", "/*first*/ /*second*/ # third", "", "b=2"),
		lines("value={", "a=1 # first", "longer=222 # second", "}", ""),
		lines("a=1", "value={ a=1, longer=2 }", "z=3"),
		lines("a=1", "value={", "x=1", "longer=2", "}", "z=3"),
		"value=[{a=1},{longer=2}]",
		lines("resource x y {", " a=1", " bb=2", "", "", " longer=3", " c=4", "}"),
		lines("a=1 # first", "b=222 # second", "", "long=3 # third", "x=4 # fourth"),
		lines("a=1", "long=<<E", "x", "E", "", "", "b=2", "cc=3"),
		lines("a=1", "long=2", "", "# group", "", "b=3", "cc=4"),
	} {
		for _, width := range []int{16, 80} {
			output := renderFile(t, source, width)
			assertReferenceFormat(t, output)
		}
	}
}
