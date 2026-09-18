// Package lowering converts syntax into immutable document layouts. Expression
// accepts a complete, diagnostic-free parse and an expression node from that
// parse. It does not parse, evaluate, render, or format enclosing body trivia.
//
// Expression applies these formatting policies:
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
//   - Objects: entries use key = value with no alignment. Flat entries have
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
//     projection arrows may start a further-indented continuation line. Grouping
//     ellipses stay attached to their values; no trailing comma is introduced.
//   - Templates: quoted and heredoc literal chunks retain their spelling and
//     line structure and never wrap for width. Interpolation and directive
//     contents flatten in safe expression context, including source newlines
//     and nested groups, even beyond print width. Only mandatory comment and
//     heredoc lines remain; this takes precedence over source-vertical objects.
//     Boundaries use ${expr}, ${~expr~}, %{if condition}, and %{~if condition~}.
//     No whitespace is synthesized outside sequence boundaries into literal text.
//   - Heredocs: opener, marker, literal indentation, and closing-marker spelling
//     are preserved. Literal lines bypass automatic indentation. The marker's
//     terminating LF belongs to the enclosing gap or body; lowering enforces
//     it before following punctuation without adding a final LF to the expression.
//     The literal CR-run rule below also applies at this terminating line ending.
//   - Parentheses: explicit parentheses do not introduce width-triggered breaks;
//     their contents may still break. Binary, conditional, and traversal groups
//     share their break layout with explicit parentheses. Where the grammar
//     forbids expression newlines, broken operations add synthetic parentheses.
//   - Operators: binary operators and conditional question marks and colons have
//     surrounding spaces and start continuation lines. A same-precedence binary
//     chain shares a group; different precedence and conditional arms can fit
//     independently. In safe contexts, continuation lines indent one extra level
//     after the first operand; parentheses supply that indentation themselves.
//     Operand order and precedence remain unchanged.
//   - Traversals: attributes, ordinary and legacy indices, and attribute/full
//     splats retain their syntax and projection structure. Steps can break
//     before a dot; bracket steps stay attached to the previous step. Safe
//     contexts indent traversal continuation lines one extra level after the base.
//     Numeric tokens retain a separating space only when the following dot step
//     could continue the numeric token, as with .0 or .e2 but not .id. Index
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
//   - Enclosing trivia: the enclosing syntax node owns outer trivia.
//
// Lowering uses iterative traversal, including for deep unary chains. Returned
// documents may share immutable source strings and can be rendered concurrently.
// Construction takes linear time and storage in the expression's tree size;
// rendering follows document's own resource contract. No final newline is added.
//
// All expression forms produced by a diagnostic-free native-HCL parse are
// supported. Bodies, attributes, and blocks belong to the enclosing formatter.
package lowering
