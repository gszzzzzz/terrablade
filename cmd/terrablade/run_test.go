package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"terrablade"
)

func TestRunStdin(t *testing.T) {
	for _, test := range []struct {
		name, input, output, stderr string
		args                        []string
		status                      int
	}{
		{name: "implicit stdin", input: "a=1", output: "a = 1\n"},
		{name: "explicit stdin", args: []string{"-"}, input: "a=1", output: "a = 1\n"},
		{name: "empty"},
		{name: "blank", input: "\t \n"},
		{name: "normalization", input: `a="${foo.0}"`, output: "a = foo[0]\n"},
		{name: "comment CR", input: "#\r", output: "#\r\r\n"},
		{name: "check change", args: []string{"--check"}, input: "a=1", output: "<stdin>\n", status: 1},
		{name: "check fixed", args: []string{"--check", "-"}, input: "a = 1\n"},
		{name: "check empty", args: []string{"--check"}},
		{name: "parse error", input: "a=", stderr: "<stdin>:1:3: ExpectedExpression: Expected an expression.\n", status: 2},
		{name: "check parse error", args: []string{"--check"}, input: "a=", stderr: "<stdin>:1:3: ExpectedExpression: Expected an expression.\n", status: 2},
		{name: "all diagnostics", input: "a=\xff", stderr: "<stdin>:1:3: InvalidUTF8: Invalid UTF-8 encoding.\n<stdin>:1:3: ExpectedExpression: Expected an expression.\n", status: 2},
		{name: "grapheme position", input: "#👩‍💻\r\nx=\"é\"\r\na=", stderr: "<stdin>:3:3: ExpectedExpression: Expected an expression.\n", status: 2},
		{name: "width and indentation", args: []string{"--print-width=16", "--indent-width", "4"}, input: "b {\n a=[alpha,beta,gamma]\n}\n", output: "b {\n    a = [\n        alpha,\n        beta,\n        gamma,\n    ]\n}\n"},
		{name: "tab width", args: []string{"--print-width=20", "--tab-width=16"}, input: "a=f(\"\t\",beta)", output: "a = f(\n  \"\t\",\n  beta,\n)\n"},
		{name: "zero defaults", args: []string{"--print-width=0", "--indent-width=0", "--tab-width=0"}, input: "b {a=1}", output: "b {\n  a = 1\n}\n"},
		{name: "help", args: []string{"--help"}, output: usage},
		{name: "short help", args: []string{"-h"}, output: usage},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertRun(t, test.args, test.input, test.status, test.output, test.stderr)
		})
	}
}

func TestRunUsageErrorsBeforeReading(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"--check", "--write"}, "--check and --write are mutually exclusive"},
		{[]string{"--write"}, "stdin must be the only input and cannot be used with --write"},
		{[]string{"--write", "-"}, "stdin must be the only input and cannot be used with --write"},
		{[]string{"--check", "-", "file.tf"}, "stdin must be the only input and cannot be used with --write"},
		{[]string{"-", "file.tf"}, "stdin must be the only input and cannot be used with --write"},
		{[]string{"a.tf", "b.tf"}, "multiple files require --check or --write"},
		{[]string{"x.tf", "--check"}, `options must precede files: "--check" (use -- for a dash-prefixed filename)`},
		{[]string{"--check", "x.tf", "--unknown"}, `options must precede files: "--unknown" (use -- for a dash-prefixed filename)`},
		{[]string{"x.tf", "-h"}, `options must precede files: "-h" (use -- for a dash-prefixed filename)`},
		{[]string{"--unknown"}, "flag provided but not defined: -unknown"},
		{[]string{"--print-width"}, "flag needs an argument: -print-width"},
		{[]string{"--print-width=abc"}, `invalid value "abc" for flag -print-width: parse error`},
		{[]string{"--check=invalid"}, `invalid boolean value "invalid" for -check: parse error`},
		{[]string{"--print-width=-1"}, "PrintWidth must not be negative (got -1)"},
		{[]string{"--indent-width=-1"}, "IndentWidth must not be negative (got -1)"},
		{[]string{"--tab-width=-1"}, "TabWidth must not be negative (got -1)"},
		{[]string{"--indent-width=17"}, "IndentWidth must not exceed 16 (got 17)"},
		{[]string{"--tab-width=17"}, "TabWidth must not exceed 16 (got 17)"},
		{[]string{"--indent-width=" + strconv.Itoa(int(^uint(0)>>1))}, "IndentWidth must not exceed 16 (got " + strconv.Itoa(int(^uint(0)>>1)) + ")"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(test.args, forbiddenReader{t}, &stdout, &stderr)
			if status != 2 || stdout.Len() != 0 || stderr.String() != "terrablade: "+test.want+"\n" {
				t.Fatalf("got status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunFiles(t *testing.T) {
	dir := t.TempDir()
	first := putFile(t, dir, "z.tf", "z=1")
	second := putFile(t, dir, "a.tf", "a=2")
	canonical := putFile(t, dir, "fixed.tf", "b = 3\n")
	invalid := putFile(t, dir, "bad.tf", "x=")
	assertRun(t, []string{first}, "", 0, "z = 1\n", "")
	assertContents(t, first, "z=1")
	assertRun(t, []string{"--check", first, canonical, second}, "", 1, first+"\n"+second+"\n", "")
	assertRun(t, []string{"--check", canonical}, "", 0, "", "")
	// Errors dominate changes regardless of which one occurs first. Successful
	// later files are still processed, with diagnostics and paths in input order.
	for _, paths := range [][]string{{invalid, first, second}, {first, invalid, second}} {
		assertRun(t, append([]string{"--check"}, paths...), "", 2, first+"\n"+second+"\n",
			invalid+":1:3: ExpectedExpression: Expected an expression.\n")
	}
	assertRun(t, []string{"--check", first, first}, "", 1, first+"\n"+first+"\n", "")
	assertRun(t, []string{dir}, "", 2, "", "terrablade: "+dir+": input is not a regular file\n")
	assertContents(t, first, "z=1")
	assertContents(t, second, "a=2")
	assertContents(t, invalid, "x=")
}

func TestRunDashFilename(t *testing.T) {
	t.Chdir(t.TempDir())
	putFile(t, ".", "--check", "a=1")
	putFile(t, ".", "-", "b=2")
	putFile(t, ".", "x.tf", "x=3")
	putFile(t, ".", "--", "c=4")
	assertRun(t, []string{"--", "--check"}, "", 0, "a = 1\n", "")
	assertRun(t, []string{"./-"}, "", 0, "b = 2\n", "")
	assertRun(t, []string{"--check", "x.tf"}, "", 1, "x.tf\n", "")
	assertRun(t, []string{"--check", "--", "x.tf", "--check"}, "", 1, "x.tf\n--check\n", "")
	assertRun(t, []string{"--check", "x.tf", "--", "--check"}, "", 1, "x.tf\n--check\n", "")
	assertRun(t, []string{"--", "--"}, "", 0, "c = 4\n", "")
	assertRun(t, []string{"--write", "x.tf", "--check"}, "", 2, "",
		"terrablade: options must precede files: \"--check\" (use -- for a dash-prefixed filename)\n")
	assertContents(t, "x.tf", "x=3")
}

func TestRunEmptyPath(t *testing.T) {
	_, err := os.Stat("")
	var pathError *os.PathError
	if !errors.As(err, &pathError) {
		t.Fatalf("expected an OS error for an empty path: %v", err)
	}
	want := `terrablade: "": ` + pathError.Op + ": " + pathError.Err.Error() + "\n"
	var stdout, stderr bytes.Buffer
	status := run([]string{""}, forbiddenReader{t}, &stdout, &stderr)
	if status != 2 || stdout.Len() != 0 || stderr.String() != want {
		t.Fatalf("empty path: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestRunMissingFiles(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.tf")
	good := putFile(t, dir, "good.tf", "a=1")
	_, err := os.Stat(missing)
	var pathError *os.PathError
	if !errors.As(err, &pathError) {
		t.Fatalf("expected an OS error for a missing path: %v", err)
	}
	want := "terrablade: " + missing + ": " + pathError.Op + ": " + pathError.Err.Error() + "\n"
	var stdout, stderr bytes.Buffer
	status := run([]string{"--check", missing, good}, forbiddenReader{t}, &stdout, &stderr)
	if status != 2 || stdout.String() != good+"\n" || stderr.String() != want {
		t.Fatalf("got status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
}

func TestRunStreamFailures(t *testing.T) {
	var stdout, stderr bytes.Buffer
	status := run(nil, failedReader{}, &stdout, &stderr)
	if status != 2 || stdout.Len() != 0 || stderr.String() != "terrablade: <stdin>: test read failure\n" {
		t.Fatalf("read failure: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
	}
	for _, args := range [][]string{nil, {"--help"}, {"--check"}} {
		for _, writer := range []io.Writer{failedWriter{}, shortWriter{}} {
			stderr.Reset()
			status := run(args, strings.NewReader("a=1"), writer, &stderr)
			if status != 2 || !strings.HasPrefix(stderr.String(), "terrablade: stdout: ") {
				t.Fatalf("write failure %v: status=%d stderr=%q", args, status, stderr.String())
			}
		}
	}
	if status := run(nil, strings.NewReader("a="), &stdout, failedWriter{}); status != 2 {
		t.Fatalf("stderr failure changed error status: %d", status)
	}
}

func TestLabelsEscapeControlCharacters(t *testing.T) {
	for _, path := range []string{"", "ordinary.tf", "path with spaces.tf", "한글.tf", "bad\nname.tf", "bad\x1b[31m.tf", "bad\tname.tf", "bad\xff.tf"} {
		want := path
		if path == "" || strings.ContainsAny(path, "\n\x1b\t") || strings.Contains(path, "\xff") {
			want = strconv.Quote(path)
		}
		if got := pathLabel(path); got != want {
			t.Errorf("pathLabel(%q) = %q, want %q", path, got, want)
		}
	}
	var stderr bytes.Buffer
	reportError(&stderr, "bad\nname.tf", &os.PathError{Op: "open", Path: "bad\nname.tf", Err: os.ErrNotExist})
	if got, want := stderr.String(), "terrablade: \"bad\\nname.tf\": open: file does not exist\n"; got != want {
		t.Fatalf("escaped OS error = %q, want %q", got, want)
	}
	stderr.Reset()
	reportError(&stderr, "file.tf", errors.Join(
		&os.PathError{Op: "write", Path: "file.tf", Err: os.ErrPermission},
		&os.PathError{Op: "close", Path: "file.tf", Err: errors.New("close failed")}))
	if got := stderr.String(); !strings.Contains(got, "write file.tf: permission denied") ||
		!strings.Contains(got, "close file.tf: close failed") || strings.Count(got, "\n") != 1 {
		t.Fatalf("joined failure lost information or injected a line: %q", got)
	}
}

func FuzzRunStdin(f *testing.F) {
	for _, source := range []string{"", "a=1", "a=", "#\r", "a=\xff", `a="${foo.0}"`} {
		f.Add(source, uint8(80), uint8(2), uint8(8), false)
		f.Add(source, uint8(16), uint8(4), uint8(16), true)
	}
	f.Fuzz(func(t *testing.T, source string, width, indent, tab uint8, check bool) {
		options := terrablade.Options{PrintWidth: int(width), IndentWidth: int(indent % 18), TabWidth: int(tab % 18)}
		args := []string{fmt.Sprintf("--print-width=%d", options.PrintWidth),
			fmt.Sprintf("--indent-width=%d", options.IndentWidth), fmt.Sprintf("--tab-width=%d", options.TabWidth)}
		if check {
			args = append(args, "--check")
		}
		want, err := terrablade.Format([]byte(source), options)
		var stdout, stderr bytes.Buffer
		status := run(args, strings.NewReader(source), &stdout, &stderr)
		if err != nil {
			if status != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("rejected input: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
			}
			return
		}
		wantStatus := 0
		if check {
			if string(want) != source {
				wantStatus, want = 1, []byte("<stdin>\n")
			} else {
				want = nil
			}
		}
		if status != wantStatus || !bytes.Equal(stdout.Bytes(), want) || stderr.Len() != 0 {
			t.Fatalf("status=%d stdout=%q stderr=%q, want status=%d stdout=%q", status, stdout.String(), stderr.String(), wantStatus, want)
		}
	})
}

func assertRun(t testing.TB, args []string, input string, status int, stdout, stderr string) {
	t.Helper()
	var out, err bytes.Buffer
	got := run(args, strings.NewReader(input), &out, &err)
	if got != status || out.String() != stdout || err.String() != stderr {
		t.Fatalf("run(%q) = status %d, stdout %q, stderr %q; want %d, %q, %q",
			args, got, out.String(), err.String(), status, stdout, stderr)
	}
}

func putFile(t testing.TB, dir, name, text string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertContents(t testing.TB, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

type forbiddenReader struct{ t testing.TB }

func (r forbiddenReader) Read([]byte) (int, error) {
	r.t.Fatal("unexpected read from stdin")
	return 0, io.EOF
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("test read failure") }

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("test write failure") }

type shortWriter struct{}

func (shortWriter) Write([]byte) (int, error) { return 0, nil }
