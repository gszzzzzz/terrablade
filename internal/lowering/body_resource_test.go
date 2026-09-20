package lowering_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// The seed corpus stays in escaped form. A seed is opaque input for the
// fuzzer rather than a layout expectation, and the dense list is easier to
// scan for coverage gaps than one call per line would be.
func FuzzFile(f *testing.F) {
	for _, source := range []string{
		"", "\ufeff\n", "a=1", "a=1\nlong=2", "a=1\n\nlong=2",
		"a=1 # first\nlong=222 # second", "a=1\n# group\nb=2",
		"a=1\n/*a*/ /*b*/ # c\n\nlong=2", "a=1\n\n/*prefix*/ b=2",
		"/*a*/ x=1\nlong=2", "a /*key*/=/*value*/ 1 /*tail*/\n",
		"b {}", "b bare \"quoted\" {}", "b { a=1 }", "b { # open\n a=1 # value\n}",
		"b { /*open*/ a=1 }", "b { /*first*/ /*second*/ a=1 }", "b { /*first\nsecond*/ a=1 }",
		"b /*type*/ label /*brace*/ {}", "b /*type*/ {}",
		"b /*same*/ first /*same*/ second /*same*/ {}",
		"# lead\nb /*first*/ bare /*second\nline*/ label /*brace*/ { # open\n # body\n} // end\n",
		"b {\n a=1\n inner { x=2 }\n b=3\n}\nx=4",
		"a=1\n\n# block\nb {}\n# next\nz=3",
		"a=<<E\nx\nE\n", "a=1\nlong=<<E\nx\nE\nz=2",
		"b {\n a=<<-E\n  x\n  E\n}\n", "a=<<E\nx\r\r\nE\r\r\n",
		"a={\nx=1\nlong=2\n}\nz=3", "a={x={\nx=1\nlong=2\n},z=3}",
		"a=[{x=1},{long=2}]", "a={\nx=1 # first\nlong=22 # second\n}",
		"a=f(alpha+beta, ready?yes:no)", "a=foo[alpha+beta].first_attribute.second_attribute",
		"a=f(foo # base\n.bar)", "a=foo.0 .0", "a=foo.0 .e2", "a=aws_instance.foo.0.id",
		`a="prefix ${~{x=1}~}"`, `a="prefix %{if {x=1}}yes%{endif}"`,
		"a=[for x in xs:x.id if x.enabled]", "a={for x in xs:long_key=>long_value}",
		"이름=1\né=2\nx=3", "b {\n /* first\r\n  second */\n a=1\n}",
		`a="${foo.0.bar}"`, `a={"${a}"="${b}"}`, `a="${"${x}"}"`,
		"b {\n a=\"${foo # c\n.bar}\"\n b=\"${# c\nx # d\n}\"\n}\n",
		`a="${x[*].a}".0`, `a=foo.*.0`, `a=foo[*].0`,
		"a=\"${<<E\nx\nE\n}\" # tail\n", "b {\n a=\"${<<E\nx\nE\n}\" /*tail*/\n}\n",
		"#x\r\r\n", "a=1 #x\r\r\nb=2", `a={"${"k"}"=1}`,
	} {
		f.Add(source, uint8(20))
	}
	f.Fuzz(func(t *testing.T, source string, width uint8) {
		if len(source) > 8192 {
			t.Skip()
		}
		result := syntax.Parse([]byte(source))
		if len(result.Diagnostics()) != 0 {
			t.Skip()
		}
		doc, err := lowering.File(result)
		if err != nil {
			t.Fatal(err)
		}
		options := document.Options{PrintWidth: int(width) + 1}
		output := document.Render(doc, options)
		assertFileContent(t, source, output)
		if again := renderFile(t, output, options.PrintWidth); again != output {
			t.Fatalf("not idempotent: %q => %q", output, again)
		}
	})
}

func TestDeepAndWideBodies(t *testing.T) {
	// Construction must not recurse with block nesting. Rendering a deeply
	// indented body has intrinsically quadratic output bytes, so test rendering
	// separately at a depth that keeps that unavoidable output affordable.
	deep := strings.Repeat("b {\n", 20000) + "a=x\n" + strings.Repeat("}\n", 20000)
	result := syntax.Parse([]byte(deep))
	if len(result.Diagnostics()) != 0 {
		t.Fatal(result.Diagnostics())
	}
	if _, err := lowering.File(result); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{
		strings.Repeat("b {\n", 512) + "a=x\n" + strings.Repeat("}\n", 512),
		wideBody(10000),
		"b" + strings.Repeat(" label", 20000) + " {}\n",
		"b" + strings.Repeat(" /*header*/ label", 20000) + " {}\n",
	} {
		output := renderFile(t, source, 30)
		assertFileContent(t, source, output)
		if again := renderFile(t, output, 30); again != output {
			t.Fatal("large body is not idempotent")
		}
	}
}

func TestConcurrentFileLowering(t *testing.T) {
	result := syntax.Parse([]byte("b {\n a=1\n longer={\nx=1\nlong=2\n}\n}\n"))
	doc, err := lowering.File(result)
	if err != nil {
		t.Fatal(err)
	}
	want := document.Render(doc, document.Options{})
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			other, err := lowering.File(result)
			if err != nil {
				t.Error(err)
				return
			}
			if document.Render(other, document.Options{}) != want || document.Render(doc, document.Options{}) != want {
				t.Error("concurrent file layout changed")
			}
		})
	}
	workers.Wait()
}

func BenchmarkFileLowering(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000} {
		for name, source := range map[string]string{
			"wide":   wideBody(count),
			"deep":   strings.Repeat("b {\n", count) + "a=x\n" + strings.Repeat("}\n", count),
			"header": "b" + strings.Repeat(" /*header*/ label", count) + " {}\n",
		} {
			b.Run(name+"/"+strconv.Itoa(count), func(b *testing.B) {
				result := syntax.Parse([]byte(source))
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := lowering.File(result); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
