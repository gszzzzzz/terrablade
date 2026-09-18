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
// Lexing and expression parsing are private implementation details. The private
// expression parser validates native expressions, not configuration bodies.
// A public Parse entry point and its Result will come with body parsing.
// Diagnostics describe lexical and syntax errors. Contextual keywords retain
// their lexical Identifier kind in the tree.
package syntax
