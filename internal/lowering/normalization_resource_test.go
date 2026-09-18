package lowering_test

import (
	"strconv"
	"strings"
	"testing"

	"terrablade/internal/document"
	"terrablade/internal/lowering"
)

func TestDeepAndWideNormalization(t *testing.T) {
	for name, source := range map[string]string{
		"wrappers":           strings.Repeat(`"${`, 512) + "a" + strings.Repeat(`}"`, 512),
		"commented wrappers": strings.Repeat(`"${/*lead*/`, 256) + "a" + strings.Repeat(`/*tail*/}"`, 256),
		"indices":            "foo" + strings.Repeat(".0 ", 20000),
		"wide":               "[" + strings.Repeat(`"${foo.0}",`, 10000) + "]",
		"operation":          strings.Repeat(`"${a}" + `, 20000) + `"${a}"`,
		"comments":           `"${` + strings.Repeat("/*lead*/ ", 10000) + "a" + strings.Repeat(" /*tail*/", 10000) + `}"`,
	} {
		t.Run(name, func(t *testing.T) {
			output := render(t, source, 40)
			if next := render(t, output, 40); next != output {
				t.Fatal("normalization is not idempotent")
			}
			assertFileContent(t, "value = "+source+"\n", "value = "+output+"\n")
		})
	}
}

func BenchmarkNormalization(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000} {
		for name, source := range map[string]string{
			"indices":   "foo" + strings.Repeat(".0 ", count),
			"wide":      "[" + strings.Repeat(`"${foo.0}",`, count) + "]",
			"operation": strings.Repeat(`"${a}" + `, count) + `"${a}"`,
		} {
			b.Run(name+"/"+strconv.Itoa(count), func(b *testing.B) {
				result, root := parse(b, source)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					doc, err := lowering.Expression(result, root)
					if err != nil {
						b.Fatal(err)
					}
					document.Render(doc, document.Options{PrintWidth: 40})
				}
			})
		}
	}
}
