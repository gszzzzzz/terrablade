package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"terrablade"
)

const (
	exitOK      = 0
	exitChanged = 1
	exitError   = 2
)

const usage = `Usage: terrablade [options] [file ...]

Format stdin (no file, or -) or one file to stdout.
Multiple files require --check or --write. Options must precede files;
use -- before a filename beginning with a dash, and ./- for a file named -.

Options:
  --check             List inputs that would change; do not write formatted text
  --write             Write changed files in place
  --print-width int   Preferred display width (default 80; 0 selects default)
  --indent-width int  Spaces per level (default 2; 1..16; 0 selects default)
  --tab-width int     Distance between tab stops (default 8; 1..16; 0 selects default)
  --help              Show this help

--check and --write are mutually exclusive. Stdin must be the only input
and cannot be used with --write. Directories are not supported.
Exit codes: 0 success, 1 --check found changes, 2 usage, parse, or I/O error.
`

// run owns command policy and presentation. Tests use the same arguments and
// streams as main; filesystem behavior is exercised with real temporary files.
// It processes files in argument order and keeps going after file-local errors.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	var options terrablade.Options
	var check, write bool
	flags := flag.NewFlagSet("terrablade", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&check, "check", false, "")
	flags.BoolVar(&write, "write", false, "")
	flags.IntVar(&options.PrintWidth, "print-width", 0, "")
	flags.IntVar(&options.IndentWidth, "indent-width", 0, "")
	flags.IntVar(&options.TabWidth, "tab-width", 0, "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			if err := writeText(stdout, usage); err != nil {
				reportError(stderr, "stdout", err)
				return exitError
			}
			return exitOK
		}
		reportGlobalError(stderr, err)
		return exitError
	}
	paths := flags.Args()
	// flag stops at the first positional argument. Diagnose misplaced options
	// in that tail, unless -- already ended option parsing. A later -- also
	// introduces literal filenames; omit that separator without mutating args.
	parsed := len(args) - len(paths)
	if parsed == 0 || args[parsed-1] != "--" {
		for i, path := range paths {
			if path == "--" {
				paths = slices.Concat(paths[:i], paths[i+1:])
				break
			}
			if len(path) > 1 && path[0] == '-' {
				reportGlobalError(stderr, fmt.Errorf("options must precede files: %q (use -- for a dash-prefixed filename)", path))
				return exitError
			}
		}
	}
	if check && write {
		reportGlobalError(stderr, errors.New("--check and --write are mutually exclusive"))
		return exitError
	}
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	for _, path := range paths {
		if path == "-" && (write || len(paths) != 1) {
			reportGlobalError(stderr, errors.New("stdin must be the only input and cannot be used with --write"))
			return exitError
		}
	}
	if len(paths) > 1 && !check && !write {
		reportGlobalError(stderr, errors.New("multiple files require --check or --write"))
		return exitError
	}
	// Validation belongs to Format, including defaults and future option limits.
	// Check it before reading any input or changing any file.
	if _, err := terrablade.Format(nil, options); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	status := exitOK
	for _, path := range paths {
		label := path
		var source []byte
		var err error
		if path == "-" {
			label = "<stdin>"
			source, err = io.ReadAll(stdin)
		} else {
			source, err = readFile(path)
		}
		if err != nil {
			reportError(stderr, label, err)
			status = exitError
			continue
		}
		formatted, err := terrablade.Format(source, options)
		if err != nil {
			reportError(stderr, label, err)
			status = exitError
			continue
		}
		changed := !bytes.Equal(source, formatted)
		switch {
		case check:
			if changed {
				status = max(status, exitChanged)
				err = writeText(stdout, pathLabel(label)+"\n")
			}
		case write:
			// Invalid and unchanged inputs must never be opened for writing.
			if changed {
				err = writeFile(path, formatted)
				if err != nil {
					reportError(stderr, label, err)
					status = exitError
					continue
				}
				err = writeText(stdout, pathLabel(label)+"\n")
			}
		default:
			_, err = io.Copy(stdout, bytes.NewReader(formatted))
		}
		if err != nil {
			// Stop on a failed output stream: continuing --write would modify
			// more files without being able to report their successful writes.
			reportError(stderr, "stdout", err)
			return exitError
		}
	}
	return status
}

func reportGlobalError(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "terrablade: %s\n", pathLabel(err.Error()))
}

// reportError always identifies its input or output stream. An empty filename
// is an actual argument, not a sentinel for a global command error.
func reportError(stderr io.Writer, label string, err error) {
	var parsed *terrablade.ParseError
	if errors.As(err, &parsed) {
		for _, diagnostic := range parsed.Diagnostics() {
			fmt.Fprintf(stderr, "%s:%d:%d: %s: %s\n", pathLabel(label),
				diagnostic.Span.Start.Line, diagnostic.Span.Start.Column,
				diagnostic.Kind, diagnostic.Message)
		}
		return
	}
	// OS error strings may contain raw filenames. Keep paths in our escaped
	// label and retain the operation and underlying cause without duplicating it.
	// Only shorten a direct OS error. A joined error can describe both write
	// and close failures; neither should disappear behind one child PathError.
	if detail, ok := err.(*os.PathError); ok {
		err = fmt.Errorf("%s: %v", detail.Op, detail.Err)
	}
	fmt.Fprintf(stderr, "terrablade: %s: %s\n", pathLabel(label), pathLabel(err.Error()))
}

func pathLabel(path string) string {
	if path == "" || !utf8.ValidString(path) || strings.ContainsFunc(path, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return strconv.Quote(path)
	}
	return path
}

func writeText(writer io.Writer, text string) error {
	_, err := io.Copy(writer, strings.NewReader(text))
	return err
}
