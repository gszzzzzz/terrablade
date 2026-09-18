package lowering_test

import (
	"reflect"
	"testing"
)

func TestFileLineCommentCarriageReturns(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"EOF", "#\r", "#\r\r\n"},
		{"EOF CR run", "//x\r\r", "//x\r\r\r\n"},
		{"CRLF", "#x\r\n", "#x\n"},
		{"literal CR before CRLF", "#x\r\r\n", "#x\r\r\n"},
		{"literal CR run before CRLF", "#x\r\r\r\n", "#x\r\r\r\n"},
		{"body middle", "a=1\n#x\r\r\nb=2", "a = 1\n#x\r\r\nb = 2\n"},
		{"trailing EOF", "a=1 #x\r", "a = 1 #x\r\r\n"},
		{"trailing body comment", "a=1 #x\r\r\nb=2", "a = 1 #x\r\r\nb = 2\n"},
		{"nested body", "b { #x\r\r\na=1\n}", "b { #x\r\r\n  a = 1\n}\n"},
		{"consecutive comments", "#x\r\r\n//y\r\r\r\n", "#x\r\r\n//y\r\r\r\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := renderFile(t, test.source, 80)
			if output != test.want {
				t.Fatalf("output = %q, want %q", output, test.want)
			}
			assertFileContent(t, test.source, output)
			if next := renderFile(t, output, 80); next != output {
				t.Fatalf("not idempotent: %q => %q", output, next)
			}
		})
	}
}

func TestExpressionLineCommentCarriageReturns(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"call", "f(a, #x\r\r\nb)", "f(\n  a, #x\r\r\n  b,\n)"},
		{"CRLF", "f(a, #x\r\nb)", "f(\n  a, #x\n  b,\n)"},
		{"CR run", "f(a, //x\r\r\r\nb)", "f(\n  a, //x\r\r\r\n  b,\n)"},
		{"wrapper edge", "\"${a #x\r\r\n}\"", "(a #x\r\r\n)"},
		{"wrapper traversal", "\"${a #x\r\r\n.b}\"", "(\n  a #x\r\r\n  .b\n)"},
		{"template interpolation", "\"prefix ${a #x\r\r\n}\"", "\"prefix ${a #x\r\r\n}\""},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := render(t, test.source, 80)
			if output != test.want {
				t.Fatalf("output = %q, want %q", output, test.want)
			}
			before, original := parse(t, test.source)
			after, normalized := parse(t, output)
			if !reflect.DeepEqual(expressionTokens(before, original), expressionTokens(after, normalized)) {
				t.Fatalf("comment content changed: %q => %q", test.source, output)
			}
			if next := render(t, output, 80); next != output {
				t.Fatalf("not idempotent: %q => %q", output, next)
			}
		})
	}
}
