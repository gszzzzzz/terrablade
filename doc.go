// Package terrablade formats native HCL configuration files.
//
// Format parses, normalizes, and lays out a complete file in one call. It needs
// no Terraform or OpenTofu executable, evaluates no expressions, and checks no
// application schema. HCL JSON is not supported.
//
// # Layout
//
// Formatting removes outer blank lines and a leading BOM, and ends every file,
// even an empty one, with LF. Attribute groups keep at most one source blank
// line; blocks are separated by one blank line. Assignments and trailing
// comments align within consecutive groups. Comments and literal template
// content, including lone CR bytes, are preserved. CRLF becomes LF except where
// a CR must remain for literal CR content to survive the next parse.
//
// # Normalization
//
// Interpolation-only quoted wrappers are removed recursively: "${a}" becomes a.
// Legacy numeric traversal steps become bracket indices: foo.0 becomes foo[0],
// except inside attribute-splat projections, where the rewrite would change
// scope. Parentheses are kept where they protect precedence, computed keys,
// comments, or mandatory lines. Templates and heredocs keep their literal
// content, and traversals and template sequences are not split to fit the
// preferred width.
//
// # Compatibility
//
// Default indentation follows Terraform and OpenTofu conventions; wrapping and
// blank-line policies are Terrablade's own. Output with default indentation is
// meant to be left unchanged by terraform fmt and tofu fmt. Known exceptions:
// Terrablade keeps a space after a numeric token when a following dot step could
// extend it, as with .0 or .e2 (some tofu fmt versions remove that space and
// then reject their own output), and indents mandatory comment lines in general
// quoted templates differently.
//
// # Diagnostics
//
// Diagnostics describe the original input, not the formatted output. Lines and
// grapheme-cluster columns are one-based; byte offsets are zero-based. CRLF is
// one cluster and one line ending, and an offset within a cluster shares its
// starting column. Each malformed UTF-8 byte, tab, and lone CR occupies one
// column. Layout instead measures Unicode 17 terminal display widths, with East
// Asian ambiguous characters narrow and tabs expanded to TabWidth stops.
//
// # Resource use
//
// The parser limits expression nesting, reporting NestingLimitExceeded; other
// traversal is iterative. Output grows quadratically with block nesting depth,
// and pathological nested groups or fragmented grapheme clusters can take
// quadratic rendering time. Locating d diagnostics in n input bytes takes
// O(n + d log d) time. There is no cancellation or output-size limit, so
// callers formatting untrusted input should bound its size.
package terrablade
