//go:build darwin || linux

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWritePreservesMetadata(t *testing.T) {
	for _, mode := range []os.FileMode{0600, 0640, 0755} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			path := putFile(t, dir, "main.tf", "a=1")
			if err := os.Chmod(path, mode); err != nil {
				t.Fatal(err)
			}
			before := statFile(t, path)
			assertRun(t, []string{"--write", path}, "", 0, path+"\n", "")
			after := statFile(t, path)
			oldStat := before.Sys().(*syscall.Stat_t)
			newStat := after.Sys().(*syscall.Stat_t)
			if before.Mode() != after.Mode() || oldStat.Uid != newStat.Uid || oldStat.Gid != newStat.Gid {
				t.Fatalf("metadata changed: mode %s -> %s, uid/gid %d/%d -> %d/%d",
					before.Mode(), after.Mode(), oldStat.Uid, oldStat.Gid, newStat.Uid, newStat.Gid)
			}
			if !os.SameFile(before, after) {
				t.Fatal("changed file inode was replaced")
			}
			assertContents(t, path, "a = 1\n")
		})
	}
}

func TestRunUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read permission-denied fixtures")
	}
	dir := t.TempDir()
	path := putFile(t, dir, "main.tf", "a=1")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	var stdout, stderr bytes.Buffer
	status := run([]string{path}, forbiddenReader{t}, &stdout, &stderr)
	if status != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), ": open: permission denied\n") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestWritePreservesDifferentGroup(t *testing.T) {
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := putFile(t, dir, "main.tf", "a=1")
	originalGroup := statFile(t, path).Sys().(*syscall.Stat_t).Gid
	for _, group := range groups {
		if uint32(group) == originalGroup {
			continue
		}
		if err := os.Chown(path, -1, group); err != nil {
			continue
		}
		assertRun(t, []string{"--write", path}, "", 0, path+"\n", "")
		if got := statFile(t, path).Sys().(*syscall.Stat_t).Gid; got != uint32(group) {
			t.Fatalf("group changed from %d to %d", group, got)
		}
		assertContents(t, path, "a = 1\n")
		return
	}
	t.Skip("no other permitted group available for ownership fixture")
}

func TestWriteUnwritableDirectory(t *testing.T) {
	dir := t.TempDir()
	path := putFile(t, dir, "main.tf", "a=1")
	canonical := putFile(t, dir, "fixed.tf", "b = 2\n")
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })
	// Updating an existing writable file needs no writable parent directory.
	assertRun(t, []string{"--write", path}, "", 0, path+"\n", "")
	assertContents(t, path, "a = 1\n")
	assertRun(t, []string{"--write", canonical}, "", 0, "", "")
}

func TestWriteReadOnlyFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write permission-denied fixtures")
	}
	dir := t.TempDir()
	changed := putFile(t, dir, "changed.tf", "a=1")
	canonical := putFile(t, dir, "fixed.tf", "b = 2\n")
	invalid := putFile(t, dir, "invalid.tf", "c=")
	for _, path := range []string{changed, canonical, invalid} {
		if err := os.Chmod(path, 0400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(path, 0600) })
	}
	assertRun(t, []string{"--write", changed}, "", 2, "", "terrablade: "+changed+": open: permission denied\n")
	assertContents(t, changed, "a=1")
	// These must finish before a writable open, even when writing would fail.
	assertRun(t, []string{"--write", canonical}, "", 0, "", "")
	assertRun(t, []string{"--write", invalid}, "", 2, "", invalid+":1:3: ExpectedExpression: Expected an expression.\n")
	assertContents(t, invalid, "c=")
}

func TestRunFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.tf")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	// --write rejects the pipe before opening it, so no writer is needed.
	assertRun(t, []string{"--write", path}, "", 2, "", "terrablade: "+path+": --write requires a regular file\n")

	written := make(chan error, 1)
	go func() {
		written <- os.WriteFile(path, []byte("a=1"), 0)
	}()
	assertRun(t, []string{path}, "", 0, "a = 1\n", "")
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestControlCharacterFilenames(t *testing.T) {
	dir := t.TempDir()
	path := putFile(t, dir, "a\n\x1b.tf", "a=1")
	assertRun(t, []string{"--check", path}, "", 1, pathLabel(path)+"\n", "")
	assertRun(t, []string{"--write", path}, "", 0, pathLabel(path)+"\n", "")
	assertContents(t, path, "a = 1\n")
	invalid := putFile(t, dir, "b\t.tf", "a=")
	assertRun(t, []string{invalid}, "", 2, "", pathLabel(invalid)+":1:3: ExpectedExpression: Expected an expression.\n")
}

type brokenPipeWriter struct{}

func (brokenPipeWriter) Write([]byte) (int, error) {
	return 0, &os.PathError{Op: "write", Path: "/dev/stdout", Err: syscall.EPIPE}
}

func TestBrokenPipeIsQuiet(t *testing.T) {
	dir := t.TempDir()
	first := putFile(t, dir, "first.tf", "a=1")
	second := putFile(t, dir, "second.tf", "b=2")
	for _, args := range [][]string{{first}, {"--help"}, {"--write", first, second}} {
		var stderr bytes.Buffer
		status := run(args, forbiddenReader{t}, brokenPipeWriter{}, &stderr)
		if status != 2 || stderr.Len() != 0 {
			t.Errorf("%q: status=%d stderr=%q", args, status, stderr.String())
		}
	}
	// A closed pipe still stops the run before later files change.
	assertContents(t, second, "b=2")
}
