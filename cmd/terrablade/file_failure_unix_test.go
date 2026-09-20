//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWriteFailureCanLeavePartialContent(t *testing.T) {
	const helperPath = "TERRABLADE_TEST_LIMITED_WRITE_PATH"
	if path := os.Getenv(helperPath); path != "" {
		// Limit only this subprocess. A real short file write exercises the
		// failure path without adding a filesystem abstraction to production.
		signal.Ignore(syscall.SIGXFSZ)
		var limit syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
			t.Fatal(err)
		}
		originalLimit := limit
		defer func() {
			// The test binary may write a coverage report after this returns.
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &originalLimit); err != nil {
				t.Errorf("restore file-size limit: %v", err)
			}
		}()
		limit.Cur = 1
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		status := run([]string{"--write", path}, forbiddenReader{t}, &stdout, &stderr)
		if status != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), ": write: ") {
			t.Fatalf("write failure: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
		}
		return
	}

	path := putFile(t, t.TempDir(), "main.tf", "a=1")
	before := statFile(t, path)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestWriteFailureCanLeavePartialContent$")
	command.Env = append(os.Environ(), helperPath+"="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("limited-write subprocess: %v\n%s", err, output)
	}
	assertContents(t, path, "a")
	if !os.SameFile(before, statFile(t, path)) {
		t.Fatal("failed write replaced the inode")
	}
}
