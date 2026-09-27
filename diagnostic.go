package terrablade

import (
	"fmt"
	"slices"

	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// DiagnosticKind is a stable, symbolic error category. Match these values
// rather than Diagnostic.Message, whose English wording may improve over time.
// The zero value is not a diagnostic kind. Future versions may add new kinds.
type DiagnosticKind string

// Lexical and syntax diagnostic kinds describe native HCL errors.
const (
	InvalidUTF8                  DiagnosticKind = "InvalidUTF8"
	InvalidCharacter             DiagnosticKind = "InvalidCharacter"
	UnterminatedBlockComment     DiagnosticKind = "UnterminatedBlockComment"
	UnterminatedQuotedTemplate   DiagnosticKind = "UnterminatedQuotedTemplate"
	UnterminatedTemplateSequence DiagnosticKind = "UnterminatedTemplateSequence"
	InvalidEscape                DiagnosticKind = "InvalidEscape"
	NewlineInQuotedTemplate      DiagnosticKind = "NewlineInQuotedTemplate"
	UnterminatedHeredoc          DiagnosticKind = "UnterminatedHeredoc"
	ExpectedExpression           DiagnosticKind = "ExpectedExpression"
	UnexpectedToken              DiagnosticKind = "UnexpectedToken"
	ExpectedClosingParen         DiagnosticKind = "ExpectedClosingParen"
	ExpectedClosingBracket       DiagnosticKind = "ExpectedClosingBracket"
	ExpectedConditionalColon     DiagnosticKind = "ExpectedConditionalColon"
	ExpectedArgumentSeparator    DiagnosticKind = "ExpectedArgumentSeparator"
	ExpectedAttributeName        DiagnosticKind = "ExpectedAttributeName"
	ExpectedFunctionName         DiagnosticKind = "ExpectedFunctionName"
	ExpectedOpeningParen         DiagnosticKind = "ExpectedOpeningParen"
	InvalidLegacyIndex           DiagnosticKind = "InvalidLegacyIndex"
	NestedAttributeSplat         DiagnosticKind = "NestedAttributeSplat"
	NestingLimitExceeded         DiagnosticKind = "NestingLimitExceeded"
	InvalidNumber                DiagnosticKind = "InvalidNumber"
	ExpectedTupleSeparator       DiagnosticKind = "ExpectedTupleSeparator"
	ExpectedClosingBrace         DiagnosticKind = "ExpectedClosingBrace"
	ExpectedObjectValueSeparator DiagnosticKind = "ExpectedObjectValueSeparator"
	ExpectedObjectItemSeparator  DiagnosticKind = "ExpectedObjectItemSeparator"
	ExpectedForVariable          DiagnosticKind = "ExpectedForVariable"
	ExpectedForIn                DiagnosticKind = "ExpectedForIn"
	ExpectedForColon             DiagnosticKind = "ExpectedForColon"
	ExpectedForArrow             DiagnosticKind = "ExpectedForArrow"
	UnexpectedForKey             DiagnosticKind = "UnexpectedForKey"
	UnexpectedForGrouping        DiagnosticKind = "UnexpectedForGrouping"
	ExpectedTemplateSequenceEnd  DiagnosticKind = "ExpectedTemplateSequenceEnd"
	ExpectedTemplateDirective    DiagnosticKind = "ExpectedTemplateDirective"
	UnknownTemplateDirective     DiagnosticKind = "UnknownTemplateDirective"
	UnexpectedTemplateDirective  DiagnosticKind = "UnexpectedTemplateDirective"
	ExpectedTemplateEndIf        DiagnosticKind = "ExpectedTemplateEndIf"
	ExpectedTemplateEndFor       DiagnosticKind = "ExpectedTemplateEndFor"
	ExpectedBodyItem             DiagnosticKind = "ExpectedBodyItem"
	ExpectedAttributeOrBlock     DiagnosticKind = "ExpectedAttributeOrBlock"
	ExpectedBodyItemSeparator    DiagnosticKind = "ExpectedBodyItemSeparator"
	ExpectedBlockOpeningBrace    DiagnosticKind = "ExpectedBlockOpeningBrace"
	ExpectedLiteralBlockLabel    DiagnosticKind = "ExpectedLiteralBlockLabel"
	ExpectedSingleLineAttribute  DiagnosticKind = "ExpectedSingleLineAttribute"
	ExpectedSingleLineBlockEnd   DiagnosticKind = "ExpectedSingleLineBlockEnd"
	DuplicateAttribute           DiagnosticKind = "DuplicateAttribute"
)

// The public vocabulary is explicit: an internal kind appears in a public
// diagnostic only once it is mapped here, even when the names match.
var publicDiagnosticKinds = [syntax.DiagnosticKindCount]DiagnosticKind{
	syntax.InvalidUTF8:                  InvalidUTF8,
	syntax.InvalidCharacter:             InvalidCharacter,
	syntax.UnterminatedBlockComment:     UnterminatedBlockComment,
	syntax.UnterminatedQuotedTemplate:   UnterminatedQuotedTemplate,
	syntax.UnterminatedTemplateSequence: UnterminatedTemplateSequence,
	syntax.InvalidEscape:                InvalidEscape,
	syntax.NewlineInQuotedTemplate:      NewlineInQuotedTemplate,
	syntax.UnterminatedHeredoc:          UnterminatedHeredoc,
	syntax.ExpectedExpression:           ExpectedExpression,
	syntax.UnexpectedToken:              UnexpectedToken,
	syntax.ExpectedClosingParen:         ExpectedClosingParen,
	syntax.ExpectedClosingBracket:       ExpectedClosingBracket,
	syntax.ExpectedConditionalColon:     ExpectedConditionalColon,
	syntax.ExpectedArgumentSeparator:    ExpectedArgumentSeparator,
	syntax.ExpectedAttributeName:        ExpectedAttributeName,
	syntax.ExpectedFunctionName:         ExpectedFunctionName,
	syntax.ExpectedOpeningParen:         ExpectedOpeningParen,
	syntax.InvalidLegacyIndex:           InvalidLegacyIndex,
	syntax.NestedAttributeSplat:         NestedAttributeSplat,
	syntax.NestingLimitExceeded:         NestingLimitExceeded,
	syntax.InvalidNumber:                InvalidNumber,
	syntax.ExpectedTupleSeparator:       ExpectedTupleSeparator,
	syntax.ExpectedClosingBrace:         ExpectedClosingBrace,
	syntax.ExpectedObjectValueSeparator: ExpectedObjectValueSeparator,
	syntax.ExpectedObjectItemSeparator:  ExpectedObjectItemSeparator,
	syntax.ExpectedForVariable:          ExpectedForVariable,
	syntax.ExpectedForIn:                ExpectedForIn,
	syntax.ExpectedForColon:             ExpectedForColon,
	syntax.ExpectedForArrow:             ExpectedForArrow,
	syntax.UnexpectedForKey:             UnexpectedForKey,
	syntax.UnexpectedForGrouping:        UnexpectedForGrouping,
	syntax.ExpectedTemplateSequenceEnd:  ExpectedTemplateSequenceEnd,
	syntax.ExpectedTemplateDirective:    ExpectedTemplateDirective,
	syntax.UnknownTemplateDirective:     UnknownTemplateDirective,
	syntax.UnexpectedTemplateDirective:  UnexpectedTemplateDirective,
	syntax.ExpectedTemplateEndIf:        ExpectedTemplateEndIf,
	syntax.ExpectedTemplateEndFor:       ExpectedTemplateEndFor,
	syntax.ExpectedBodyItem:             ExpectedBodyItem,
	syntax.ExpectedAttributeOrBlock:     ExpectedAttributeOrBlock,
	syntax.ExpectedBodyItemSeparator:    ExpectedBodyItemSeparator,
	syntax.ExpectedBlockOpeningBrace:    ExpectedBlockOpeningBrace,
	syntax.ExpectedLiteralBlockLabel:    ExpectedLiteralBlockLabel,
	syntax.ExpectedSingleLineAttribute:  ExpectedSingleLineAttribute,
	syntax.ExpectedSingleLineBlockEnd:   ExpectedSingleLineBlockEnd,
	syntax.DuplicateAttribute:           DuplicateAttribute,
}

// publicDiagnosticKind translates an internal kind through the explicit map
// above. An unmapped kind is a programming error: panicking here keeps a new
// internal kind from leaking into the public vocabulary under its own name.
func publicDiagnosticKind(kind syntax.DiagnosticKind) DiagnosticKind {
	if kind >= syntax.DiagnosticKindCount || publicDiagnosticKinds[kind] == "" {
		panic(fmt.Sprintf("terrablade: internal invariant: unmapped diagnostic kind %s", kind))
	}
	return publicDiagnosticKinds[kind]
}

// Position identifies a location in the original input, including EOF.
// Its zero value is not a source position: the first byte is (0, 1, 1).
type Position struct {
	Offset int // Zero-based byte offset.
	Line   int // One-based line number; only LF starts a new line.
	Column int // One-based Unicode 17 grapheme-cluster column, not display width.
}

// Span is a half-open interval [Start.Offset, End.Offset) in the original input.
// End may equal Start for missing syntax. Neither endpoint includes a filename.
type Span struct {
	Start Position
	End   Position
}

// Diagnostic describes one lexical or syntax error at an original-source span.
// Message is standalone English prose without a location; its wording is not
// stable. Diagnostics may overlap and do not imply a one-to-one mapping to tokens.
type Diagnostic struct {
	Kind    DiagnosticKind
	Message string
	Span    Span
}

// ParseError contains all lexical and syntax diagnostics for rejected input.
// It owns its diagnostics without retaining the input or parse tree. Copies may
// be read concurrently. A zero ParseError has no diagnostics.
type ParseError struct {
	diagnostics []Diagnostic
}

// Error summarizes the first diagnostic. Use Diagnostics for every error and
// programmatic decisions; Error's human-readable wording is not stable.
func (e *ParseError) Error() string {
	if len(e.diagnostics) == 0 {
		return "terrablade: invalid HCL"
	}
	first := e.diagnostics[0]
	noun := "diagnostics"
	if len(e.diagnostics) == 1 {
		noun = "diagnostic"
	}
	return fmt.Sprintf("terrablade: %d:%d: %s (%d %s)",
		first.Span.Start.Line, first.Span.Start.Column, first.Message, len(e.diagnostics), noun)
}

// Diagnostics returns an independent copy, or nil for a zero ParseError.
// Diagnostics are ordered by starting byte offset, with lexical errors first
// at equal offsets. Remaining ties retain their reporting order.
func (e *ParseError) Diagnostics() []Diagnostic { return slices.Clone(e.diagnostics) }

// newParseError converts internal diagnostics, which carry byte offsets only,
// into public ones with line and column, locating every endpoint in one scan.
func newParseError(result syntax.Result, parsed []syntax.Diagnostic) *ParseError {
	located := make([]syntax.Position, 2*len(parsed))
	endpoints := make([]*syntax.Position, len(located))
	for i, diagnostic := range parsed {
		located[2*i].Offset, located[2*i+1].Offset = diagnostic.Span.Start, diagnostic.Span.End
		endpoints[2*i], endpoints[2*i+1] = &located[2*i], &located[2*i+1]
	}
	result.Locate(endpoints)

	diagnostics := make([]Diagnostic, len(parsed))
	for i, diagnostic := range parsed {
		diagnostics[i] = Diagnostic{
			Kind: publicDiagnosticKind(diagnostic.Kind), Message: diagnostic.Kind.Message(),
			Span: Span{Start: Position(located[2*i]), End: Position(located[2*i+1])},
		}
	}
	return &ParseError{diagnostics: diagnostics}
}
