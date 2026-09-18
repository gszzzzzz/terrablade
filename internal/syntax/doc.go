// Package syntax provides the lossless syntax foundation for native HCL.
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
// leading BOM retained before the Body for upstream compatibility. Body owns
// inter-item and edge trivia. Attribute contains its name token, equals token,
// and value expression. Block contains its type token, zero or more BlockLabel
// nodes, opening brace, nested Body, and closing brace. The braces belong to
// Block, not Body. Labels preserve either an identifier or quoted literal; they
// are never variable-reference expressions. Malformed nodes can be incomplete.
//
// Lexing, expression parsing, and configuration-body parsing are private
// implementation details. A public Parse entry point and its Result are deferred
// until source ownership and diagnostic access contracts are settled.
// Diagnostics describe lexical and syntax errors. Contextual keywords retain
// their lexical Identifier kind in the tree.
package syntax
