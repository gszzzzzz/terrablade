package document_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/clipperhouse/displaywidth"

	"github.com/gszzzzzz/terrablade/internal/document"
)

func TestRenderLayouts(t *testing.T) {
	list := document.Group(document.Concat(
		document.Text("["),
		document.Indent(document.Concat(document.SoftLine(), document.Text("alpha,"), document.Line(), document.Text("beta"))),
		document.IfBreak(document.Text(","), document.Doc{}),
		document.SoftLine(), document.Text("]"),
	))
	nested := document.Group(document.Concat(
		document.Text("prefix"), document.Line(),
		document.Group(document.Concat(
			document.Text("a"), document.Line(), document.Text("b"),
			document.IfBreak(document.Text("!"), document.Text("?")),
		)),
	))
	pair := document.Group(document.Concat(document.Text("a"), document.Line(), document.Text("b")))
	hardParent := document.Group(document.Concat(
		document.Text("a"), document.Line(),
		document.Group(document.Concat(document.Text("b"), document.HardLine(), document.Text("c"))),
	))
	literalIndent := document.Indent(document.Concat(
		document.Text("<<E"), document.LiteralLine(), document.Text("  raw"),
		document.LiteralLine(), document.Text("E"), document.HardLine(), document.Text("next"),
	))
	for _, test := range []struct {
		name  string
		doc   document.Doc
		width int
		want  string
	}{
		{"zero", document.Doc{}, 1, ""},
		{"empty constructors", document.Concat(
			document.Text(""), document.Group(document.Doc{}), document.Indent(document.Doc{}),
			document.IfBreak(document.Doc{}, document.Doc{}),
		), 1, ""},
		{"text exceeds width", document.Text("long"), 1, "long"},
		{"flat list exact fit", list, 13, "[alpha, beta]"},
		{"broken list", list, 12, "[\n  alpha,\n  beta,\n]"},
		{"outside group", document.Concat(
			document.Text("a"), document.Line(), document.SoftLine(),
			document.IfBreak(document.Text("b"), document.Text("f")),
		), 80, "a\n\nb"},
		{"suffix affects fit", document.Concat(pair, document.Text("!")), 3, "a\nb!"},
		{"next line excluded", document.Concat(pair, document.HardLine(), document.Text("long")), 3, "a b\nlong"},
		{"hardline forces ancestors", hardParent, 80, "a\nb\nc"},
		{"broken branch does not force group", document.Group(document.Concat(
			document.Text("a"), document.IfBreak(document.HardLine(), document.Text("b")),
		)), 80, "ab"},
		{"flat branch hardline prevents flat", document.Group(document.IfBreak(document.Text("broken"), document.HardLine())), 80, "broken"},
		{"nested group can stay flat", nested, 8, "prefix\na b?"},
		{"inner ifbreak selects nearest group", nested, 2, "prefix\na\nb!"},
		{"indent does not pad initial text", document.Indent(document.Concat(document.Text("a"), document.HardLine(), document.Text("b"))), 80, "a\n  b"},
		{"empty lines have no padding", document.Indent(document.Concat(
			document.HardLine(), document.HardLine(), document.Text("x"), document.HardLine(),
		)), 80, "\n\n  x\n"},
		{"explicit whitespace is preserved", document.Concat(
			document.Text("a  "), document.HardLine(), document.Text(" \t"), document.LiteralLine(),
		), 80, "a  \n \t\n"},
		{"literal line retains indent context", literalIndent, 80, "<<E\n  raw\nE\n  next"},
		{"literal line forces group", document.Group(document.Concat(
			document.Text("a"), document.Line(), document.Text("b"), document.LiteralLine(), document.Text("c"),
		)), 80, "a\nb\nc"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := document.Render(test.doc, document.Options{PrintWidth: test.width}); got != test.want {
				t.Fatalf("Render = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDisplayWidth(t *testing.T) {
	for _, test := range []struct {
		name, text string
		width      int
	}{
		{"ASCII", "abc", 3},
		{"CJK", "한글", 4},
		{"combining", "e\u0301", 1},
		{"emoji ZWJ", "👩‍💻", 2},
		{"flag", "🇰🇷", 2},
		{"emoji modifier", "👍🏽", 2},
		{"emoji presentation", "❤️", 2},
		{"ambiguous narrow", "·Ω", 2},
		{"unicode 17 emoji", "\U0001FAEA", 2},
		{"tab", "a\tb", 9},
		{"lone CR", "a\rb", 2},
		{"lone CR ends grapheme", "e\r\u0301", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			// The same visible string must wrap identically whether lowering
			// emits one text node or splits it at every rune boundary.
			parts := []document.Doc{}
			for _, r := range test.text {
				parts = append(parts, document.Text(string(r)))
			}
			for _, text := range []document.Doc{document.Text(test.text), document.Concat(parts...)} {
				doc := document.Group(document.Concat(text, document.Line(), document.Text("x")))
				for _, width := range []int{test.width + 1, test.width + 2} {
					separator := "\n"
					if width == test.width+2 {
						separator = " "
					}
					if got, want := document.Render(doc, document.Options{PrintWidth: width}), test.text+separator+"x"; got != want {
						t.Errorf("width %d: %q, want %q", width, got, want)
					}
				}
			}
		})
	}
}

func TestAdjacentGroupsBreakIndependently(t *testing.T) {
	call := document.Group(document.Concat(
		document.Text("fn("),
		document.Indent(document.Concat(
			document.SoftLine(), document.Text("a,"), document.Line(), document.Text("b"),
		)),
		document.SoftLine(), document.Text(")"),
	))
	traversal := document.Group(document.Indent(document.Concat(
		document.SoftLine(), document.Text(".attribute"),
	)))
	doc := document.Concat(call, traversal)
	for _, test := range []struct {
		width int
		want  string
	}{
		{18, "fn(a, b).attribute"},
		{12, "fn(a, b)\n  .attribute"},
		{7, "fn(\n  a,\n  b\n)\n  .attribute"},
	} {
		if got := document.Render(doc, document.Options{PrintWidth: test.width}); got != test.want {
			t.Errorf("width %d: %q, want %q", test.width, got, test.want)
		}
	}
}

func TestOptionsAndIndentationFit(t *testing.T) {
	doc := document.Indent(document.Concat(
		document.HardLine(),
		document.Group(document.Concat(document.Text("a\tb"), document.Line(), document.Text("c"))),
	))
	for _, test := range []struct {
		options document.Options
		want    string
	}{
		{document.Options{}, "\n  a\tb c"},
		{document.Options{PrintWidth: 8, IndentWidth: 3, TabWidth: 4}, "\n   a\tb\n   c"},
		{document.Options{PrintWidth: 8, IndentWidth: 1, TabWidth: 4}, "\n a\tb c"},
	} {
		if got := document.Render(doc, test.options); got != test.want {
			t.Errorf("options %+v: %q, want %q", test.options, got, test.want)
		}
	}
}

func TestInvalidInputs(t *testing.T) {
	for _, text := range []string{"a\nb", "\r\n", "\xff", "\xe2\x82"} {
		t.Run(fmt.Sprintf("Text %q", text), func(t *testing.T) {
			mustPanic(t, func() { document.Text(text) })
		})
	}
	for _, options := range []document.Options{{PrintWidth: -1}, {IndentWidth: -1}, {TabWidth: -1}} {
		mustPanic(t, func() { document.Render(document.Doc{}, options) })
	}
	maxInt := int(^uint(0) >> 1)
	mustPanic(t, func() {
		document.Render(document.Indent(document.Indent(document.Text("x"))), document.Options{IndentWidth: maxInt})
	})
	mustPanic(t, func() { document.Render(document.Text("\t\t"), document.Options{TabWidth: maxInt}) })
}

func mustPanic(t *testing.T, action func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected panic")
		}
	}()
	action()
}

func TestDisplayWidthIgnoresEnvironmentAndDependencyDefaults(t *testing.T) {
	t.Setenv("RUNEWIDTH_EASTASIAN", "1")
	t.Setenv("LC_ALL", "ko_KR.UTF-8")
	original := displaywidth.DefaultOptions
	displaywidth.DefaultOptions = displaywidth.Options{EastAsianWidth: true, ControlSequences: true}
	t.Cleanup(func() { displaywidth.DefaultOptions = original })
	doc := document.Group(document.Concat(document.Text("Ω"), document.Line(), document.Text("x")))
	if got := document.Render(doc, document.Options{PrintWidth: 3}); got != "Ω x" {
		t.Fatalf("environment or dependency defaults changed layout: %q", got)
	}
}

func TestGraphemeAcrossGroupEdges(t *testing.T) {
	for _, doc := range []document.Doc{
		document.Concat(document.Text("👩"), document.Group(document.Concat(
			document.Text("\u200d💻"), document.Line(), document.Text("x"),
		))),
		document.Concat(document.Group(document.Concat(
			document.Text("x"), document.Line(), document.Text("👩"),
		)), document.Text("\u200d💻")),
	} {
		if got := document.Render(doc, document.Options{PrintWidth: 4}); strings.Contains(got, "\n") {
			t.Fatalf("cluster crossing group edge was over-counted: %q", got)
		}
		if got := document.Render(doc, document.Options{PrintWidth: 3}); !strings.Contains(got, "\n") {
			t.Fatalf("cluster crossing group edge was under-counted: %q", got)
		}
	}
}

func TestGraphemeBoundarySegments(t *testing.T) {
	for _, text := range []string{
		"🇰🇷🇦🇧🇨x", "👩‍👩‍👧‍👦x", "क्\u200dकx", "\u0600\u0600a b", "e\u0301\u0302x",
	} {
		for offset := range text {
			for width := 1; width <= 12; width++ {
				layout := func(content document.Doc) document.Doc {
					return document.Group(document.Concat(content, document.Line(), document.Text("end")))
				}
				options := document.Options{PrintWidth: width}
				whole := layout(document.Text(text))
				split := layout(document.Concat(document.Text(text[:offset]), document.Text(text[offset:])))
				if got, want := document.Render(split, options), document.Render(whole, options); got != want {
					t.Errorf("%q split at %d width %d: %q, want %q", text, offset, width, got, want)
				}
			}
		}
	}
}

func TestLiteralLineWithStructuredInterpolation(t *testing.T) {
	// Literal newlines preserve heredoc-owned leading spaces. A formatted
	// interpolation can still use the surrounding structural indentation.
	doc := document.Concat(
		document.Text("block {"),
		document.Indent(document.Concat(
			document.HardLine(), document.Text("value = <<E"),
			document.LiteralLine(), document.Text("  raw  "),
			document.LiteralLine(), document.Text("${"),
			document.Indent(document.Concat(document.HardLine(), document.Text("value"))),
			document.HardLine(), document.Text("}"),
			document.LiteralLine(), document.Text("E"),
			document.HardLine(), document.Text("next = 1"),
		)),
		document.HardLine(), document.Text("}"), document.HardLine(),
	)
	want := "block {\n  value = <<E\n  raw  \n${\n    value\n  }\nE\n  next = 1\n}\n"
	if got := document.Render(doc, document.Options{}); got != want {
		t.Fatalf("heredoc composition = %q, want %q", got, want)
	}
}

func ExampleRender() {
	doc := document.Group(document.Concat(
		document.Text("["),
		document.Indent(document.Concat(document.SoftLine(), document.Text("one,"), document.Line(), document.Text("two"))),
		document.IfBreak(document.Text(","), document.Doc{}),
		document.SoftLine(), document.Text("]"),
	))
	fmt.Println(document.Render(doc, document.Options{PrintWidth: 8}))
	// Output:
	// [
	//   one,
	//   two,
	// ]
}
