package lowering_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
)

func TestDeepOperationChains(t *testing.T) {
	const count = 20000
	for _, source := range []string{
		strings.Repeat("a + ", count) + "a",
		"root" + strings.Repeat(".attribute", count),
		"f(" + strings.Repeat("a + ", count) + "a)",
		"f(root" + strings.Repeat(".attribute[0]", count) + ")",
	} {
		result, node := parse(t, source)
		output := render(t, source, 30)
		reparsed, next := parse(t, output)
		if !reflect.DeepEqual(expressionTokens(result, node), expressionTokens(reparsed, next)) {
			t.Fatal("deep operation changed expression structure")
		}
		if again := render(t, output, 30); again != output {
			t.Fatal("deep operation is not idempotent")
		}
	}
}

func BenchmarkOperationChains(b *testing.B) {
	for _, count := range []int{1000, 5000, 10000} {
		for name, source := range map[string]string{
			"binary":    strings.Repeat("a + ", count) + "a",
			"traversal": "root" + strings.Repeat(".attribute", count),
		} {
			b.Run(name+"/"+strconv.Itoa(count), func(b *testing.B) {
				result, node := parse(b, source)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					doc, err := lowering.Expression(result, node)
					if err != nil {
						b.Fatal(err)
					}
					document.Render(doc, document.Options{PrintWidth: 30})
				}
			})
		}
	}
}
