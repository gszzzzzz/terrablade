package terrablade

import (
	"fmt"
	"math"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// Options controls formatting. A zero field selects its default; negative
// values are invalid.
type Options struct {
	// PrintWidth is the preferred line width in terminal display columns,
	// default 80. Unbreakable text and alignment may exceed it.
	PrintWidth int
	// IndentWidth is the number of spaces per indentation level, at most 16,
	// default 2.
	IndentWidth int
	// TabWidth is the distance between tab stops, at most 16, default 8.
	// Literal tabs are preserved; indentation always uses spaces.
	TabWidth int
}

// maxSpacingWidth caps IndentWidth and TabWidth, which are multiplied by
// nesting depth or tab count. PrintWidth only selects breaks and needs no cap.
const maxSpacingWidth = 16

// OptionsError reports an out-of-range Options field. Option is the field name
// and [Min, Max] the accepted range; Max is math.MaxInt if there is no limit.
type OptionsError struct {
	Option   string
	Value    int
	Min, Max int
}

func (e *OptionsError) Error() string {
	switch {
	case e.Value > e.Max:
		return fmt.Sprintf("terrablade: %s must not exceed %d (got %d)", e.Option, e.Max, e.Value)
	case e.Min == 0:
		return fmt.Sprintf("terrablade: %s must not be negative (got %d)", e.Option, e.Value)
	default:
		return fmt.Sprintf("terrablade: %s must be at least %d (got %d)", e.Option, e.Min, e.Value)
	}
}

// Format returns the formatted form of source, a complete native HCL file.
//
// Format neither modifies nor retains source, and the result does not share its
// storage. It performs no I/O and is safe for concurrent use while source is not
// modified. Output is deterministic, idempotent, and ends in LF.
//
// Format returns nil and an *OptionsError for invalid options, or nil and a
// *ParseError for invalid source. It returns no other errors.
func Format(source []byte, options Options) ([]byte, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}

	result := syntax.Parse(source)
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		return nil, newParseError(result, diagnostics)
	}

	return []byte(document.Render(lowering.File(result), document.Options{
		PrintWidth: options.PrintWidth, IndentWidth: options.IndentWidth, TabWidth: options.TabWidth,
	})), nil
}

// Validate returns an *OptionsError for the first out-of-range field, in
// declaration order, or nil. Format performs the same check.
func (o Options) Validate() error {
	for _, option := range []struct {
		name  string
		value int
		max   int
	}{
		{"PrintWidth", o.PrintWidth, math.MaxInt},
		{"IndentWidth", o.IndentWidth, maxSpacingWidth},
		{"TabWidth", o.TabWidth, maxSpacingWidth},
	} {
		if option.value < 0 || option.value > option.max {
			return &OptionsError{Option: option.name, Value: option.value, Min: 0, Max: option.max}
		}
	}
	return nil
}
