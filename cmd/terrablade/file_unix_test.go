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
	for _, mode := range []os.FileMode{0400, 0600, 0640, 0755, 0700 | os.ModeSetuid, 0750 | os.ModeSetgid} {
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
			if os.SameFile(before, after) {
				t.Fatal("changed file was modified in place instead of replaced")
			}
			assertContents(t, path, "a = 1\n")
			assertNoTemps(t, dir)
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
	if os.Geteuid() == 0 {
		t.Skip("root can write permission-denied fixtures")
	}
	dir := t.TempDir()
	path := putFile(t, dir, "main.tf", "a=1")
	canonical := putFile(t, dir, "fixed.tf", "b = 2\n")
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0700) })
	var stdout, stderr bytes.Buffer
	status := run([]string{"--write", path}, forbiddenReader{t}, &stdout, &stderr)
	if status != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "permission denied") {
		t.Fatalf("status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	assertContents(t, path, "a=1")
	assertNoTemps(t, dir)
	// No-op writes must not require write permission in the parent directory.
	assertRun(t, []string{"--write", canonical}, "", 0, "", "")
}

func TestRunRejectsFIFOWithoutOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pipe.tf")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	assertRun(t, []string{path}, "", 2, "", "terrablade: "+path+": input is not a regular file\n")
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
