// Package syntax parses native HCL configurations into lossless concrete
// syntax trees. It does not parse HCL JSON, evaluate expressions, or validate
// schemas.
//
// Parse returns a Result holding the source, the tree, and the diagnostics.
// Every source byte, including whitespace, comments, and malformed UTF-8,
// appears in exactly one token leaf, so concatenating the leaves reproduces
// the input. Trivia is an ordinary token child, not metadata on a neighboring
// node. NodeKind documents each node's children.
//
// Malformed input still yields a complete File: recovery keeps the bytes it
// cannot parse in ErrorNode subtrees or incomplete nodes, and reports each
// problem as a Diagnostic. All diagnostics are errors. Result.Locate converts
// their byte offsets to lines and columns.
//
// Recursive expression nesting is limited; past the limit Parse reports
// NestingLimitExceeded and keeps the unparsed remainder under File. Other
// productions are iterative and can build arbitrarily deep trees, so
// consumers should traverse iteratively too.
package syntax
