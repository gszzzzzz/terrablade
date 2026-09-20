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

	"github.com/gszzzzzz/terrablade"
)

// Exit codes. Scripts running --check need to tell "changes found" apart from
// a failure, so the former is 1 and every usage, parse, or I/O error is 2.
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

// invocation is a validated command line: the formatting options, the output
// mode, and the inputs to process in order, where "-" stands for stdin.
type invocation struct {
	options      terrablade.Options
	check, write bool
	paths        []string
}

// run owns command policy and presentation. Tests use the same arguments and
// streams as main; filesystem behavior is exercised with real temporary files.
// It processes files in argument order and keeps going after file-local errors.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	command, err := parseArguments(args)
	if errors.Is(err, flag.ErrHelp) {
		if writeErr := writeText(stdout, usage); writeErr != nil {
			reportError(stderr, "stdout", writeErr)
			return exitError
		}
		return exitOK
	}
	if err != nil {
		reportGlobalError(stderr, err)
		return exitError
	}

	// Validation belongs to Format, including defaults and future option limits.
	// Check it before reading any input or changing any file.
	if _, err := terrablade.Format(nil, command.options); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}

	status := exitOK
	for _, path := range command.paths {
		code, fatal := command.process(path, stdin, stdout, stderr)
		status = max(status, code)
		if fatal {
			return status
		}
	}
	return status
}

// parseArguments validates the whole command line before any input is read,
// so a usage error can never follow a partially written file. It returns
// flag.ErrHelp when help was requested.
func parseArguments(args []string) (invocation, error) {
	var command invocation
	flags := flag.NewFlagSet("terrablade", flag.ContinueOnError)
	// run prints the usage text itself, on stdout and only for --help; the flag
	// package would otherwise print its own version to stderr on every error.
	flags.SetOutput(io.Discard)
	flags.BoolVar(&command.check, "check", false, "")
	flags.BoolVar(&command.write, "write", false, "")
	flags.IntVar(&command.options.PrintWidth, "print-width", 0, "")
	flags.IntVar(&command.options.IndentWidth, "indent-width", 0, "")
	flags.IntVar(&command.options.TabWidth, "tab-width", 0, "")
	if err := flags.Parse(args); err != nil {
		return invocation{}, err
	}

	// flag stops at the first positional argument. Diagnose misplaced options
	// in that tail, unless -- already ended option parsing. A later -- also
	// introduces literal filenames; omit that separator without mutating args.
	paths := flags.Args()
	parsed := len(args) - len(paths)
	if parsed == 0 || args[parsed-1] != "--" {
		for i, path := range paths {
			if path == "--" {
				paths = slices.Concat(paths[:i], paths[i+1:])
				break
			}
			if len(path) > 1 && path[0] == '-' {
				return invocation{}, fmt.Errorf("options must precede files: %q (use -- for a dash-prefixed filename)", path)
			}
		}
	}

	// The modes are exclusive because each defines what stdout means: a list of
	// changed inputs, a list of rewritten files, or one formatted document.
	if command.check && command.write {
		return invocation{}, errors.New("--check and --write are mutually exclusive")
	}
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	// Stdin has no file to rewrite in place, and as an unnamed input it cannot
	// be listed alongside files.
	for _, path := range paths {
		if path == "-" && (command.write || len(paths) != 1) {
			return invocation{}, errors.New("stdin must be the only input and cannot be used with --write")
		}
	}
	// Plain output is one formatted document; several inputs would run
	// together on stdout with no boundary between them.
	if len(paths) > 1 && !command.check && !command.write {
		return invocation{}, errors.New("multiple files require --check or --write")
	}

	command.paths = paths
	return command, nil
}

// process formats one input and returns its exit code. The second result is
// true when run must stop: a failed output stream is fatal because continuing
// under --write would modify more files without being able to report them.
func (c invocation) process(path string, stdin io.Reader, stdout, stderr io.Writer) (int, bool) {
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
		return exitError, false
	}

	formatted, err := terrablade.Format(source, c.options)
	if err != nil {
		reportError(stderr, label, err)
		return exitError, false
	}

	changed := !bytes.Equal(source, formatted)
	status := exitOK
	switch {
	case c.check:
		if changed {
			status = exitChanged
			err = writeText(stdout, pathLabel(label)+"\n")
		}
	case c.write:
		// Invalid and unchanged inputs must never be opened for writing.
		if changed {
			if writeErr := writeFile(path, formatted); writeErr != nil {
				reportError(stderr, label, writeErr)
				return exitError, false
			}
			err = writeText(stdout, pathLabel(label)+"\n")
		}
	default:
		_, err = io.Copy(stdout, bytes.NewReader(formatted))
	}
	if err != nil {
		reportError(stderr, "stdout", err)
		return exitError, true
	}
	return status, false
}

// reportGlobalError reports a command-level error that concerns no particular
// input, such as a usage mistake.
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

// pathLabel makes a path safe to print on one line. Filenames may contain
// control characters or invalid UTF-8 that would corrupt or forge terminal
// output, and an empty name would vanish, so those are shown Go-quoted.
func pathLabel(path string) string {
	if path == "" || !utf8.ValidString(path) || strings.ContainsFunc(path, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return strconv.Quote(path)
	}
	return path
}

// writeText writes text to writer, reporting only whether it succeeded.
// io.Copy rather than io.WriteString: Copy turns a short write that reports no
// error into io.ErrShortWrite, so a truncated stdout still fails the command
// instead of silently dropping output (TestRunStreamFailures covers this).
func writeText(writer io.Writer, text string) error {
	_, err := io.Copy(writer, strings.NewReader(text))
	return err
}
