// Package lowering converts syntax into immutable document layouts. Expression
// accepts a complete, diagnostic-free parse and an expression node from that
// parse. It does not parse, evaluate, render, or format enclosing body trivia.
//
// Expression preserves names, numeric spellings, explicit parentheses, and
// comment spelling. Whitespace between tokens is canonicalized. Calls and
// tuples flatten when they fit and otherwise put entries on separate indented
// lines, with a trailing comma except after an expanded call argument. Source
// trailing commas are omitted in flat layouts. Empty delimiters stay compact
// unless comments require a line break. Comments remain in source order between
// the same significant elements; line comments force a line break. Literal
// comment content, including indentation and lone CR, is preserved, while CRLF
// is normalized to LF. Outer trivia is owned by the enclosing syntax node.
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
