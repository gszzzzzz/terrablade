package main

import (
	"bytes"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestRenameFailureKeepsOriginalAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := putFile(t, dir, "main.tf", "a=1")
	// Darwin's user immutable flag (UF_IMMUTABLE in sys/stat.h) allows reading
	// this file and writing a sibling, but refuses replacement at Rename itself.
	// This exercises a real final-commit failure without a mock filesystem.
	const userImmutable = 0x00000002
	if err := syscall.Chflags(path, userImmutable); err != nil {
		t.Skipf("filesystem does not support immutable files: %v", err)
	}
	t.Cleanup(func() {
		if err := syscall.Chflags(path, 0); err != nil {
			t.Errorf("clear immutable fixture flag: %v", err)
		}
	})
	before := statFile(t, path)
	var stdout, stderr bytes.Buffer
	status := run([]string{"--write", path}, forbiddenReader{t}, &stdout, &stderr)
	if status != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), ": rename: ") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	assertContents(t, path, "a=1")
	if after := statFile(t, path); !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("failed replacement modified original metadata")
	}
	assertNoTemps(t, dir)
}
