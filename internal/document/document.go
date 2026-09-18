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
	indentKind
	ifBreakKind
)

type node struct {
	kind     kind
	text     string
	children []Doc
	// A hard line on the flat path prevents every enclosing group from
	// flattening. The unselected broken branch of IfBreak must not force it.
	forceBreak bool
}

// Text preserves s exactly. It panics for invalid UTF-8 or CR/LF; use line
// primitives for newlines. Tabs are preserved and measured using Options.TabWidth.
func Text(s string) Doc {
	if !utf8.ValidString(s) || strings.ContainsAny(s, "\r\n") {
		panic("document: Text requires valid UTF-8 without CR or LF")
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
	if count <= 1 {
		return only
	}
	children := make([]Doc, 0, count)
	for _, part := range parts {
		if part.node != nil {
			children = append(children, part)
		}
	}
	// Copy only immediate children: repeated Concat(previous, next) remains
	// linear to construct rather than copying the entire prefix every time.
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

var (
	lineNode        = &node{kind: lineKind}
	softLineNode    = &node{kind: softLineKind}
	hardLineNode    = &node{kind: hardLineKind, forceBreak: true}
	literalLineNode = &node{kind: literalLineKind, forceBreak: true}
)

// Group attempts to flatten its contents into the remaining print width.
// If it cannot, its lines break and nested groups make independent decisions.
func Group(content Doc) Doc { return wrap(groupKind, content) }

// Indent adds one indentation level within content. It affects indentation
// after ordinary line breaks, not text already on the current line.
func Indent(content Doc) Doc { return wrap(indentKind, content) }

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
