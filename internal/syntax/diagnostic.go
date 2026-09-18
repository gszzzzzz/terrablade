package syntax

var diagnosticMessages = [diagnosticKindCount]string{
	InvalidUTF8:                  "Invalid UTF-8 encoding.",
	InvalidCharacter:             "Invalid character.",
	UnterminatedBlockComment:     "Unterminated block comment; expected '*/'.",
	UnterminatedQuotedTemplate:   "Unterminated quoted template; expected a closing quote.",
	UnterminatedTemplateSequence: "Unterminated template interpolation or directive; expected '}'.",
	InvalidEscape:                "Invalid escape sequence.",
	NewlineInQuotedTemplate:      "A quoted template cannot contain a literal newline; use a heredoc instead.",
	UnterminatedHeredoc:          "Unterminated heredoc; expected a closing marker on its own line.",
	ExpectedExpression:           "Expected an expression.",
	UnexpectedToken:              "Unexpected token.",
	ExpectedClosingParen:         "Expected ')'.",
	ExpectedClosingBracket:       "Expected ']'.",
	ExpectedConditionalColon:     "Expected ':' between the true and false expressions of a conditional.",
	ExpectedArgumentSeparator:    "Expected ',' between function arguments.",
	ExpectedAttributeName:        "Expected an attribute name after '.'.",
	ExpectedFunctionName:         "Expected a function name after '::'.",
	ExpectedOpeningParen:         "Expected '(' to start function arguments.",
	InvalidLegacyIndex:           "A legacy dot index cannot contain a decimal point.",
	NestedAttributeSplat:         "An attribute splat cannot contain another '.*' step.",
	NestingLimitExceeded:         "Expression nesting exceeds the parser limit.",
	InvalidNumber:                "Invalid or unrepresentable number literal.",
	ExpectedTupleSeparator:       "Expected ',' between tuple elements.",
	ExpectedClosingBrace:         "Expected '}'.",
	ExpectedObjectValueSeparator: "Expected '=' or ':' between an object key and its value.",
	ExpectedObjectItemSeparator:  "Expected a comma or newline between object items.",
	ExpectedForVariable:          "Expected an iteration variable name.",
	ExpectedForIn:                "Expected 'in' after the iteration variable names.",
	ExpectedForColon:             "Expected ':' after the collection in a for expression.",
	ExpectedForArrow:             "Expected '=>' between the key and value of an object for expression.",
	UnexpectedForKey:             "A tuple for expression cannot produce a key.",
	UnexpectedForGrouping:        "Grouping with '...' is allowed only in an object for expression.",
	ExpectedTemplateSequenceEnd:  "Expected '}' to close the template sequence.",
	ExpectedTemplateDirective:    "Expected a template directive name.",
	UnknownTemplateDirective:     "Unknown template directive; expected 'if', 'else', 'endif', 'for', or 'endfor'.",
	UnexpectedTemplateDirective:  "The template directive has no matching open scope or repeats an 'else'.",
	ExpectedTemplateEndIf:        "Expected an 'endif' template directive.",
	ExpectedTemplateEndFor:       "Expected an 'endfor' template directive.",
	ExpectedBodyItem:             "Expected an attribute or block name.",
	ExpectedAttributeOrBlock:     "Expected '=' for an attribute, or a label or '{' for a block.",
	ExpectedBodyItemSeparator:    "Expected a newline after the attribute or block.",
	ExpectedBlockOpeningBrace:    "Expected '{' to start the block body.",
	ExpectedLiteralBlockLabel:    "Block labels must be literal strings without interpolation or directives.",
	ExpectedSingleLineAttribute:  "A single-line block may contain only one attribute.",
	ExpectedSingleLineBlockEnd:   "Expected '}' on the same line after the block's attribute.",
	DuplicateAttribute:           "An attribute with this name is already defined in the same body.",
}

// Message returns a standalone English sentence describing the error category,
// without source text, filename, or location. Unknown kinds return
// "Unknown diagnostic.". Message does not allocate.
//
// Wording may improve over time. Use the kind for programmatic decisions and
// String for its stable symbolic name, rather than matching message text.
func (k DiagnosticKind) Message() string {
	if k < diagnosticKindCount {
		return diagnosticMessages[k]
	}
	return "Unknown diagnostic."
}
