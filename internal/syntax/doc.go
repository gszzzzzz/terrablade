// Package syntax provides the lossless syntax foundation for native HCL.
//
// Tree leaves preserve every source byte, including malformed UTF-8, whitespace,
// and comments. Each leaf refers to a half-open byte span in an owned source
// snapshot. One final zero-width EOF marks the end, even for empty input.
// Nodes and tokens are read-only values; trivia is an ordered token element,
// never metadata attached to a neighboring node.
//
// Lexing, file assembly, and expression parsing are private implementation
// details. Flat file assembly does not validate grammar; the private expression
// parser validates only its supported expression subset, not configuration
// bodies. A public Parse entry point and its Result will come with body parsing.
// Diagnostics describe lexical and syntax errors. Contextual keywords retain
// their lexical Identifier kind in the tree.
package syntax
