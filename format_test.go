package terrablade_test

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gszzzzzz/terrablade"
)

// Keep callers on the supported interface: all tests in this package import
// only the root module, and these assignments pin the main callable contracts.
var (
	_ func([]byte, terrablade.Options) ([]byte, error) = terrablade.Format
	_ error                                            = (*terrablade.ParseError)(nil)
	_ error                                            = (*terrablade.OptionsError)(nil)
)

func TestFormat(t *testing.T) {
	for _, test := range []struct {
		name, source, want string
	}{
		{"empty", "", ""},
		{"whitespace", " \t\r\n\n", ""},
		{"BOM only", "\ufeff \n", ""},
		{"BOM and CRLF", "\ufeffa=1\r\nlong=2\r\n", "a    = 1\nlong = 2\n"},
		{"blank groups", "\na=1\nlong=2\n\n\nx=3\n\n", "a    = 1\nlong = 2\n\nx = 3\n"},
		{"blocks", "resource aws_instance web {ami=\"x\"}\nz=1", "resource \"aws_instance\" \"web\" {\n  ami = \"x\"\n}\n\nz = 1\n"},
		{"header comments", `block /*type*/ bare /*label*/ "q" {}`, "block \"bare\" \"q\" /*type*/ /*label*/ {}\n"},
		{"normalization", `a="${foo.0.bar}"`, "a = foo[0].bar\n"},
		{"computed key", `a={"${name}"=1}`, "a = { (name) = 1 }\n"},
		{"literal key", `a={"${"k"}"=1}`, "a = { \"k\" = 1 }\n"},
		{"precedence", `a=-"${x + y}"`, "a = -(x + y)\n"},
		{"full splat", `a=foo[*].0.bar`, "a = foo[*][0].bar\n"},
		{"attribute splat", `a=foo.*.0 .0`, "a = foo.*.0 .0\n"},
		{"EOF comment CR", "#\r", "#\r\r\n"},
		{"heredoc", "a=<<-E\n  hello\n  E\n", "a = <<-E\n  hello\n  E\n"},
		{"template", `a="prefix ${ foo.0 }"`, "a = \"prefix ${foo[0]}\"\n"},
		{"comprehension", `a=[for v in xs:v.id if v.enabled]`, "a = [for v in xs : v.id if v.enabled]\n"},
		{"object comprehension", `a={for k,v in xs:k=>v...}`, "a = { for k, v in xs : k => v... }\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := format(t, []byte(test.source), terrablade.Options{})
			if string(got) != test.want {
				t.Fatalf("Format() = %q, want %q", got, test.want)
			}
			if again := format(t, got, terrablade.Options{}); !bytes.Equal(again, got) {
				t.Fatalf("not idempotent: %q => %q", got, again)
			}
		})
	}
	if got := format(t, nil, terrablade.Options{}); len(got) != 0 {
		t.Fatalf("Format(nil) = %q", got)
	}
}

func TestOptions(t *testing.T) {
	const source = "b {\n a=[alpha,beta,gamma]\n}\n"
	defaultOutput := format(t, []byte(source), terrablade.Options{})
	if got := format(t, []byte(source), terrablade.Options{PrintWidth: 80, IndentWidth: 2, TabWidth: 8}); !bytes.Equal(got, defaultOutput) {
		t.Fatalf("explicit defaults changed output: %q => %q", defaultOutput, got)
	}
	if got := format(t, []byte(source), terrablade.Options{PrintWidth: 16, IndentWidth: 4}); string(got) != "b {\n    a = [\n        alpha,\n        beta,\n        gamma,\n    ]\n}\n" {
		t.Fatalf("width/indent options ignored: %q", got)
	}
	for _, test := range []struct {
		options terrablade.Options
		field   string
		value   int
	}{
		{terrablade.Options{PrintWidth: -1}, "PrintWidth", -1},
		{terrablade.Options{IndentWidth: -2}, "IndentWidth", -2},
		{terrablade.Options{TabWidth: -3}, "TabWidth", -3},
		{terrablade.Options{PrintWidth: -1, IndentWidth: -2, TabWidth: -3}, "PrintWidth", -1},
	} {
		for _, source := range [][]byte{nil, []byte("a=1"), []byte("invalid=")} {
			got, err := terrablade.Format(source, test.options)
			var optionError *terrablade.OptionsError
			if got != nil || !errors.As(err, &optionError) {
				t.Fatalf("Format invalid options = %q, %v", got, err)
			}
			if optionError.Option != test.field || optionError.Value != test.value || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("invalid option detail: %+v", optionError)
			}
		}
	}
}

func TestFormatOwnership(t *testing.T) {
	for _, text := range []string{"a=1", "a = 1\n", "# immutable comment\n"} {
		source := []byte(text)
		got := format(t, source, terrablade.Options{})
		if string(source) != text {
			t.Fatal("Format modified its input")
		}
		want := bytes.Clone(got)
		clear(source)
		if !bytes.Equal(got, want) {
			t.Fatal("output aliases input")
		}
		other := format(t, want, terrablade.Options{})
		clear(got)
		if !bytes.Equal(other, want) {
			t.Fatal("independent outputs alias one another")
		}
	}
	input := []byte("a=\xff")
	_, err := terrablade.Format(input, terrablade.Options{})
	var parseError *terrablade.ParseError
	if !errors.As(err, &parseError) {
		t.Fatal(err)
	}
	want := parseError.Diagnostics()
	clear(input)
	got := parseError.Diagnostics()
	got[0] = terrablade.Diagnostic{}
	if !reflect.DeepEqual(parseError.Diagnostics(), want) {
		t.Fatal("diagnostics alias source or caller's returned slice")
	}
	var zero terrablade.ParseError
	if zero.Diagnostics() != nil || zero.Error() == "" {
		t.Fatal("zero ParseError does not provide safe empty diagnostics")
	}
}

func TestTabWidth(t *testing.T) {
	source := []byte("a=f(\"\t\",beta)")
	for _, test := range []struct {
		width int
		want  string
	}{
		{0, "a = f(\"\t\", beta)\n"},
		{8, "a = f(\"\t\", beta)\n"},
		{16, "a = f(\n  \"\t\",\n  beta,\n)\n"},
	} {
		options := terrablade.Options{PrintWidth: 20, TabWidth: test.width}
		got := format(t, source, options)
		if string(got) != test.want {
			t.Fatalf("TabWidth %d: got %q, want %q", test.width, got, test.want)
		}
		if again := format(t, got, options); !bytes.Equal(again, got) {
			t.Fatalf("TabWidth %d is not idempotent: %q => %q", test.width, got, again)
		}
	}
}

func format(t testing.TB, source []byte, options terrablade.Options) []byte {
	t.Helper()
	output, err := terrablade.Format(source, options)
	if err != nil {
		t.Fatalf("Format(%q): %v", source, err)
	}
	return output
}
