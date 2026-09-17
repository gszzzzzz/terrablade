// Package syntax provides the lossless syntax foundation for native HCL.
//
// Tree leaves preserve every source byte, including malformed UTF-8, whitespace,
// and comments. Each leaf refers to a half-open byte span in an owned source
// snapshot. One final zero-width EOF marks the end, even for empty input.
// Nodes and tokens are read-only values; trivia is an ordered token element,
// never metadata attached to a neighboring node.
//
// Lexing and file assembly are currently private implementation details. File
// assembly does not validate grammar. A public Parse entry point and its Result
// will be introduced with grammar validation; this foundation exposes neither.
// Diagnostics currently describe lexical errors only. Keywords remain lexical
// identifiers for the eventual parser to interpret.
package syntax
