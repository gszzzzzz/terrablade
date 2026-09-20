package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCLIProcess(t *testing.T) {
	dir := t.TempDir()
	name := "terrablade"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	for _, test := range []struct {
		name, input, stdout, stderr string
		args                        []string
		status                      int
	}{
		{name: "stdin", input: "a=1", stdout: "a = 1\n"},
		{name: "stdin dash", args: []string{"-"}, input: `a="${foo.0}"`, stdout: "a = foo[0]\n"},
		{name: "check change", args: []string{"--check"}, input: "a=1", stdout: "<stdin>\n", status: 1},
		{name: "check fixed", args: []string{"--check"}, input: "a = 1\n"},
		{name: "parse error", input: "a=", stderr: "<stdin>:1:3: ExpectedExpression: Expected an expression.\n", status: 2},
		{name: "bad option", args: []string{"--tab-width=17"}, input: "a=1", stderr: "terrablade: TabWidth must not exceed 16 (got 17)\n", status: 2},
		{name: "help", args: []string{"--help"}, stdout: usage},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertProcess(t, binary, dir, test.args, test.input, test.status, test.stdout, test.stderr)
		})
	}
	t.Run("files", func(t *testing.T) {
		putFile(t, dir, "z.tf", "z=1")
		putFile(t, dir, "a.tf", "a=2")
		putFile(t, dir, "bad.tf", "b=")
		assertProcess(t, binary, dir, []string{"z.tf"}, "", 0, "z = 1\n", "")
		assertProcess(t, binary, dir, []string{"--check", "z.tf", "a.tf"}, "", 1, "z.tf\na.tf\n", "")
		assertContents(t, filepath.Join(dir, "z.tf"), "z=1")
		if writeSupported {
			assertProcess(t, binary, dir, []string{"--write", "z.tf", "bad.tf", "a.tf"}, "", 2, "z.tf\na.tf\n",
				"bad.tf:1:3: ExpectedExpression: Expected an expression.\n")
			assertContents(t, filepath.Join(dir, "z.tf"), "z = 1\n")
			assertContents(t, filepath.Join(dir, "a.tf"), "a = 2\n")
			assertContents(t, filepath.Join(dir, "bad.tf"), "b=")
			assertProcess(t, binary, dir, []string{"--write", "z.tf", "a.tf"}, "", 0, "", "")
			assertProcess(t, binary, dir, []string{"--check", "z.tf", "a.tf"}, "", 0, "", "")
		} else {
			assertProcess(t, binary, dir, []string{"--write", "z.tf"}, "", 2, "", "terrablade: --write is only supported on macOS and Linux\n")
			assertContents(t, filepath.Join(dir, "z.tf"), "z=1")
		}
		assertNoTemps(t, dir)
	})
	t.Run("broken stdout", func(t *testing.T) {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		reader.Close()
		defer writer.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary)
		command.Stdin = strings.NewReader("a=1")
		command.Stdout = writer
		var stderr bytes.Buffer
		command.Stderr = &stderr
		err = command.Run()
		var exited *exec.ExitError
		if !errors.As(err, &exited) || exited.ExitCode() != 2 || !strings.HasPrefix(stderr.String(), "terrablade: stdout: ") {
			t.Fatalf("broken stdout: error=%v stderr=%q; want exit 2 and I/O diagnostic", err, stderr.String())
		}
	})
}

func assertProcess(t *testing.T, binary, dir string, args []string, input string, status int, stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = dir
	command.Stdin = strings.NewReader(input)
	// An empty PATH makes an accidental Terraform/OpenTofu runtime dependency
	// fail in the actual command, not just in an in-process runner test.
	command.Env = append(os.Environ(), "PATH=")
	var out, errOut bytes.Buffer
	command.Stdout, command.Stderr = &out, &errOut
	err := command.Run()
	gotStatus := 0
	if err != nil {
		var exited *exec.ExitError
		if !errors.As(err, &exited) {
			t.Fatal(err)
		}
		gotStatus = exited.ExitCode()
	}
	if gotStatus != status || out.String() != stdout || errOut.String() != stderr {
		t.Fatalf("CLI %q: status=%d stdout=%q stderr=%q; want %d, %q, %q",
			args, gotStatus, out.String(), errOut.String(), status, stdout, stderr)
	}
}
