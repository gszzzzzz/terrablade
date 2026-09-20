package document_test

import (
	"testing"

	"github.com/gszzzzzz/terrablade/internal/document"
)

func TestForceFlat(t *testing.T) {
	group := document.Group(document.Concat(document.Text("alpha"), document.Line(), document.Text("beta")))
	for _, test := range []struct {
		name string
		doc  document.Doc
		want string
	}{
		{"empty", document.ForceFlat(document.Doc{}), ""},
		{"nested group overflows", document.ForceFlat(group), "alpha beta"},
		{
			"lexical scope",
			document.Concat(document.ForceFlat(group), document.HardLine(), group),
			"alpha beta\nalpha\nbeta",
		},

		{
			"flat branches",
			document.ForceFlat(document.Concat(
				document.SoftLine(),
				document.IfBreak(document.Text("broken"), document.Text("flat")),
			)),
			"flat",
		},

		{
			"mandatory lines resume flat",
			document.ForceFlat(document.Indent(document.Concat(
				group, document.HardLine(),
				group, document.LiteralLine(),
				group,
			))),
			"alpha beta\n  alpha beta\nalpha beta",
		},
		{
			"mandatory lines force outer group",
			document.Group(document.Concat(
				document.Text("a"), document.Line(),
				document.ForceFlat(document.Concat(
					document.Text("b"), document.HardLine(), document.Text("c"),
				)),
			)),
			"a\nb\nc",
		},

		{
			"fit examines flat continuation",
			document.Concat(
				document.Group(document.Concat(
					document.Text("a"), document.SoftLine(), document.Text("b"),
				)),
				document.ForceFlat(document.Concat(document.SoftLine(), document.Text("long"))),
			),
			"a\nblong",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := document.Render(test.doc, document.Options{PrintWidth: 2}); got != test.want {
				t.Fatalf("rendered %q, want %q", got, test.want)
			}
		})
	}
}
