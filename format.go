package terrablade

import (
	"fmt"

	"terrablade/internal/document"
	"terrablade/internal/lowering"
	"terrablade/internal/syntax"
)

// Options controls formatting. Each zero field selects its default.
// Negative fields cause Format to return an *OptionsError before parsing.
type Options struct {
	// PrintWidth is the preferred terminal display width, default 80.
	// Unbreakable text, traversals, template sequences, and alignment may exceed it.
	PrintWidth int
	// IndentWidth is the number of spaces per indentation level, default 2.
	IndentWidth int
	// TabWidth is the distance between tab stops, default 8. Literal tabs are
	// preserved; generated indentation always uses spaces.
	TabWidth int
}

// OptionsError identifies a negative layout option. Option is its Go field name.
// When several options are invalid, Format reports the first in declaration order.
type OptionsError struct {
	Option string
	Value  int
}

func (e *OptionsError) Error() string {
	return fmt.Sprintf("terrablade: %s must not be negative (got %d)", e.Option, e.Value)
}

// Format formats a complete native HCL configuration. It parses, normalizes
// expressions, and lays out the complete file without evaluating expressions or
// validating application-specific schemas. HCL JSON is not supported.
//
// Format neither modifies source nor retains it after returning. The caller may
// reuse source after the call; returned nonempty bytes have independent storage.
// Concurrent calls are safe when their input buffers are not being modified.
// Formatting identical input with identical options is deterministic and
// idempotent. A successful empty result has length zero; its nilness is unspecified.
//
// Invalid options return an *OptionsError. Lexical, syntax, and parser nesting
// limit errors return a *ParseError with original-source diagnostics. Every error
// returns nil output; recovered partial input is never formatted. Filenames and
// diagnostic presentation belong to callers. Format performs no I/O.
//
// Layout arithmetic overflow panics. Positive options have no arbitrary upper
// bound, so callers should choose widths appropriate for the desired output size.
func Format(source []byte, options Options) ([]byte, error) {
	for _, option := range []struct {
		name  string
		value int
	}{
		{"PrintWidth", options.PrintWidth},
		{"IndentWidth", options.IndentWidth},
		{"TabWidth", options.TabWidth},
	} {
		if option.value < 0 {
			return nil, &OptionsError{Option: option.name, Value: option.value}
		}
	}
	result := syntax.Parse(source)
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		return nil, newParseError(result.Source(), diagnostics)
	}
	doc, err := lowering.File(result)
	if err != nil {
		return nil, fmt.Errorf("terrablade: cannot format parsed input: %w", err)
	}
	return []byte(document.Render(doc, document.Options{
		PrintWidth: options.PrintWidth, IndentWidth: options.IndentWidth, TabWidth: options.TabWidth,
	})), nil
}
