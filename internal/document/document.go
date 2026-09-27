package document

import (
	"strings"
	"unicode/utf8"
)

// Doc is an immutable layout description. Its zero value is empty. Copies may
// share storage and can be composed and rendered concurrently.
type Doc struct{ node *node }

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

// node is the immutable storage behind a Doc; the fields in use depend on kind.
type node struct {
	kind kind
	text string
	// children holds Concat parts, or IfBreak's broken and flat branches.
	children []Doc
	// child is the content of a single-child kind. IfBreak uses children so
	// that node stays in a smaller size class.
	child  Doc
	column uint8
	// forceBreak reports a mandatory line on the flat path, which prevents
	// every enclosing group from flattening.
	forceBreak bool
}

// Text returns s as literal content. It panics if s is invalid UTF-8 or
// contains LF. A lone CR is measured as zero width, not as a line break.
func Text(s string) Doc {
	if !utf8.ValidString(s) || strings.ContainsRune(s, '\n') {
		panic("document: Text requires valid UTF-8 without LF")
	}
	if s == "" {
		return Doc{}
	}
	return Doc{&node{kind: textKind, text: s}}
}

// Concat joins parts in order, ignoring empty ones. It does not retain the
// parts slice.
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

	if count <= 1 {
		return only
	}

	// Copying only immediate children keeps repeated Concat(prefix, next) linear.
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

// LiteralLine emits an unindented newline and forces enclosing groups to break.
// Later ordinary lines keep the current indentation.
func LiteralLine() Doc { return Doc{literalLineNode} }

// Line primitives are stateless, so each kind shares one node.
var (
	lineNode        = &node{kind: lineKind}
	softLineNode    = &node{kind: softLineKind}
	hardLineNode    = &node{kind: hardLineKind, forceBreak: true}
	literalLineNode = &node{kind: literalLineKind, forceBreak: true}
)

// Group attempts to flatten its contents into the remaining print width.
// If it cannot, its lines break and nested groups make independent decisions.
func Group(content Doc) Doc { return wrap(groupKind, content) }

// ForceFlat renders content, nested groups, and IfBreak flat regardless of
// print width. HardLine and LiteralLine still break; flat mode resumes after.
func ForceFlat(content Doc) Doc { return wrap(forceFlatKind, content) }

// Indent adds one indentation level within content. It affects indentation
// after ordinary line breaks, not text already on the current line.
func Indent(content Doc) Doc { return wrap(indentKind, content) }

// Cell aligns content with cells of the same column on adjacent rows by
// inserting spaces before it; content supplies its own minimum separator.
// Columns must not decrease within a row, and only a row's first cell of each
// column takes part. A cell spanning an ordinary line break takes no part and
// splits its chain. LiteralLine does not start a row, so a prefix runs through
// literal lines to the start of its row, as upstream HCL measures it.
func Cell(column uint8, content Doc) Doc {
	if content.node == nil {
		return content
	}
	return Doc{&node{kind: cellKind, column: column, child: content, forceBreak: content.node.forceBreak}}
}

// wrap returns a k node around content, or content itself if it is empty.
func wrap(k kind, content Doc) Doc {
	if content.node == nil {
		return content
	}
	return Doc{&node{kind: k, child: content, forceBreak: content.node.forceBreak}}
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
