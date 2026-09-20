package terrablade_test

import (
	"bytes"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/gszzzzzz/terrablade"
)

func FuzzFormat(f *testing.F) {
	for _, source := range []string{
		"", "\ufeff \r\n", "a=1", "a=", "a=\xff", "#\r", "a=1 #x\r\r\nb=2",
		"resource x y {\n a=1\n longer=2\n\n tags={x=1,y=2}\n}",
		`block /*type*/ bare /*label*/ "q" {}`, `a="${foo.0.bar}"`,
		`a={"${"k"}"=1}`, `a=-"${x+y}"`, `a="${"${x}"}"`,
		`a=(foo[*].b)[0]`, `a=foo[*].b[0]`, `a=foo.*.0 .0`,
		`a="prefix ${foo # comment` + "\n.bar}\"",
		"a=<<E\nx\r\r\nE\r\r\n", "a=\"${<<E\nx\nE\n}\" # tail\n",
		"a=f(\"\t\",alpha+beta,ready?yes:no)", "a=[for x in xs:x.id if x.enabled]",
		"a={for k,v in xs:k=>v...}", "/* é👩‍💻\xff */ 이름=",
	} {
		f.Add([]byte(source), uint8(20), uint8(2), uint8(8))
	}
	f.Fuzz(func(t *testing.T, source []byte, width, indent, tab uint8) {
		if len(source) > 8192 {
			t.Skip()
		}
		options := terrablade.Options{PrintWidth: int(width), IndentWidth: int(indent % 17), TabWidth: int(tab % 17)}
		before := bytes.Clone(source)
		output, err := terrablade.Format(source, options)
		if !bytes.Equal(source, before) {
			t.Fatal("Format modified source")
		}
		if err != nil {
			var parseError *terrablade.ParseError
			if output != nil || !errors.As(err, &parseError) {
				t.Fatalf("invalid input returned %q and %v", output, err)
			}
			diagnostics := parseError.Diagnostics()
			if len(diagnostics) == 0 {
				t.Fatal("ParseError lacks diagnostics")
			}
			previous := 0
			for _, diagnostic := range diagnostics {
				start, end := diagnostic.Span.Start, diagnostic.Span.End
				if start.Offset < previous || start.Offset > end.Offset || end.Offset > len(source) || diagnostic.Kind == "" || diagnostic.Message == "" {
					t.Fatalf("invalid diagnostic: %+v", diagnostic)
				}
				for _, position := range []terrablade.Position{start, end} {
					if position.Line != bytes.Count(source[:position.Offset], []byte{'\n'})+1 || position.Column < 1 {
						t.Fatalf("invalid position: %+v", position)
					}
				}
				previous = start.Offset
			}
			_, repeated := terrablade.Format(source, options)
			var repeatedParseError *terrablade.ParseError
			if !errors.As(repeated, &repeatedParseError) || !reflect.DeepEqual(diagnostics, repeatedParseError.Diagnostics()) {
				t.Fatal("diagnostics changed between identical calls")
			}
			return
		}
		if !utf8.Valid(output) || len(output) > 0 && output[len(output)-1] != '\n' {
			t.Fatalf("invalid formatted file: %q", output)
		}
		if again := format(t, output, options); !bytes.Equal(again, output) {
			t.Fatalf("not idempotent with %+v: %q => %q", options, output, again)
		}
	})
}

func TestFormatDeepAndWide(t *testing.T) {
	for name, source := range map[string]string{
		"blocks":    strings.Repeat("b {\n", 256) + "a=1\n" + strings.Repeat("}\n", 256),
		"unary":     "a=" + strings.Repeat("!", 512) + "true",
		"binary":    "a=x" + strings.Repeat("+x", 20000),
		"traversal": "a=foo" + strings.Repeat(".attribute.0", 10000),
		"labels":    "b" + strings.Repeat(" /*header*/ label", 10000) + " {}",
		"wide":      wideFile(10000),
	} {
		t.Run(name, func(t *testing.T) {
			got := format(t, []byte(source), terrablade.Options{PrintWidth: 40})
			if again := format(t, got, terrablade.Options{PrintWidth: 40}); !bytes.Equal(again, got) {
				t.Fatalf("%s file is not idempotent", name)
			}
		})
	}
}

func TestManyDiagnostics(t *testing.T) {
	const count = 10000
	for _, source := range []string{strings.Repeat("a=\n", count), strings.Repeat("\xff", count)} {
		diagnostics := parseDiagnostics(t, []byte(source))
		if len(diagnostics) < count {
			t.Fatalf("only %d diagnostics for %d invalid entries", len(diagnostics), count)
		}
		last := diagnostics[len(diagnostics)-1].Span.End
		if last.Offset != len(source) {
			t.Fatalf("final diagnostic did not reach EOF: %+v", last)
		}
		if strings.Contains(source, "\n") {
			if last.Line != count+1 || last.Column != 1 {
				t.Fatalf("wrong final line: %+v", last)
			}
		} else if last.Line != 1 || last.Column != count+1 {
			t.Fatalf("wrong final column: %+v", last)
		}
	}
}

func TestFormatConcurrent(t *testing.T) {
	valid := []byte("b {\n a=\"${foo.0}\"\n values=[alpha,beta,gamma]\n}\n")
	invalid := []byte("/* é👩‍💻 */ a=\xff\nb=\n")
	options := terrablade.Options{PrintWidth: 20}
	want := format(t, valid, options)
	_, err := terrablade.Format(invalid, options)
	var shared *terrablade.ParseError
	if !errors.As(err, &shared) {
		t.Fatal(err)
	}
	diagnostics := shared.Diagnostics()
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 20 {
				got, err := terrablade.Format(valid, options)
				if err != nil || !bytes.Equal(got, want) {
					t.Errorf("concurrent output changed: %q, %v", got, err)
				}
				output, err := terrablade.Format(invalid, options)
				var parseError *terrablade.ParseError
				if output != nil || !errors.As(err, &parseError) || !reflect.DeepEqual(parseError.Diagnostics(), diagnostics) {
					t.Errorf("concurrent diagnostics changed: %v", err)
				}
				copy := shared.Diagnostics()
				if !reflect.DeepEqual(copy, diagnostics) || shared.Error() == "" {
					t.Error("shared ParseError changed")
				}
				clear(copy)
			}
		})
	}
	workers.Wait()
}

func BenchmarkFormat(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000} {
		for name, source := range map[string]string{
			"valid":       wideFile(count),
			"diagnostics": strings.Repeat("a=\n", count),
		} {
			b.Run(name+"/"+strconv.Itoa(count), func(b *testing.B) {
				input := []byte(source)
				b.ReportAllocs()
				b.SetBytes(int64(len(input)))
				for b.Loop() {
					output, err := terrablade.Format(input, terrablade.Options{})
					if name == "valid" && (err != nil || len(output) == 0) || name == "diagnostics" && err == nil {
						b.Fatalf("unexpected format result: %v", err)
					}
				}
			})
		}
	}
}

func wideFile(count int) string {
	var source strings.Builder
	for i := range count {
		source.WriteString("a")
		source.WriteString(strconv.Itoa(i))
		source.WriteString("=foo.0 # value\n")
	}
	return source.String()
}
