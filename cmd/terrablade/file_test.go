package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWrite(t *testing.T) {
	if !writeSupported {
		t.Skip("write policy is not implemented on this platform")
	}
	dir := t.TempDir()
	first := putFile(t, dir, "z.tf", "z=1")
	second := putFile(t, dir, "a.tf", "a=2")
	invalid := putFile(t, dir, "bad.tf", "x=")
	canonical := putFile(t, dir, "fixed.tf", "b = 3\n")
	assertRun(t, []string{"--write", first, canonical, invalid, second}, "", 2, first+"\n"+second+"\n",
		invalid+":1:3: ExpectedExpression: Expected an expression.\n")
	assertContents(t, first, "z = 1\n")
	assertContents(t, second, "a = 2\n")
	assertContents(t, invalid, "x=")
	assertRun(t, []string{"--write", first, second}, "", 0, "", "")
	assertRun(t, []string{"--check", first, second}, "", 0, "", "")
	assertNoTemps(t, dir)
}

func TestWriteUnchangedIsNoOp(t *testing.T) {
	if !writeSupported {
		t.Skip("write policy is not implemented on this platform")
	}
	dir := t.TempDir()
	for _, source := range []string{"a = 1\n", ""} {
		path := putFile(t, dir, "fixed.tf", source)
		old := time.Unix(1_600_000_000, 0)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
		before := statFile(t, path)
		assertRun(t, []string{"--write", path}, "", 0, "", "")
		after := statFile(t, path)
		if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() {
			t.Fatalf("unchanged file was replaced: before=%+v after=%+v", before, after)
		}
		assertContents(t, path, source)
	}
	assertNoTemps(t, dir)
}

func TestWriteOutputFailureStopsLaterFiles(t *testing.T) {
	if !writeSupported {
		t.Skip("write policy is not implemented on this platform")
	}
	dir := t.TempDir()
	first := putFile(t, dir, "first.tf", "a=1")
	second := putFile(t, dir, "second.tf", "b=2")
	var stderr bytes.Buffer
	status := run([]string{"--write", first, second}, forbiddenReader{t}, failedWriter{}, &stderr)
	if status != 2 || stderr.String() != "terrablade: stdout: test write failure\n" {
		t.Fatalf("status=%d stderr=%q", status, stderr.String())
	}
	// The first file was committed before its report failed. Do not claim it
	// rolled back, and do not keep mutating files after the output stream fails.
	assertContents(t, first, "a = 1\n")
	assertContents(t, second, "b=2")
	assertNoTemps(t, dir)
}

func TestWriteLinks(t *testing.T) {
	if !writeSupported {
		t.Skip("write policy is not implemented on this platform")
	}
	for _, kind := range []string{"symbolic", "hard"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := putFile(t, dir, "target.tf", "a=1")
			link := filepath.Join(dir, "link.tf")
			var err error
			var refusal string
			if kind == "symbolic" {
				err = os.Symlink("target.tf", link)
				refusal = "refusing to replace a symbolic link"
			} else {
				err = os.Link(target, link)
				refusal = "refusing to replace a file with multiple hard links"
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(link)
			if err != nil {
				t.Fatal(err)
			}
			assertRun(t, []string{link}, "", 0, "a = 1\n", "")
			assertRun(t, []string{"--check", link}, "", 1, link+"\n", "")
			assertRun(t, []string{"--write", link}, "", 2, "", "terrablade: "+link+": "+refusal+"\n")
			assertContents(t, target, "a=1")
			assertContents(t, link, "a=1")
			// Making the target canonical allows --write without any replacement,
			// including when the pathname is itself a symbolic or hard link.
			if err := os.WriteFile(target, []byte("a = 1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			canonical := statFile(t, target)
			assertRun(t, []string{"--write", link}, "", 0, "", "")
			after, err := os.Lstat(link)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || !os.SameFile(canonical, statFile(t, target)) {
				t.Fatal("link or target was replaced")
			}
			if kind == "symbolic" {
				if value, err := os.Readlink(link); err != nil || value != "target.tf" {
					t.Fatalf("symlink changed: %q, %v", value, err)
				}
			}
			assertNoTemps(t, dir)
		})
	}
}

func TestWriteThroughSymlinkedDirectory(t *testing.T) {
	if !writeSupported {
		t.Skip("write policy is not implemented on this platform")
	}
	dir := t.TempDir()
	targetDir := filepath.Join(dir, "target")
	if err := os.Mkdir(targetDir, 0700); err != nil {
		t.Fatal(err)
	}
	target := putFile(t, targetDir, "main.tf", "a=1")
	link := filepath.Join(dir, "linked-dir")
	if err := os.Symlink(targetDir, link); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(link, "main.tf")
	assertRun(t, []string{"--write", path}, "", 0, path+"\n", "")
	assertContents(t, target, "a = 1\n")
	assertNoTemps(t, targetDir)
}

func TestWriteRejectsStaleSnapshot(t *testing.T) {
	if !writeSupported {
		t.Skip("write policy is not implemented on this platform")
	}
	for _, change := range []string{"size", "mtime", "identity", "mode", "deleted"} {
		t.Run(change, func(t *testing.T) {
			dir := t.TempDir()
			path := putFile(t, dir, "main.tf", "a=1")
			_, snapshot, err := readFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "a=1"
			switch change {
			case "size":
				want = "a=123"
				putFile(t, dir, "main.tf", want)
			case "mtime":
				later := snapshot.ModTime().Add(time.Hour)
				err = os.Chtimes(path, later, later)
			case "identity":
				other := putFile(t, dir, "other.tf", want)
				err = os.Rename(other, path)
			case "mode":
				err = os.Chmod(path, 0644)
			case "deleted":
				err = os.Remove(path)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := replaceFile(path, []byte("a = 1\n"), snapshot); err == nil {
				t.Fatal("stale snapshot was overwritten")
			}
			if change == "deleted" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("deleted path was recreated: %v", err)
				}
			} else {
				assertContents(t, path, want)
			}
			assertNoTemps(t, dir)
		})
	}
}

func TestWriteUnsupportedPlatform(t *testing.T) {
	if writeSupported {
		t.Skip("write is supported on this platform")
	}
	dir := t.TempDir()
	path := putFile(t, dir, "main.tf", "a=1")
	assertRun(t, []string{"--write", path}, "", 2, "", "terrablade: --write is only supported on macOS and Linux\n")
	assertContents(t, path, "a=1")
	assertNoTemps(t, dir)
}

func statFile(t testing.TB, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func assertNoTemps(t testing.TB, dir string) {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasPrefix(file.Name(), ".terrablade-") {
			t.Errorf("temporary file leaked: %s", file.Name())
		}
	}
}
