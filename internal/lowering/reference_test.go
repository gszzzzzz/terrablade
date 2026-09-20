package lowering_test

import (
	"os"
	"os/exec"
	"testing"
)

const referenceCLIEnv = "TERRABLADE_REFERENCE_CLI"

func referenceCLIEnabled() bool {
	return os.Getenv(referenceCLIEnv) != ""
}

func referenceCLI(t testing.TB) string {
	t.Helper()
	name := os.Getenv(referenceCLIEnv)
	if name == "" {
		t.Skip("set " + referenceCLIEnv + " to terraform or tofu to run compatibility tests")
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("find reference CLI %q: %v", name, err)
	}
	return path
}
