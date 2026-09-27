// Package document builds immutable layout descriptions and renders them as
// text. It knows nothing about HCL; callers choose the formatting policy.
//
// # Layout
//
// Group renders its content flat, together with the continuation up to the
// next line break, if that fits within PrintWidth. Otherwise its lines break and
// nested groups decide independently. Outside any group, Line and SoftLine
// break and IfBreak selects its broken branch. HardLine and LiteralLine on a
// group's flat path force that group and its ancestors to break. ForceFlat
// keeps nested groups and IfBreak flat regardless of width, but mandatory lines
// still break.
//
// Line primitives emit LF. Rendering never adds a final newline or trims Text,
// and it writes indentation only before text, so empty lines carry none.
//
// # Width
//
// Layout measures terminal display columns, with East Asian ambiguous
// characters narrow, tabs expanded to TabWidth stops, and grapheme clusters
// that span Text nodes measured together. Alignment instead counts grapheme
// clusters. PrintWidth is a preference: Text is never split, and alignment
// padding is added after line breaks are chosen, so either may exceed it.
//
// # Errors and resource use
//
// Invalid Text, negative options, and width overflow panic, as programming
// errors. Docs may be shared and rendered concurrently. Construction and
// rendering are iterative, with no nesting limit, and scale with the expanded
// document size. Pathological nested groups, or one grapheme cluster split
// across many Text nodes, can take quadratic time.
package document
