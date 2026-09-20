package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"terrablade"
)

func TestRunWrite(t *testing.T) {
	dir := t.TempDir()
	first := putFile(t, dir, "z.tf", "z=1")
	second := putFile(t, dir, "a.tf", "a=2")
	invalid := putFile(t, dir, "bad.tf", "x=")
	canonical := putFile(t, dir, "fixed.tf", "b = 3\n")
	beforeInvalid := statFile(t, invalid)
	assertRun(t, []string{"--write", first, canonical, invalid, second}, "", 2, first+"\n"+second+"\n",
		invalid+":1:3: ExpectedExpression: Expected an expression.\n")
	assertContents(t, first, "z = 1\n")
	assertContents(t, second, "a = 2\n")
	assertContents(t, invalid, "x=")
	if after := statFile(t, invalid); !os.SameFile(beforeInvalid, after) || !beforeInvalid.ModTime().Equal(after.ModTime()) {
		t.Fatal("invalid file was touched")
	}
	assertRun(t, []string{"--write", first, second}, "", 0, "", "")
	assertRun(t, []string{"--check", first, second}, "", 0, "", "")
}

func TestWritePreservesInodeAndMode(t *testing.T) {
	for _, test := range []struct{ source, want string }{
		{"a=1", "a = 1\n"}, {"a    =   1   \n\n\n", "a = 1\n"}, {" \n\t", ""},
	} {
		dir := t.TempDir()
		path := putFile(t, dir, "main.tf", test.source)
		if err := os.Chmod(path, 0640); err != nil {
			t.Fatal(err)
		}
		before := statFile(t, path)
		assertRun(t, []string{"--write", path}, "", 0, path+"\n", "")
		after := statFile(t, path)
		if !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatalf("write replaced inode or mode: before=%+v after=%+v", before, after)
		}
		assertContents(t, path, test.want)
	}
}

func TestWriteUnchangedIsNoOp(t *testing.T) {
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
			t.Fatalf("unchanged file was touched: before=%+v after=%+v", before, after)
		}
		assertContents(t, path, source)
	}
}

func TestWriteOutputFailureStopsLaterFiles(t *testing.T) {
	dir := t.TempDir()
	first := putFile(t, dir, "first.tf", "a=1")
	second := putFile(t, dir, "second.tf", "b=2")
	var stderr bytes.Buffer
	status := run([]string{"--write", first, second}, forbiddenReader{t}, failedWriter{}, &stderr)
	if status != 2 || stderr.String() != "terrablade: stdout: test write failure\n" {
		t.Fatalf("status=%d stderr=%q", status, stderr.String())
	}
	// The first file was written before its report failed. Do not keep
	// modifying later files after the output stream fails.
	assertContents(t, first, "a = 1\n")
	assertContents(t, second, "b=2")
}

func TestWriteLinks(t *testing.T) {
	for _, kind := range []string{"symbolic", "hard"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := putFile(t, dir, "target.tf", "a=1")
			link := filepath.Join(dir, "link.tf")
			var err error
			if kind == "symbolic" {
				err = os.Symlink("target.tf", link)
			} else {
				err = os.Link(target, link)
			}
			if err != nil {
				t.Skipf("filesystem cannot create %s link: %v", kind, err)
			}
			before, err := os.Lstat(link)
			if err != nil {
				t.Fatal(err)
			}
			beforeTarget := statFile(t, target)
			assertRun(t, []string{link}, "", 0, "a = 1\n", "")
			assertRun(t, []string{"--check", link}, "", 1, link+"\n", "")
			assertRun(t, []string{"--write", link, target}, "", 0, link+"\n", "")
			assertContents(t, target, "a = 1\n")
			assertContents(t, link, "a = 1\n")
			after, err := os.Lstat(link)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || !os.SameFile(beforeTarget, statFile(t, target)) {
				t.Fatal("link or target inode was replaced")
			}
			if kind == "symbolic" {
				if value, err := os.Readlink(link); err != nil || value != "target.tf" {
					t.Fatalf("symlink changed: %q, %v", value, err)
				}
			}
			canonical := statFile(t, target)
			assertRun(t, []string{"--write", link}, "", 0, "", "")
			if after := statFile(t, target); !canonical.ModTime().Equal(after.ModTime()) {
				t.Fatal("canonical linked file was touched")
			}
		})
	}
}

func TestWriteFileDoesNotCreateMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.tf")
	if err := writeFile(path, []byte("a = 1\n")); !os.IsNotExist(err) {
		t.Fatalf("missing path: got %v, want not-exist error", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing path was created: %v", err)
	}
}

func FuzzRunWrite(f *testing.F) {
	for _, source := range []string{"", " \n\t", "a=1", "a = 1\n", "a=", "#\r", "a=\xff", `a="${foo.0}"`} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		path := putFile(t, t.TempDir(), "main.tf", source)
		before := statFile(t, path)
		want, err := terrablade.Format([]byte(source), terrablade.Options{})
		var stdout, stderr bytes.Buffer
		status := run([]string{"--write", path}, forbiddenReader{t}, &stdout, &stderr)
		if err != nil {
			if status != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("invalid input: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
			assertContents(t, path, source)
		} else {
			label := ""
			if string(want) != source {
				label = path + "\n"
			}
			if status != 0 || stdout.String() != label || stderr.Len() != 0 {
				t.Fatalf("valid input: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
			assertContents(t, path, string(want))
		}
		after := statFile(t, path)
		if !os.SameFile(before, after) {
			t.Fatal("input inode was replaced")
		}
		if (err != nil || string(want) == source) && !before.ModTime().Equal(after.ModTime()) {
			t.Fatal("invalid or unchanged file was touched")
		}
	})
}

func statFile(t testing.TB, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
