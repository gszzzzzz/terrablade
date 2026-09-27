// Package lowering converts syntax trees into document layouts.
//
// File lowers a complete, diagnostic-free parse into a document.Doc. It
// neither parses nor evaluates. Expressions are first normalized (quoted
// wrappers removed, legacy numeric steps bracketed, object colons turned into
// equals signs) on a private immutable view; the lossless syntax tree is left
// unchanged.
//
// Two kinds of parentheses appear in the output. Permanent parentheses are
// part of the normalized expression and print at every width, because the
// grammar or an operand's precedence needs them. Break parentheses belong to
// a layout and print only when its group breaks, so an expression that fits
// keeps its source spelling.
//
// Lowering is iterative, so deeply nested input does not grow the Go stack,
// and takes time and space linear in the size of the tree. The returned
// documents are immutable and may be rendered concurrently.
package lowering
