// Package reference locates the Terraform or OpenTofu executable used by
// compatibility tests.
package reference

import (
	"os"
	"os/exec"
	"testing"
)

// Variable names the environment variable that selects the executable. CI sets
// it per compatibility job; an unset value means the tests are skipped.
const Variable = "TERRABLADE_REFERENCE_CLI"

// Enabled reports whether a reference executable was requested, for callers
// that must decide before they have a testing.TB to skip with.
func Enabled() bool { return os.Getenv(Variable) != "" }

// CLI returns the resolved path of the reference executable. An unset variable
// skips: the reference tools are optional for local development. A name that
// cannot be found fails instead, because a misspelled or missing executable
// must not quietly look like "compatibility testing was not requested".
func CLI(t testing.TB) string {
	t.Helper()
	name := os.Getenv(Variable)
	if name == "" {
		t.Skip("set " + Variable + " to terraform or tofu to run compatibility tests")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("find reference CLI %q: %v", name, err)
	}
	return path
}
