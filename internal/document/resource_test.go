package document_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/clipperhouse/displaywidth"

	"terrablade/internal/document"
)

func TestCompositionOwnershipAndConcurrentRendering(t *testing.T) {
	parts := []document.Doc{document.Text("alpha"), document.Line(), document.Text("beta")}
	doc := document.Group(document.Concat(parts...))
	clear(parts)
	copied := doc
	doc = document.Text("other")
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 20 {
				for _, width := range []int{10, 9} {
					want := "alpha beta"
					if width == 9 {
						want = "alpha\nbeta"
					}
					if got := document.Render(copied, document.Options{PrintWidth: width}); got != want {
						t.Errorf("Render = %q, want %q", got, want)
					}
				}
			}
		})
	}
	readers.Wait()
	if got := document.Render(doc, document.Options{}); got != "other" {
		t.Fatalf("replacement doc = %q", got)
	}
}

func TestDeepDocuments(t *testing.T) {
	const depth = 50000
	for _, shape := range []string{"left concat", "right concat", "groups", "indent and literal lines"} {
		t.Run(shape, func(t *testing.T) {
			doc := document.Text("x")
			want := "x"
			for range depth {
				switch shape {
				case "left concat":
					doc = document.Concat(doc, document.Text("x"))
				case "right concat":
					doc = document.Concat(document.Text("x"), doc)
				case "groups":
					doc = document.Group(doc)
				case "indent and literal lines":
					doc = document.Indent(document.Group(document.Concat(document.LiteralLine(), doc)))
				}
			}
			switch shape {
			case "left concat", "right concat":
				want = strings.Repeat("x", depth+1)
			case "indent and literal lines":
				want = strings.Repeat("\n", depth) + "x"
			}
			if got := document.Render(doc, document.Options{}); got != want {
				t.Fatalf("deep %s rendered %d bytes, want %d", shape, len(got), len(want))
			}
		})
	}
}

func TestSharedDocuments(t *testing.T) {
	shared := document.Group(document.Concat(document.Text("a"), document.Line(), document.Text("b")))
	doc := document.Concat(shared, document.Text("!"), document.HardLine(), document.Text("prefix "), shared)
	if got, want := document.Render(doc, document.Options{PrintWidth: 8}), "a b!\nprefix a\nb"; got != want {
		t.Fatalf("shared group rendered %q, want %q", got, want)
	}
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

func FuzzTextPartition(f *testing.F) {
	for _, text := range []string{"a", "e\u0301👩‍💻🇰🇷", "한\t글", "\u0600a", "a\uFE0F", "\t\t"} {
		f.Add(text, uint8(8))
	}
	f.Fuzz(func(t *testing.T, text string, width uint8) {
		// Converting to runes deliberately normalizes malformed UTF-8; Text's
		// invalid-input contract is tested separately. Keep the fuzz work bounded.
		if len(text) > 4096 {
			t.Skip()
		}
		runes := []rune(text)
		parts := make([]document.Doc, 0, len(runes))
		for i, r := range runes {
			if r == '\r' || r == '\n' {
				runes[i] = ' '
			}
			parts = append(parts, document.Text(string(runes[i])))
		}
		layout := func(content document.Doc) document.Doc {
			return document.Group(document.Concat(content, document.Line(), document.Text("x")))
		}
		options := document.Options{PrintWidth: int(width) + 1}
		want := document.Render(layout(document.Text(string(runes))), options)
		if got := document.Render(layout(document.Concat(parts...)), options); got != want {
			t.Fatalf("partitioned text rendered %q, single text rendered %q", got, want)
		}
	})
}

func BenchmarkRender(b *testing.B) {
	for _, shape := range []string{"flat", "broken", "unicode"} {
		b.Run(shape, func(b *testing.B) {
			text := "item"
			if shape == "unicode" {
				text = "한글👩‍💻"
			}
			parts := make([]document.Doc, 0, 200)
			for range 100 {
				parts = append(parts, document.Text(text), document.Line())
			}
			doc := document.Group(document.Indent(document.Concat(parts...)))
			options := document.Options{PrintWidth: 80}
			if shape == "flat" {
				options.PrintWidth = 1000
			}
			b.ReportAllocs()
			for b.Loop() {
				_ = document.Render(doc, options)
			}
		})
	}
}

func BenchmarkRenderSiblingGroups(b *testing.B) {
	for _, count := range []int{5000, 10000, 20000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			parts := make([]document.Doc, 0, count*2)
			item := document.Group(document.Concat(document.Text("a"), document.Line(), document.Text("b")))
			for range count {
				parts = append(parts, item, document.HardLine())
			}
			doc := document.Concat(parts...)
			b.ReportAllocs()
			for b.Loop() {
				_ = document.Render(doc, document.Options{})
			}
		})
	}
}
