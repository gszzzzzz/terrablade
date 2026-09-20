package terrablade

import (
	"fmt"

	"github.com/gszzzzzz/terrablade/internal/document"
	"github.com/gszzzzzz/terrablade/internal/lowering"
	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// Options controls formatting. Each zero field selects its default.
// Out-of-range fields cause Format to return an *OptionsError before parsing.
type Options struct {
	// PrintWidth is the preferred terminal display width, default 80.
	// Unbreakable text, traversals, template sequences, and alignment may exceed it.
	PrintWidth int
	// IndentWidth is the number of spaces per indentation level, default 2.
	// Values from 1 through 16 are accepted; zero selects the default.
	IndentWidth int
	// TabWidth is the distance between tab stops, default 8. Literal tabs are
	// preserved; generated indentation always uses spaces.
	// Values from 1 through 16 are accepted; zero selects the default.
	TabWidth int
}

// Spacing units multiply nesting depth or tab count. Bound that amplification
// and arithmetic while allowing common 2/4/8/16-space layouts. PrintWidth only
// selects breaks and does not need this cap. Deep bodies retain their inherent
// output-size cost, as described in doc.go.
const maxSpacingWidth = 16

// OptionsError identifies an out-of-range layout option. Option is its Go field
// name. PrintWidth must be nonnegative; IndentWidth and TabWidth must be in [0, 16].
// When several options are invalid, Format reports the first in declaration order.
type OptionsError struct {
	Option string
	Value  int
}

func (e *OptionsError) Error() string {
	if (e.Option == "IndentWidth" || e.Option == "TabWidth") && e.Value > maxSpacingWidth {
		return fmt.Sprintf("terrablade: %s must not exceed %d (got %d)", e.Option, maxSpacingWidth, e.Value)
	}
	return fmt.Sprintf("terrablade: %s must not be negative (got %d)", e.Option, e.Value)
}

// Format formats a complete native HCL configuration. It parses, normalizes
// expressions, and lays out the complete file without evaluating expressions or
// validating application-specific schemas. HCL JSON is not supported.
//
// Format neither modifies source nor retains it after returning. The caller may
// reuse source after the call; returned bytes have independent storage.
// Concurrent calls are safe when their input buffers are not being modified.
// Formatting identical input with identical options is deterministic and
// idempotent. Every successful result ends in LF, including an empty input file.
//
// Invalid options return an *OptionsError. Lexical, syntax, and parser nesting
// limit errors return a *ParseError with original-source diagnostics. Every error
// is one of these two types and returns nil output; recovered partial input is
// never formatted. Filenames and diagnostic presentation belong to callers.
// Format performs no I/O.
func Format(source []byte, options Options) ([]byte, error) {
	for _, option := range []struct {
		name  string
		value int
	}{
		{"PrintWidth", options.PrintWidth},
		{"IndentWidth", options.IndentWidth},
		{"TabWidth", options.TabWidth},
	} {
		if option.value < 0 || option.name != "PrintWidth" && option.value > maxSpacingWidth {
			return nil, &OptionsError{Option: option.name, Value: option.value}
		}
	}
	result := syntax.Parse(source)
	if diagnostics := result.Diagnostics(); len(diagnostics) != 0 {
		return nil, newParseError(result.Source(), diagnostics)
	}
	doc, err := lowering.File(result)
	if err != nil {
		// Lowering supports every diagnostic-free native-HCL parse. Failure
		// here is an internal invariant violation, not a third caller error.
		panic(fmt.Sprintf("terrablade: internal invariant: cannot lower diagnostic-free input: %v", err))
	}
	return []byte(document.Render(doc, document.Options{
		PrintWidth: options.PrintWidth, IndentWidth: options.IndentWidth, TabWidth: options.TabWidth,
	})), nil
}
