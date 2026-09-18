// Package syntax parses native HCL configurations into lossless concrete syntax
// trees. This internal module is shared by Terrablade's consumers; it does not
// provide HCL JSON parsing, expression evaluation, or schema validation.
//
// Parse is the complete-file entry point. Its Result owns a source snapshot and
// provides read-only tree handles and copied diagnostics. The caller may reuse
// the input buffer after Parse returns. Results, nodes, and elements can be
// copied and read concurrently. A node or element keeps its tree alive; retain
// Result or its Source string separately when source text is needed.
//
// A zero Result represents no parse: its root is InvalidNode, its source is
// empty, and its diagnostics are nil. Parse(nil) instead returns an empty File
// containing an empty Body and EOF. Malformed input still produces a File and
// diagnostics, with incomplete or ErrorNode nodes retaining the original bytes.
// All diagnostics are errors; a result with diagnostics must not be formatted.
// Diagnostics are ordered by byte offset, with lexical errors first at equal
// offsets. The tree and diagnostic order are deterministic for identical input.
//
// Tree leaves preserve every source byte, including malformed UTF-8, whitespace,
// and comments. Each leaf refers to a half-open byte span in an owned source
// snapshot. One final zero-width EOF marks the end, even for empty input.
// Nodes and tokens are read-only values; trivia is an ordered token element,
// never metadata attached to a neighboring node.
//
// An ObjectItem's first expression child is its key. A bare name such as foo in
// {foo = 1} keeps its VariableExpression node but denotes the literal key "foo";
// semantic consumers must interpret it through ObjectItem context. Parenthesized
// keys retain their parentheses and represent computed key expressions.
//
// A configuration File contains one Body and a final EOF, with an optional
// leading BOM retained before the Body for upstream compatibility. If a parser
// resource limit is reached, File may also contain ErrorNode and trivia children
// preserving the unparsed remainder between Body and EOF. Body owns
// inter-item and edge trivia. Attribute contains its name token, equals token,
// and value expression. Block contains its type token, zero or more BlockLabel
// nodes, opening brace, nested Body, and closing brace. The braces belong to
// Block, not Body. Labels preserve either an identifier or quoted literal; they
// are never variable-reference expressions. Malformed nodes can be incomplete.
//
// Obtain node or token text with Result.Text and its Span from the same parse.
// Text follows Go's byte-slicing rules and panics for invalid spans; spans carry
// no source identity, so callers must select the intended Result. Result.Source
// returns the complete owned text. Kind String methods supply symbolic names for
// debugging rather than user-facing diagnostic prose; numeric kinds are not a
// storage format.
// Contextual keywords retain their lexical Identifier kind in the tree.
//
// Lexing, grammar recovery, source ownership, and tree storage are private
// implementation details behind Parse. Parsing bounds recursive expression
// nesting and reports NestingLimitExceeded while preserving unparsed bytes.
// Iterative productions can still build deep trees, so consumers should use
// iterative traversal. Child and text access allocate no memory; traversal stacks
// belong to callers, and Diagnostics copies its slice only when errors are present.
package syntax
