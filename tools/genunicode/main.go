// Command genunicode regenerates the lexer's Unicode identifier tables.
// Run through go generate ./internal/syntax; normal builds need no network.
// The Unicode version and source checksum are intentionally pinned together:
// changing them changes the accepted HCL identifiers and requires review.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"go/format"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	version      = "17.0.0"
	sourceURL    = "https://www.unicode.org/Public/" + version + "/ucd/DerivedCoreProperties.txt"
	sourceSHA256 = "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08"

	// outputFile is written into the current directory, which go generate
	// sets to the package holding the directive. anchorFile must already be
	// there: it holds the directive and the types the tables refer to, so its
	// presence proves the generator is running where its output belongs.
	outputFile = "unicode_tables.go"
	anchorFile = "identifier.go"

	// maxSourceBytes bounds the download. The pinned file is well under this
	// size, and a response cut off here fails the checksum rather than
	// producing partial tables.
	maxSourceBytes = 4 << 20
)

// properties lists the derived properties emitted, in output order, with the
// Go identifier each table receives.
var properties = []struct{ property, name string }{
	{"ID_Start", "idStart"},
	{"ID_Continue", "idContinue"},
}

// interval is an inclusive range of code points as parsed from the source.
// It uses uint64 to match strconv.ParseUint; the parser bounds hi to the
// Unicode code space before the interval is stored.
type interval struct{ lo, hi uint64 }

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, "genunicode:", err)
		os.Exit(1)
	}
}

// generate runs the whole pipeline: verify the working directory, download
// and verify the source, parse it, render Go source, and write the table file.
func generate() error {
	if _, err := os.Stat(anchorFile); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s not found in the current directory; run go generate ./internal/syntax from the repository root", anchorFile)
		}
		return err
	}

	data, err := download()
	if err != nil {
		return err
	}
	tables, err := parseProperties(data)
	if err != nil {
		return err
	}
	output, err := render(tables)
	if err != nil {
		return err
	}

	return os.WriteFile(outputFile, output, 0o644)
}

// download fetches the pinned source and verifies its checksum, so a changed
// or tampered upstream file can never reach the tables unnoticed.
func download() ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Get(sourceURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", sourceURL, response.Status)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxSourceBytes))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", sourceURL, err)
	}

	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != sourceSHA256 {
		return nil, fmt.Errorf("%s: SHA-256 %s does not match pinned %s", sourceURL, got, sourceSHA256)
	}
	return data, nil
}

// parseProperties extracts the code point ranges of every property listed in
// properties from DerivedCoreProperties.txt. Adjacent ranges are merged; the
// file lists each property in ascending order, so merging only ever touches
// the previously emitted range.
func parseProperties(data []byte) (map[string][]interval, error) {
	wanted := make(map[string]bool, len(properties))
	for _, p := range properties {
		wanted[p.property] = true
	}

	tables := map[string][]interval{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "#")
		fields := strings.Split(line, ";")
		if len(fields) != 2 {
			continue
		}
		property := strings.TrimSpace(fields[1])
		if !wanted[property] {
			continue
		}

		rangeText := strings.TrimSpace(fields[0])
		loText, hiText, multiple := strings.Cut(rangeText, "..")
		lo, err := strconv.ParseUint(loText, 16, 32)
		if err != nil {
			return nil, err
		}
		hi := lo
		if multiple {
			hi, err = strconv.ParseUint(hiText, 16, 32)
			if err != nil {
				return nil, err
			}
		}
		if hi < lo || hi > 0x10FFFF {
			return nil, fmt.Errorf("invalid code point range %s", rangeText)
		}

		// The lexer's binary search needs sorted, disjoint ranges, so any
		// range that starts at or before the previous end is a corrupt
		// source. One that starts right after it extends that range, keeping
		// the table as short as possible.
		ranges := tables[property]
		if len(ranges) > 0 && lo <= ranges[len(ranges)-1].hi+1 {
			if lo <= ranges[len(ranges)-1].hi {
				return nil, fmt.Errorf("%s: unordered or overlapping range %s", property, rangeText)
			}
			ranges[len(ranges)-1].hi = hi
		} else {
			ranges = append(ranges, interval{lo, hi})
		}
		tables[property] = ranges
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for _, p := range properties {
		if len(tables[p.property]) == 0 {
			return nil, fmt.Errorf("%s: no ranges found", p.property)
		}
	}
	return tables, nil
}

// render emits the gofmt-formatted Go source for unicode_tables.go.
func render(tables map[string][]interval) ([]byte, error) {
	var output bytes.Buffer
	fmt.Fprintln(&output, "// Code generated by tools/genunicode; DO NOT EDIT.")
	fmt.Fprintln(&output, "// Pinned ID_Start/ID_Continue properties; see identifier.go for rationale and regeneration.")
	fmt.Fprintf(&output, "// Unicode %s, %s\n// SHA-256: %s\n", version, sourceURL, sourceSHA256)
	fmt.Fprintln(&output, "// Derived Unicode data is covered by ../../LICENSE-UNICODE.\n\npackage syntax")
	fmt.Fprintln(&output, "\n// UnicodeVersion pins identifiers independently of the Go toolchain's tables.")
	fmt.Fprintf(&output, "const UnicodeVersion = %q\n", version)

	for _, p := range properties {
		fmt.Fprintf(&output, "\nvar %s = [...]runeRange{\n", p.name)
		for _, r := range tables[p.property] {
			fmt.Fprintf(&output, "{0x%X, 0x%X},\n", r.lo, r.hi)
		}
		fmt.Fprintln(&output, "}")
	}

	return format.Source(output.Bytes())
}
