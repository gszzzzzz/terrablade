package lowering_test

import (
	"testing"

	"github.com/gszzzzzz/terrablade/internal/reference"
)

// assertReferenceFormat fails unless the reference CLI leaves output exactly
// as it is. This package's output is canonical only if the tool it has to
// coexist with agrees, so the oracle is "fmt changes nothing", not "fmt
// produces something similar".
func assertReferenceFormat(t *testing.T, output string) {
	t.Helper()
	formatted, err := reference.Format(t, []byte(output))
	if err != nil {
		t.Fatalf("reference CLI failed: %v", err)
	}
	if string(formatted) != output {
		t.Errorf("reference CLI changed canonical formatting:\n%q\n=>\n%q", output, formatted)
	}
}
