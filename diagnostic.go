package terrablade

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/uax29/v2/graphemes"

	"terrablade/internal/syntax"
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

// Keep the external vocabulary explicit. An internal kind must be deliberately
// mapped here before it can appear in a public diagnostic, even when its name
// happens to match. The internal count makes additions visible to our tests.
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

func newParseError(source string, parsed []syntax.Diagnostic) *ParseError {
	diagnostics := make([]Diagnostic, len(parsed))
	endpoints := make([]*Position, 0, 2*len(parsed))
	for i, diagnostic := range parsed {
		diagnostics[i] = Diagnostic{
			Kind: publicDiagnosticKind(diagnostic.Kind), Message: diagnostic.Kind.Message(),
			Span: Span{Start: Position{Offset: diagnostic.Span.Start}, End: Position{Offset: diagnostic.Span.End}},
		}
		endpoints = append(endpoints, &diagnostics[i].Span.Start, &diagnostics[i].Span.End)
	}
	locate(source, endpoints)
	return &ParseError{diagnostics: diagnostics}
}

// Resolve every endpoint in one source scan. Repeated single-position lookups
// would make error reporting quadratic for an input with many diagnostics.
func locate(source string, endpoints []*Position) {
	slices.SortFunc(endpoints, func(a, b *Position) int { return a.Offset - b.Offset })
	line, column, next := 1, 1, 0
	accept := func(end int, newline bool) {
		for next < len(endpoints) && endpoints[next].Offset < end {
			endpoints[next].Line, endpoints[next].Column = line, column
			next++
		}
		if newline {
			line, column = line+1, 1
		} else {
			column++
		}
	}
	for start := 0; start < len(source); {
		// Grapheme iteration assumes valid UTF-8. Invalid bytes each own a
		// column and separate surrounding clusters, matching parser positions.
		end := start
		for end < len(source) {
			r, width := utf8.DecodeRuneInString(source[end:])
			if r == utf8.RuneError && width == 1 {
				break
			}
			end += width
		}
		if end == start {
			accept(start+1, false)
			start++
			continue
		}
		clusters := graphemes.FromString(source[start:end])
		for clusters.Next() {
			accept(start+clusters.End(), strings.HasSuffix(clusters.Value(), "\n"))
		}
		start = end
	}
	for _, endpoint := range endpoints[next:] {
		endpoint.Line, endpoint.Column = line, column
	}
}
