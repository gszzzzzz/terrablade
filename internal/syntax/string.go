package syntax

import "strconv"

var tokenKindNames = [tokenKindCount]string{
	Invalid:             "Invalid",
	EOF:                 "EOF",
	BOM:                 "BOM",
	Whitespace:          "Whitespace",
	Newline:             "Newline",
	LineComment:         "LineComment",
	BlockComment:        "BlockComment",
	Identifier:          "Identifier",
	Number:              "Number",
	OpenBrace:           "OpenBrace",
	CloseBrace:          "CloseBrace",
	OpenBracket:         "OpenBracket",
	CloseBracket:        "CloseBracket",
	OpenParen:           "OpenParen",
	CloseParen:          "CloseParen",
	Plus:                "Plus",
	Minus:               "Minus",
	Star:                "Star",
	Slash:               "Slash",
	Percent:             "Percent",
	And:                 "And",
	Or:                  "Or",
	Bang:                "Bang",
	Equal:               "Equal",
	EqualEqual:          "EqualEqual",
	NotEqual:            "NotEqual",
	Less:                "Less",
	LessEqual:           "LessEqual",
	Greater:             "Greater",
	GreaterEqual:        "GreaterEqual",
	Arrow:               "Arrow",
	Colon:               "Colon",
	DoubleColon:         "DoubleColon",
	Question:            "Question",
	Dot:                 "Dot",
	Ellipsis:            "Ellipsis",
	Comma:               "Comma",
	QuoteOpen:           "QuoteOpen",
	QuoteClose:          "QuoteClose",
	TemplateText:        "TemplateText",
	InterpolationOpen:   "InterpolationOpen",
	DirectiveOpen:       "DirectiveOpen",
	StripMarker:         "StripMarker",
	TemplateSequenceEnd: "TemplateSequenceEnd",
	HeredocOpen:         "HeredocOpen",
	HeredocMarker:       "HeredocMarker",
	HeredocEndMarker:    "HeredocEndMarker",
}

var nodeKindNames = [nodeKindCount]string{
	InvalidNode:             "InvalidNode",
	File:                    "File",
	Error:                   "Error",
	LiteralExpression:       "LiteralExpression",
	VariableExpression:      "VariableExpression",
	ParenthesizedExpression: "ParenthesizedExpression",
	UnaryExpression:         "UnaryExpression",
	BinaryExpression:        "BinaryExpression",
	ConditionalExpression:   "ConditionalExpression",
	FunctionCallExpression:  "FunctionCallExpression",
	TraversalExpression:     "TraversalExpression",
	AttributeAccess:         "AttributeAccess",
	IndexAccess:             "IndexAccess",
	LegacyIndexAccess:       "LegacyIndexAccess",
	AttributeSplat:          "AttributeSplat",
	FullSplat:               "FullSplat",
	TupleExpression:         "TupleExpression",
	ObjectExpression:        "ObjectExpression",
	ObjectItem:              "ObjectItem",
	ForExpression:           "ForExpression",
}

var diagnosticKindNames = [diagnosticKindCount]string{
	InvalidUTF8:                  "InvalidUTF8",
	InvalidCharacter:             "InvalidCharacter",
	UnterminatedBlockComment:     "UnterminatedBlockComment",
	UnterminatedQuotedTemplate:   "UnterminatedQuotedTemplate",
	UnterminatedTemplateSequence: "UnterminatedTemplateSequence",
	InvalidEscape:                "InvalidEscape",
	NewlineInQuotedTemplate:      "NewlineInQuotedTemplate",
	UnterminatedHeredoc:          "UnterminatedHeredoc",
	ExpectedExpression:           "ExpectedExpression",
	UnexpectedToken:              "UnexpectedToken",
	ExpectedClosingParen:         "ExpectedClosingParen",
	ExpectedClosingBracket:       "ExpectedClosingBracket",
	ExpectedConditionalColon:     "ExpectedConditionalColon",
	ExpectedArgumentSeparator:    "ExpectedArgumentSeparator",
	ExpectedAttributeName:        "ExpectedAttributeName",
	ExpectedFunctionName:         "ExpectedFunctionName",
	ExpectedOpeningParen:         "ExpectedOpeningParen",
	InvalidLegacyIndex:           "InvalidLegacyIndex",
	NestedAttributeSplat:         "NestedAttributeSplat",
	UnsupportedExpression:        "UnsupportedExpression",
	NestingLimitExceeded:         "NestingLimitExceeded",
	InvalidNumber:                "InvalidNumber",
	ExpectedTupleSeparator:       "ExpectedTupleSeparator",
	ExpectedClosingBrace:         "ExpectedClosingBrace",
	ExpectedObjectValueSeparator: "ExpectedObjectValueSeparator",
	ExpectedObjectItemSeparator:  "ExpectedObjectItemSeparator",
	ExpectedForVariable:          "ExpectedForVariable",
	ExpectedForIn:                "ExpectedForIn",
	ExpectedForColon:             "ExpectedForColon",
	ExpectedForArrow:             "ExpectedForArrow",
	UnexpectedForKey:             "UnexpectedForKey",
	UnexpectedForGrouping:        "UnexpectedForGrouping",
}

// String returns the source-facing name used in diagnostics and debug output.
func (k TokenKind) String() string {
	if k < tokenKindCount && tokenKindNames[k] != "" {
		return tokenKindNames[k]
	}
	return "TokenKind(" + strconv.FormatUint(uint64(k), 10) + ")"
}

// String returns the grammar-facing name used in diagnostics and debug output.
func (k NodeKind) String() string {
	if k < nodeKindCount && nodeKindNames[k] != "" {
		return nodeKindNames[k]
	}
	return "NodeKind(" + strconv.FormatUint(uint64(k), 10) + ")"
}

// String returns the stable symbolic name of a diagnostic category. User-facing
// diagnostic messages remain separate because they need source context and prose.
func (k DiagnosticKind) String() string {
	if k < diagnosticKindCount && diagnosticKindNames[k] != "" {
		return diagnosticKindNames[k]
	}
	return "DiagnosticKind(" + strconv.FormatUint(uint64(k), 10) + ")"
}
