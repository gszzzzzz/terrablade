package document

import (
	"strings"
	"unicode/utf8"
)

// Doc is an immutable layout description. Its zero value is empty. Copies may
// share storage and can be composed and rendered concurrently.
type Doc struct{ node *node }

// kind identifies which primitive a node represents.
type kind uint8

const (
	textKind kind = iota
	concatKind
	lineKind
	softLineKind
	hardLineKind
	literalLineKind
	groupKind
	forceFlatKind
	indentKind
	ifBreakKind
	cellKind
)

// node is the shared, immutable storage behind a Doc. Which fields are
// populated depends on kind.
type node struct {
	kind kind
	// text is the literal content of a textKind node.
	text string
	// children holds the parts of a concatKind node, the single content of
	// the wrapping kinds, or the broken and flat branches of ifBreakKind.
	children []Doc
	// column is the alignment column; it is only meaningful for cellKind.
	column uint8
	// A hard line on the flat path prevents every enclosing group from
	// flattening. The unselected broken branch of IfBreak must not force it.
	forceBreak bool
}

// Text preserves s exactly. It panics for invalid UTF-8 or LF; use line
// primitives for LF/CRLF newlines. Lone CR is preserved as a zero-width control,
// not a line break. Tabs are preserved and measured using Options.TabWidth.
func Text(s string) Doc {
	if !utf8.ValidString(s) || strings.ContainsRune(s, '\n') {
		panic("document: Text requires valid UTF-8 without LF")
	}
	if s == "" {
		return Doc{}
	}
	return Doc{&node{kind: textKind, text: s}}
}

// Concat joins documents in order, retaining an independent copy of parts.
// Empty documents are ignored; nested concatenations need not be flattened.
func Concat(parts ...Doc) Doc {
	count := 0
	var only Doc
	force := false
	for _, part := range parts {
		if part.node != nil {
			count++
			only = part
			force = force || part.node.forceBreak
		}
	}

	// A single non-empty part needs no node of its own.
	if count <= 1 {
		return only
	}

	// Copy only immediate children: repeated Concat(previous, next) remains
	// linear to construct rather than copying the entire prefix every time.
	children := make([]Doc, 0, count)
	for _, part := range parts {
		if part.node != nil {
			children = append(children, part)
		}
	}
	return Doc{&node{kind: concatKind, children: children, forceBreak: force}}
}

// Line is a space in a flat group and an indented newline otherwise.
func Line() Doc { return Doc{lineNode} }

// SoftLine is empty in a flat group and an indented newline otherwise.
func SoftLine() Doc { return Doc{softLineNode} }

// HardLine always emits an indented newline and forces enclosing groups to break.
func HardLine() Doc { return Doc{hardLineNode} }

// LiteralLine always emits a newline without automatic indentation on the next
// line. It forces enclosing groups to break, but preserves their indentation
// context: a later ordinary line resumes indentation. Literal leading spaces
// belong in Text, allowing heredoc text and interpolations to be composed.
func LiteralLine() Doc { return Doc{literalLineNode} }

// Line primitives carry no per-instance state, so every call shares one node
// per kind instead of allocating.
var (
	lineNode        = &node{kind: lineKind}
	softLineNode    = &node{kind: softLineKind}
	hardLineNode    = &node{kind: hardLineKind, forceBreak: true}
	literalLineNode = &node{kind: literalLineKind, forceBreak: true}
)

// Group attempts to flatten its contents into the remaining print width.
// If it cannot, its lines break and nested groups make independent decisions.
func Group(content Doc) Doc { return wrap(groupKind, content) }

// ForceFlat flattens content and all nested groups regardless of print width.
// It selects flat IfBreak branches, but preserves HardLine and LiteralLine.
// Flat mode resumes after those mandatory lines and ends at this boundary.
func ForceFlat(content Doc) Doc { return wrap(forceFlatKind, content) }

// Indent adds one indentation level within content. It affects indentation
// after ordinary line breaks, not text already on the current line.
func Indent(content Doc) Doc { return wrap(indentKind, content) }

// Cell marks content for vertical alignment with the same column on adjacent
// rendered rows. Lower-numbered columns must precede higher-numbered columns.
// Only the first cell in each column on a row participates, even if it spans
// multiple rows; later nested cells may participate on their own starting rows.
// Content supplies its own minimum separator. Alignment adds spaces before it
// after all line breaks have been chosen, so padding may exceed PrintWidth.
// Columns count grapheme clusters, not terminal display cells.
// A cell spanning an ordinary newline is ineligible and splits its chain.
// LiteralLine belongs to opaque text and does not separate alignment rows.
func Cell(column uint8, content Doc) Doc {
	if content.node == nil {
		return content
	}
	return Doc{&node{kind: cellKind, column: column, children: []Doc{content}, forceBreak: content.node.forceBreak}}
}

// wrap builds a single-child node of kind k. An empty child yields an empty
// document, so wrappers never add nodes that render nothing. The child's
// forceBreak propagates because a mandatory line inside a wrapper still
// forces the groups outside it.
func wrap(k kind, content Doc) Doc {
	if content.node == nil {
		return content
	}
	return Doc{&node{kind: k, children: []Doc{content}, forceBreak: content.node.forceBreak}}
}

// IfBreak selects broken when the nearest enclosing group breaks, and flat
// when it flattens. Outside a group it selects broken. Only the flat branch
// participates in deciding whether that group can flatten.
func IfBreak(broken, flat Doc) Doc {
	if broken.node == nil && flat.node == nil {
		return Doc{}
	}
	return Doc{&node{
		kind: ifBreakKind, children: []Doc{broken, flat},
		forceBreak: flat.node != nil && flat.node.forceBreak,
	}}
}
