package document_test

import (
	"testing"

	"terrablade/internal/document"
)

func TestAlignedCells(t *testing.T) {
	row := func(name string, value document.Doc, comment string) document.Doc {
		var suffix document.Doc
		if comment != "" {
			suffix = document.Cell(1, document.Text(" # "+comment))
		}
		return document.Concat(document.Text(name), document.Cell(0, document.Concat(document.Text(" = "), value)), suffix)
	}
	for _, test := range []struct {
		name  string
		doc   document.Doc
		width int
		want  string
	}{
		{"empty", document.Cell(0, document.Doc{}), 80, ""},
		{"cell at output start", document.Concat(row("", document.Text("1"), ""), document.HardLine(), row("long", document.Text("2"), "")), 80, "     = 1\nlong = 2"},
		{"cell before pending indentation", document.Indent(document.Concat(document.HardLine(), row("", document.Text("1"), ""), document.HardLine(), row("long", document.Text("2"), ""))), 80, "\n       = 1\n  long = 2"},
		{"two columns", document.Concat(row("a", document.Text("1"), "a"), document.HardLine(), row("long", document.Text("222"), "b")), 80, "a    = 1   # a\nlong = 222 # b"},
		{"first cell on row wins", document.Concat(row("a", row("inner", document.Text("1"), ""), ""), document.HardLine(), row("long", document.Text("2"), "")), 80, "a    = inner = 1\nlong = 2"},
		{"multiline outer cell exposes nested rows", row("outer", document.Concat(document.Text("{"), document.HardLine(), row("a", document.Text("1"), ""), document.HardLine(), row("long", document.Text("2"), ""), document.HardLine(), document.Text("}")), ""), 80, "outer = {\na    = 1\nlong = 2\n}"},
		{"grapheme columns", document.Concat(row("한글", document.Text("1"), ""), document.HardLine(), row("abc", document.Text("2"), ""), document.HardLine(), row("e\u0301", document.Text("3"), "")), 80, "한글  = 1\nabc = 2\né   = 3"},
		{"missing row breaks chain", document.Concat(row("a", document.Text("1"), ""), document.HardLine(), document.Text("# standalone"), document.HardLine(), row("long", document.Text("2"), "")), 80, "a = 1\n# standalone\nlong = 2"},
		{"structural lines break chain", document.Concat(row("a", document.Text("1"), ""), document.HardLine(), row("long", document.Group(document.Concat(document.Text("["), document.SoftLine(), document.Text("value"), document.SoftLine(), document.Text("]"))), ""), document.HardLine(), row("z", document.Text("2"), "")), 10, "a = 1\nlong = [\nvalue\n]\nz = 2"},
		{"literal lines remain opaque", document.Concat(row("a", document.Text("1"), ""), document.HardLine(), row("long", document.Concat(document.Text("<<E"), document.LiteralLine(), document.Text("x"), document.LiteralLine(), document.Text("E")), ""), document.HardLine(), row("z", document.Text("2"), "")), 80, "a    = 1\nlong = <<E\nx\nE\nz    = 2"},
		{"padding follows layout", document.Concat(row("long", document.Text("1"), ""), document.HardLine(), row("a", document.Group(document.Concat(document.Text("b"), document.Line(), document.Text("c"))), "")), 7, "long = 1\na    = b c"},
		{"cell inside group", document.Group(document.Concat(document.Text("a"), document.Cell(0, document.Text(" = 1")), document.Line(), document.Text("b"))), 5, "a = 1\nb"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := document.Render(test.doc, document.Options{PrintWidth: test.width}); got != test.want {
				t.Fatalf("rendered %q, want %q", got, test.want)
			}
		})
	}
}
