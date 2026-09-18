package syntax

import "slices"

// Result owns an immutable source snapshot, its lossless tree, and any lexical
// or syntax diagnostics. Copies share immutable storage and may be read
// concurrently. A zero Result has empty source, no diagnostics, and an invalid
// root; call Parse(nil) to obtain a parsed empty file instead.
type Result struct {
	source      string
	root        SyntaxNode
	diagnostics []Diagnostic
}

// Parse reads a complete native HCL configuration. It neither evaluates
// expressions nor validates application-specific schemas or HCL JSON.
//
// Parse does not modify source and owns a copy of its bytes after returning, so
// the caller may then reuse or change the input. The root is always a File whose
// span covers the entire source, including for empty or malformed input.
// Diagnostics report errors; recovery retains every byte in the tree. Excessive
// recursive expression nesting reports NestingLimitExceeded and preserves the
// unparsed remainder. The exact nesting limit is an implementation detail.
func Parse(source []byte) Result {
	p := newParser(source)
	root := p.begin()
	// Upstream accepts a single BOM at byte zero despite the native HCL spec.
	// Preserve it outside Body, where it cannot be confused with a body item.
	if p.current().kind == BOM {
		p.consumeUntil(&root, p.pos+1)
	}
	root.node(p.body())
	return p.file(root)
}

// Root returns the file's read-only tree, or an invalid node for a zero Result.
// Node and element handles keep tree storage alive independently of the Result.
// Access and traversal do not allocate; callers traversing deep trees should
// manage their own iterative stack rather than recurse through every child.
func (r Result) Root() SyntaxNode { return r.root }

// Source returns the owned, unmodified source bytes as an immutable string,
// without copying. A node or token's text is source[span.Start:span.End], using
// its Span and the Source from the same Result. Keep this string or the Result
// when text is needed: tree handles and token values do not retain source bytes.
func (r Result) Source() string { return r.source }

// Diagnostics returns an independent copy of the lexical and syntax errors,
// or nil when there are none. Mutating the returned slice or its values cannot
// change the Result. An input with diagnostics must not be formatted.
//
// Diagnostics are ordered by Span.Start. At equal offsets, lexical errors come
// before parser errors and each phase retains its reporting order. Parsing the
// same bytes produces the same tree and diagnostic order. Spans may overlap or
// be empty, and missing syntax may have diagnostics without an Error node.
func (r Result) Diagnostics() []Diagnostic { return slices.Clone(r.diagnostics) }
