package main

import (
	"bytes"
	"strings"
	"testing"
)

func FuzzRunArguments(f *testing.F) {
	for _, seed := range [][2]string{
		{"check", "true"}, {"check", "false"}, {"write", "true"},
		{"print-width", "80"}, {"print-width", "-1"}, {"indent-width", "17"},
		{"tab-width", "0"}, {"help", "true"}, {"unknown", "\n\x1b\xff"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, option, value string) {
		// Always construct a flag, never a positional pathname. Fuzzing must
		// not accidentally read or replace a file outside the test fixture.
		args := []string{"--" + option + "=" + value}
		var stdout, stderr bytes.Buffer
		status := run(args, strings.NewReader("a=1"), &stdout, &stderr)
		switch status {
		case 0:
			if stderr.Len() != 0 || stdout.String() != "a = 1\n" && stdout.String() != usage {
				t.Fatalf("success: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		case 1:
			if stderr.Len() != 0 || stdout.String() != "<stdin>\n" {
				t.Fatalf("check difference: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		case 2:
			if stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "terrablade: ") ||
				strings.Count(stderr.String(), "\n") != 1 || strings.ContainsRune(stderr.String(), '\x1b') {
				t.Fatalf("argument error: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		default:
			t.Fatalf("undocumented exit status %d", status)
		}
		// The same flag after a positional argument must be diagnosed before
		// any I/O, including when its name or value contains control bytes.
		stdout.Reset()
		stderr.Reset()
		status = run([]string{"input.tf", args[0]}, forbiddenReader{t}, &stdout, &stderr)
		if status != 2 || stdout.Len() != 0 ||
			!strings.HasPrefix(stderr.String(), "terrablade: options must precede files: ") ||
			strings.Count(stderr.String(), "\n") != 1 || strings.ContainsRune(stderr.String(), '\x1b') {
			t.Fatalf("trailing option: status=%d stdout=%q stderr=%q", status, stdout.String(), stderr.String())
		}
	})
}
