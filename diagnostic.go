package terrablade

import (
	"fmt"
	"slices"

	"github.com/gszzzzzz/terrablade/internal/syntax"
)

// DiagnosticKind categorizes a Diagnostic. Kinds are stable identifiers for
// programmatic use; the zero value is not a kind, and new kinds may be added.
type DiagnosticKind string

// Lexical and syntax diagnostic kinds.
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

// publicDiagnosticKinds maps each internal kind to its public name. An
// internal kind becomes public only by being listed here.
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

// publicDiagnosticKind panics for an unmapped kind.
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
// End may equal Start, as for missing syntax.
type Span struct {
	Start Position
	End   Position
}

// Diagnostic describes one lexical or syntax error in the original input.
// Message is an English sentence without a location; unlike Kind, its wording
// is not stable. Diagnostic spans may overlap.
type Diagnostic struct {
	Kind    DiagnosticKind
	Message string
	Span    Span
}

// ParseError holds the diagnostics for rejected input, without retaining it.
// It is safe for concurrent use; a zero ParseError has no diagnostics.
type ParseError struct {
	diagnostics []Diagnostic
}

// Error summarizes the first diagnostic. Its wording is not stable; use
// Diagnostics to inspect errors programmatically.
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

// Diagnostics returns a copy of the diagnostics, or nil for a zero ParseError.
// They are sorted stably by start offset, lexical errors first.
func (e *ParseError) Diagnostics() []Diagnostic { return slices.Clone(e.diagnostics) }

// newParseError adds line and column to internal diagnostics, which carry only
// byte offsets, locating every endpoint in one scan.
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
