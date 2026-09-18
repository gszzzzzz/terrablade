// Package document composes immutable layout descriptions and renders them
// deterministically. It is the seam between syntax lowering and printing;
// it does not parse HCL or choose language-specific formatting policies.
//
// Group tries a flat layout, including the continuation on the same line.
// A flat group flattens all nested groups. In a broken group, nested groups
// can fit independently. Outside groups, Line and SoftLine break and IfBreak
// selects its broken branch. HardLine and LiteralLine on a group's flat path
// force that group and its ancestors to break, even at unlimited practical
// widths. A hard line in an unselected IfBreak branch has no effect.
// ForceFlat establishes a lexical flat boundary: nested groups and IfBreak
// stay flat even beyond PrintWidth, while mandatory lines remain intact.
//
// Indent changes the structural indentation context. Ordinary broken lines
// use it; LiteralLine omits automatic indentation once without discarding the
// context. Text owns literal whitespace, including heredoc leading spaces.
// All line primitives emit LF; callers split original CRLF or LF themselves.
// No primitive adds a final newline implicitly or trims literal whitespace.
// Text requires valid UTF-8 and no LF. Lone CR is preserved and measured as a
// zero-width control, never interpreted as a cursor movement or line break.
// Empty constructors and a zero Doc
// render as empty. Invalid text, negative options, and integer layout overflow
// panic because these are construction/configuration errors, not parse errors.
//
// PrintWidth measures terminal columns using pinned Unicode 17 display-width
// tables. East Asian ambiguous characters are always narrow; locale and mutable
// dependency defaults do not affect output. Grapheme clusters spanning Text
// nodes are measured together. Tabs use fixed tab stops. Other control characters
// follow the width library's zero-width rules; this is not a terminal emulator
// and ANSI escape sequences are not interpreted. Actual fonts/terminals can
// display some characters differently. PrintWidth is a preference: Text is
// never split, and indentation or unbreakable text can exceed it.
//
// Constructors hide storage and copy mutable inputs. Strings and child documents
// may share immutable storage, keeping their backing memory alive. No builder,
// arena ownership, or mutable traversal interface is exposed. Construction and
// rendering use no recursive traversal and impose no arbitrary nesting limit.
// Resources scale with expanded document size, not just shared node count.
// Group probes borrow the pending continuation without copying it. Independent
// groups separated by lines take linear work; pathological nested groups can
// revisit overlapping contents and take quadratic time. A long grapheme split
// across many Text nodes can also take quadratic time. Rendering owns
// its output and work stacks, so shared Docs are safe for concurrent reads.
package document
