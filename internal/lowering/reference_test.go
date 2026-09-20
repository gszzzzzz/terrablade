package lowering_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gszzzzzz/terrablade/internal/reference"
)

// assertReferenceFormat fails unless the reference CLI leaves output exactly
// as it is. This package's output is canonical only if the tool it has to
// coexist with agrees, so the oracle is "fmt changes nothing", not "fmt
// produces something similar".
func assertReferenceFormat(t *testing.T, output string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, reference.CLI(t), "fmt", "-no-color", "-")
	command.Stdin = strings.NewReader(output)
	formatted, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("reference CLI failed: %v\n%s", err, formatted)
	}
	if string(formatted) != output {
		t.Errorf("reference CLI changed canonical formatting:\n%q\n=>\n%q", output, formatted)
	}
}
