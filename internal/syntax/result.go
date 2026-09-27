package syntax

import "slices"

// Result holds a parsed source, its tree, and its diagnostics. It is immutable
// and safe for concurrent use. A zero Result has an InvalidNode root; Parse(nil)
// returns an empty File instead.
type Result struct {
	source      string
	root        Node
	diagnostics []Diagnostic
}

// Parse parses a complete native HCL configuration. It copies source, so the
// caller may reuse the slice afterwards. The root is always a File spanning the
// whole source, even when the input is empty or malformed.
func Parse(source []byte) Result {
	p := newParser(source)
	root := p.begin()
	// Upstream accepts a BOM at byte zero, although the native HCL spec does
	// not. Keep it outside Body so it cannot be taken for a body item.
	if p.current().kind == BOM {
		p.consumeUntil(&root, p.pos+1)
	}
	root.node(p.body())
	return p.file(root)
}

// Root returns the File node, or the zero Node for a zero Result.
func (r Result) Root() Node { return r.root }

// Source returns the parsed source. Tree handles do not retain it.
func (r Result) Source() string { return r.source }

// Text returns r.Source()[span.Start:span.End]. Spans do not record which
// Result they came from; the caller must pass one from r.
func (r Result) Text(span Span) string { return r.source[span.Start:span.End] }

// Diagnostics returns a copy of the lexical and syntax errors, or nil if there
// are none. They are ordered by Span.Start, with lexical errors first at equal
// offsets. Spans may be empty or overlap, and need not cover an ErrorNode. An
// input with diagnostics must not be formatted.
func (r Result) Diagnostics() []Diagnostic { return slices.Clone(r.diagnostics) }
