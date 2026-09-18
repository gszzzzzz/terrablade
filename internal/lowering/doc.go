// Package lowering converts syntax into immutable document layouts. File lowers
// a complete diagnostic-free parse. Expression lowers an expression from that
// parse without its enclosing body trivia. Neither entry parses or evaluates.
//
// File applies these body formatting policies:
//
//   - File boundaries: outer blank padding is removed. Nonempty bodies end in
//     one LF; empty bodies do not. A leading BOM is omitted from canonical output.
//   - Blocks: empty blocks use {}; every nonempty block uses an indented body
//     and a closing brace on its own line. Bare labels become quoted labels;
//     already quoted labels retain their spelling. Header comments move in source
//     order immediately before the opening brace, where upstream preserves them.
//   - Item boundaries: consecutive attributes preserve at most one source blank
//     line, which also separates assignment and trailing-comment columns. Boundaries
//     involving a block have exactly one blank line. Independent comment runs
//     retain source blank separation, capped at one line. Inline comments remain
//     inline, and outer body padding is removed without discarding comments.
//   - Heredocs: a following body separator owns the marker's final newline.
//   - Alignment: consecutive rendered attribute rows align their equals signs;
//     structural multiline values and standalone comments split those groups.
//     Heredoc contents and other literal token lines remain opaque. Consecutive
//     trailing line comments align separately. Columns count grapheme clusters,
//     matching upstream HCL formatting. Alignment happens after width-driven
//     line breaks, so padding can exceed the preferred print width.
//
// Both entries normalize expressions before constructing their layouts:
//
//   - Quoted wrappers: a quoted template containing exactly one interpolation
//     and no literal text or directives is replaced with its inner expression.
//     Strip markers on that wrapper disappear. Nested wrappers are removed
//     recursively in one pass, producing an idempotent fixed point. General
//     templates and heredoc templates retain their literal content and structure.
//   - Numeric indices: legacy .number steps become [number], preserving numeric
//     spelling and comments. Steps inside an attribute splat's projection retain
//     legacy syntax: changing foo.*.0 to foo.*[0] would change its meaning.
//     Ordinary traversals and full-splat projections use bracket indices.
//   - Grammar and content: normalization preserves precedence, computed-key
//     meaning, comment order and spelling, and retained token spelling. It adds
//     parentheses where precedence, object-key grammar, comments, or mandatory
//     newlines need them. An unwrapped, unambiguous quoted literal key needs no
//     extra parentheses; computed and nonliteral keys retain that protection.
//     All rewrites live in a private immutable view; the lossless CST is unchanged.
//
// Expression then applies these formatting policies:
//
//   - Expression context: the top-level Expression entry starts where the grammar
//     forbids unparenthesized expression newlines. Internal template interpolation
//     and directive lowering must use the safe=true path, as calls and indices do.
//   - Spelling: names, numeric spellings, explicit parentheses, and comment
//     spelling are preserved. Whitespace between tokens is canonicalized.
//   - Calls and tuples: entries flatten when they fit and otherwise occupy
//     separate indented lines. Broken layouts have a trailing comma except
//     after an expanded call argument or a final heredoc. Source trailing commas
//     are omitted in flat layouts and after final heredocs; required commas
//     after non-final heredocs stay on the next line.
//     Empty delimiters stay compact unless comments require a line break.
//   - Objects: entries use key = value, aligned on consecutive broken rows.
//     Flat entries have
//     comma separators and spaces inside braces; broken entries use one line
//     per entry and a trailing comma. Bare identifier keys retain their spelling
//     and key context, and computed keys retain explicit parentheses. Keys and
//     values remain newline-sensitive, even inside an otherwise safe context.
//     An entry ending in a heredoc omits its comma: the marker's mandatory
//     newline separates entries, and a comma on the next line is invalid HCL.
//     A source newline after the opening brace preserves vertical layout,
//     including empty objects, except inside a template sequence. Tuples and
//     for expressions do not use this source-layout rule.
//   - For expressions: flat clauses use spaces. Broken layouts put the header,
//     projection, and optional if clause on separate indented lines. Object
//     projection arrows may start a continuation line at the same indent. Grouping
//     ellipses stay attached to their values; no trailing comma is introduced.
//   - Templates: quoted and heredoc literal chunks retain their spelling and
//     line structure and never wrap for width. Interpolation and directive
//     contents flatten in safe expression context, including source newlines
//     and nested groups, even beyond print width. Only mandatory comment and
//     heredoc lines remain; this takes precedence over source-vertical objects.
//     Boundaries use ${expr}, ${~expr~}, %{if condition}, and %{~if condition~}.
//     Adjacent object braces add boundary spaces, as in ${ { key = value } }.
//     No whitespace is synthesized outside sequence boundaries into literal text.
//   - Heredocs: opener, marker, literal indentation, and closing-marker spelling
//     are preserved. Literal lines bypass automatic indentation. The marker's
//     terminating LF belongs to the enclosing gap or body; lowering enforces
//     it before following punctuation without adding a final LF to the expression.
//     The literal CR-run rule below also applies at this terminating line ending.
//   - Parentheses: explicit parentheses do not introduce width-triggered breaks;
//     their contents may still break. Binary, conditional, and traversal groups
//     share their break layout with explicit parentheses. Where the grammar
//     forbids expression newlines, broken binary and conditional operations add
//     synthetic parentheses. Width alone adds none to traversals: their steps
//     stay attached, and calls and indices provide their own safe delimiters.
//   - Operators: binary operators and conditional question marks and colons have
//     surrounding spaces and start continuation lines. A same-precedence binary
//     chain shares a group; different precedence and conditional arms can fit
//     independently. Continuation lines use the enclosing delimiter's indentation.
//     Line-leading subtraction uses upstream's tight operand spacing. Operand
//     order and precedence remain unchanged. Moving binary operators to line ends
//     is a separate policy follow-up, not part of expression normalization.
//   - Traversals: attributes, indices, and attribute/full splats retain their
//     projection structure after the normalization above. Dot and bracket steps
//     stay attached even beyond print width. Only mandatory comment/heredoc lines
//     separate steps; these use the enclosing delimiter's indentation.
//     Numeric tokens retain a separating space only when the following dot step
//     could continue the numeric token, as with .0 or .e2 but not .id.
//     This reparse-safety rule deliberately differs from OpenTofu 1.12.6, whose
//     formatter removes those spaces and then rejects its own output. Index
//     expressions can break inside brackets. Calls and following traversal
//     steps make independent fit decisions within a broken expression.
//   - Blank lines: broken tuple and object entry gaps preserve at most one source
//     blank line. Calls collapse blank lines.
//   - Comments: source order is preserved, inline comments stay inline, and
//     source newlines retain standalone comments. Commas move before their
//     leading comments. Line comments force a line break.
//   - Literal content: comment/template indentation and lone CR are preserved.
//     CRLF is normalized to LF except after a literal CR, where normalization
//     would merge that CR into the line ending and lose content on a later pass.
//     A line comment ending in a literal CR gains a protective CR before the
//     following LF, including when File adds the newline to an EOF comment.
//   - Enclosing trivia: the enclosing syntax node owns outer trivia.
//
// Lowering uses iterative traversal, including for deep unary chains. Returned
// documents may share immutable source strings and can be rendered concurrently.
// Construction takes linear time and storage in the expression's tree size;
// rendering follows document's own resource contract. Expression adds no final
// newline; File owns the complete file boundary.
//
// All expression forms produced by a diagnostic-free native-HCL parse are
// supported, as are complete bodies, attributes, blocks, and literal labels.
package lowering
