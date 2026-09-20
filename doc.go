// Package terrablade formats complete native HCL configurations through Format.
// Parsing, expression normalization, and layout are one operation; callers do
// not manage syntax trees or rendering state. No Terraform/OpenTofu executable,
// expression evaluation, or application schema is needed. HCL JSON is excluded.
//
// # Layout
//
// Formatting removes outer blank padding and a leading BOM, and emits a final
// LF for every file, including empty or whitespace-only files. Attribute
// groups retain at most one source blank line; block boundaries have one blank
// line. Assignments and trailing comments align within consecutive groups.
// Comments and literal template content are preserved, including lone CR bytes.
// CRLF becomes LF except where a protective CR is needed to retain literal CR
// content on the next parse.
//
// # Normalization
//
// Interpolation-only quoted wrappers are removed recursively: "${a}" becomes a.
// Legacy numeric traversal steps become bracket indices: foo.0 becomes foo[0].
// Steps inside attribute-splat projections retain their legacy syntax to
// preserve scope. Parentheses protect precedence, computed keys, comments, and
// mandatory lines. General templates and heredocs retain their literal content.
// Traversals and template sequences stay attached beyond the preferred width.
//
// # Compatibility
//
// Default indentation follows Terraform/OpenTofu conventions, while width-driven
// wrapping and canonical blank-line policies are Terrablade's own. Representative
// default-indent output is tested as a formatting fixed point for both tools.
// Known exceptions include the protective space kept after a numeric token when
// the following dot step could extend that token, as with .0 or .e2 (OpenTofu
// 1.12.6 removes the space and then rejects its own output), and indentation of
// mandatory comment lines in general quoted templates. Attribute-splat legacy
// indices are intentionally retained, not modernized across their scope.
//
// # Diagnostics
//
// Diagnostics describe the original bytes, not normalized output. Line and
// grapheme columns are one-based; byte offsets are zero-based. CRLF is one
// cluster and line ending; an offset within a cluster shares its starting
// column. Each malformed UTF-8 byte counts separately. Tabs and lone CR each
// occupy one column in diagnostics. Layout instead uses Unicode 17 terminal
// display widths, with narrow East Asian ambiguous characters and configurable
// tab stops. Filenames and error presentation belong to callers.
//
// # Resource use
//
// Calls share no mutable state. Storage scales with input, intermediate trees,
// and expanded output. Recursive expression nesting has a parser limit, reported
// as NestingLimitExceeded. Traversal and layout use iterative work stacks, but
// deeply indented blocks inherently produce quadratic output bytes. Pathological
// nested layout groups or fragmented graphemes can take quadratic rendering time.
// Diagnostic locations take O(n + d log d) time and O(d) additional storage for
// n input bytes and d diagnostics. There is no cancellation or output-size limit;
// callers that process untrusted input should impose appropriate resource limits.
package terrablade
