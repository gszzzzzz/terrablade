// Package lowering converts syntax into immutable document layouts. Expression
// accepts a complete, diagnostic-free parse and an expression node from that
// parse. It does not parse, evaluate, render, or format enclosing body trivia.
//
// Expression applies these formatting policies:
//
//   - Spelling: names, numeric spellings, explicit parentheses, and comment
//     spelling are preserved. Whitespace between tokens is canonicalized.
//   - Calls and tuples: entries flatten when they fit and otherwise occupy
//     separate indented lines. Broken layouts have a trailing comma except
//     after an expanded call argument; flat layouts omit source trailing commas.
//     Empty delimiters stay compact unless comments require a line break.
//   - Parentheses: explicit parentheses do not introduce width-triggered breaks;
//     their contents may still break.
//   - Blank lines: broken tuple entry gaps preserve at most one source blank
//     line. Calls collapse blank lines.
//   - Comments: source order is preserved, inline comments stay inline, and
//     source newlines retain standalone comments. Commas move before their
//     leading comments. Line comments force a line break.
//   - Literal comment content: indentation and lone CR are preserved. CRLF is
//     normalized to LF except after a literal CR, where normalization would
//     merge that CR into the line ending and lose content on a later pass.
//   - Enclosing trivia: the enclosing syntax node owns outer trivia.
//
// Lowering uses iterative traversal, including for deep unary chains. Returned
// documents may share immutable source strings and can be rendered concurrently.
// Construction takes linear time and storage in the expression's tree size;
// rendering follows document's own resource contract. No final newline is added.
//
// This initial implementation supports literals, variable references, unary
// expressions, explicit parentheses, calls, and tuples. Other expression forms
// return an error until their lowering policies are implemented.
package lowering
