//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWriteFailureRestoresOriginal(t *testing.T) {
	const helperPath, helperLimit = "TERRABLADE_TEST_LIMITED_WRITE_PATH", "TERRABLADE_TEST_LIMITED_WRITE_BYTES"
	if path := os.Getenv(helperPath); path != "" {
		// Limit only this subprocess. A real short file write exercises the
		// failure path without adding a filesystem abstraction to production.
		signal.Ignore(syscall.SIGXFSZ)
		bytesLimit, err := strconv.ParseUint(os.Getenv(helperLimit), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
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
		limit.Cur = bytesLimit
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		status := run([]string{"--write", path}, forbiddenReader{t}, &stdout, &stderr)
		if status != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), ": write: ") {
			t.Fatalf("write failure: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
		}
		// The parent process asserts on this line.
		os.Stdout.WriteString("STDERR:" + stderr.String())
		return
	}

	for _, test := range []struct {
		name     string
		limit    int
		contents string
		message  string
	}{
		// "a=1" fits under the limit but "a = 1\n" does not.
		{"restored", 4, "a=1", "; original content restored"},
		// Neither fits, so the partial content remains and both errors show.
		{"restore fails", 1, "a", "write: file too large; restore original content: write: "},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := putFile(t, t.TempDir(), "main.tf", "a=1")
			before := statFile(t, path)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "-test.run=^TestWriteFailureRestoresOriginal$")
			command.Env = append(os.Environ(), helperPath+"="+path, helperLimit+"="+strconv.Itoa(test.limit))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("limited-write subprocess: %v\n%s", err, output)
			}
			if !strings.Contains(string(output), test.message) {
				t.Fatalf("subprocess output lacks %q:\n%s", test.message, output)
			}
			assertContents(t, path, test.contents)
			if !os.SameFile(before, statFile(t, path)) {
				t.Fatal("failed write replaced the inode")
			}
		})
	}
}
