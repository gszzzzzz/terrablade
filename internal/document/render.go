package document

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
)

// Options controls layout. Zero fields use defaults; negative fields panic.
type Options struct {
	// PrintWidth is the preferred terminal display width, default 80. Text is
	// never split, so unbreakable content can exceed it.
	PrintWidth int
	// IndentWidth is the number of spaces per indentation level, default 2.
	IndentWidth int
	// TabWidth is the distance between tab stops, default 8. Literal tabs are
	// preserved; automatic indentation always uses spaces.
	TabWidth int
}

// The Options field documentation quotes these values; normalized substitutes
// them for zero fields.
const (
	defaultPrintWidth  = 80
	defaultIndentWidth = 2
	defaultTabWidth    = 8
)

// Render returns a deterministic layout. It emits LF newlines only when asked
// by the document and does not append a final newline or trim Text whitespace.
// Automatic indentation is delayed until text, so empty lines have no padding.
func Render(doc Doc, options Options) string {
	r := renderer{options: options.normalized(), stack: []command{{doc: doc}}}

	for len(r.stack) > 0 {
		last := len(r.stack) - 1
		current := r.stack[last]
		r.stack = r.stack[:last]

		// A cell-end marker is popped exactly when every command pushed for
		// the cell's content has been rendered, so the current row is the
		// cell's last row. Its zero doc must not be treated as content.
		if current.cellEnd != 0 {
			r.cells[current.cellEnd-1].lastRow = r.row
			continue
		}
		n := current.doc.node
		if n == nil {
			continue
		}

		switch n.kind {
		case textKind:
			r.emitText(n.text)
		case lineKind, softLineKind, hardLineKind, literalLineKind:
			r.emitLine(current, n.kind)
		case groupKind:
			r.enterGroup(current, n)
		case cellKind:
			r.enterCell(current, n)
		default:
			r.stack = expand(r.stack, current, r.options.IndentWidth)
		}
	}

	return alignCells(r.output.String(), r.cells)
}

// renderer is the mutable state of one Render call. Each call owns its own
// renderer, which is what makes shared Docs safe to render concurrently.
type renderer struct {
	options Options
	output  strings.Builder
	// line tracks the display width of the current output line.
	line lineWidth
	// pendingIndent is indentation owed to the current line that has not
	// been written yet. Writing it lazily, on the first text, is what keeps
	// empty lines free of trailing spaces and lets a cell at the start of a
	// line account for indentation it has not yet emitted.
	pendingIndent int
	// stack holds the commands still to render, last first. probeScratch is
	// kept between groups so fits does not reallocate its stack.
	stack, probeScratch []command
	// cells records every alignment cell in output order for alignCells.
	cells []renderedCell
	// row counts structural line breaks; rowStart is the output offset of
	// the current row. LiteralLine advances neither, because literal text
	// belongs to one opaque row for alignment purposes.
	row, rowStart int
}

// command is one unit of pending render work: a document to emit in a given
// indentation and flat/broken mode, or a cell-end marker.
type command struct {
	doc    Doc
	indent int
	// flat selects the flat branch of lines and IfBreak. It is inherited by
	// every command expanded from this one.
	flat bool
	// cellEnd is the one-based index of an alignment cell whose content ends
	// at this command, or zero for ordinary commands. Sharing the render
	// stack with content is what lets the renderer learn a cell's last row
	// without a recursive traversal; one-based so that the zero command is
	// never mistaken for a marker.
	cellEnd int
}

// flushIndent writes the indentation deferred by the last line break. It runs
// before any visible output on the line, so empty lines stay unpadded.
func (r *renderer) flushIndent() {
	if r.pendingIndent <= 0 {
		return
	}
	r.output.WriteString(strings.Repeat(" ", r.pendingIndent))
	r.line.width = r.pendingIndent
	r.pendingIndent = 0
}

func (r *renderer) emitText(text string) {
	r.flushIndent()
	r.output.WriteString(text)
	r.line.measure(text, r.options.TabWidth)
}

// emitLine renders one line primitive in the mode of the enclosing group.
func (r *renderer) emitLine(current command, k kind) {
	if current.flat && (k == lineKind || k == softLineKind) {
		if k == lineKind {
			r.flushIndent()
			r.output.WriteByte(' ')
			r.line.measure(" ", r.options.TabWidth)
		}
		return
	}

	r.output.WriteByte('\n')
	r.line = lineWidth{}

	// A literal line owes no indentation and does not start an alignment
	// row; the indentation context survives in current.indent for the next
	// ordinary line.
	if k == literalLineKind {
		r.pendingIndent = 0
		return
	}
	r.row++
	r.rowStart = r.output.Len()
	r.pendingIndent = current.indent
}

// enterGroup decides whether a group renders flat, then schedules its content.
// A group inside a flat ancestor or with a mandatory line break needs no probe.
func (r *renderer) enterGroup(current command, n *node) {
	if !current.flat && !n.forceBreak {
		candidate := command{doc: n.children[0], indent: current.indent, flat: true}

		// Indentation owed to this line occupies columns once text arrives,
		// so the probe must start from it rather than from the empty line.
		start := r.line
		if r.pendingIndent > 0 {
			start.width = r.pendingIndent
		}
		current.flat, r.probeScratch = fits(candidate, r.stack, start, r.options, r.probeScratch)
	}

	current.doc = n.children[0]
	r.stack = append(r.stack, current)
}

// enterCell records where a cell starts and schedules a marker to learn where
// it ends. The marker is pushed first so it pops after the content.
func (r *renderer) enterCell(current command, n *node) {
	r.cells = append(r.cells, renderedCell{
		column:        n.column,
		position:      r.output.Len(),
		rowStart:      r.rowStart,
		firstRow:      r.row,
		pendingIndent: r.pendingIndent,
	})
	r.stack = append(r.stack, command{cellEnd: len(r.cells)})

	current.doc = n.children[0]
	r.stack = append(r.stack, current)
}

// fits reports whether candidate, rendered flat, keeps the current line within
// the print width, starting from line, a copy of the renderer's tracker for the
// current output line. A probe includes the continuation, not just the candidate
// group. Otherwise a closing delimiter or following operator could overflow a
// line that "fits". It stops at the first physical newline; text beyond it
// uses a fresh width. The continuation is borrowed read-only. Copying the
// render stack for every group would make even a flat list of independent
// groups quadratic. The returned slice is the emptied scratch stack, handed
// back so the next probe can reuse its capacity.
func fits(candidate command, continuation []command, line lineWidth, options Options, scratch []command) (bool, []command) {
	stack := append(scratch[:0], candidate)
	next := len(continuation) - 1

	for len(stack) > 0 || next >= 0 {
		if line.width > options.PrintWidth {
			return false, stack[:0]
		}

		var current command
		if len(stack) > 0 {
			last := len(stack) - 1
			current = stack[last]
			stack = stack[:last]
		} else {
			current = continuation[next]
			next--
		}
		n := current.doc.node
		if n == nil {
			continue
		}

		switch n.kind {
		case textKind:
			line.measure(n.text, options.TabWidth)
		case lineKind, softLineKind:
			if !current.flat {
				return true, stack[:0]
			}
			if n.kind == lineKind {
				line.measure(" ", options.TabWidth)
			}
		case hardLineKind, literalLineKind:
			return true, stack[:0]
		case groupKind, cellKind:
			// Candidate descendants inherit flat mode. An undecided sibling
			// keeps the continuation's broken mode, so its break opportunity
			// can end this line instead of forcing an earlier group to break.
			current.doc = n.children[0]
			stack = append(stack, current)
		default:
			stack = expand(stack, current, options.IndentWidth)
		}
	}

	return line.width <= options.PrintWidth, stack[:0]
}

// expand pushes the children of a structural node in render order. Text,
// lines, groups, and cells are handled by the callers because they produce
// output or need probe state; the kinds here only rewrite the command.
func expand(stack []command, current command, indentWidth int) []command {
	n := current.doc.node

	switch n.kind {
	case concatKind:
		for i := len(n.children) - 1; i >= 0; i-- {
			stack = append(stack, command{doc: n.children[i], indent: current.indent, flat: current.flat})
		}
	case indentKind:
		current.indent = addWidth(current.indent, indentWidth)
		current.doc = n.children[0]
		stack = append(stack, current)
	case forceFlatKind:
		current.flat = true
		current.doc = n.children[0]
		stack = append(stack, current)
	case ifBreakKind:
		index := 0
		if current.flat {
			index = 1
		}
		current.doc = n.children[index]
		stack = append(stack, current)
	default:
		// Every other kind is handled by a caller before it gets here, so a
		// kind reaching this point means a new primitive was added without
		// teaching the render and probe loops about it.
		panic("document: unhandled kind")
	}

	return stack
}

// lineWidth measures the display width of one output line across Text nodes.
// It retains the last grapheme because the next Text may extend it, e.g.
// separate Text("👩") and Text("\u200d💻"). Measuring each node independently
// is incorrect. A copy is an independent probe: the retained string is
// immutable.
type lineWidth struct {
	// width is the display width so far, in terminal cells.
	width int
	// tail is the last accepted grapheme cluster and tailWidth its width, so
	// the cluster can be re-measured if the next text extends it.
	tail      string
	tailWidth int
}

// measure adds text to the current line's width. It is named measure rather
// than append so that it does not shadow the builtin in a package that uses
// append throughout.
func (w *lineWidth) measure(text string, tabWidth int) {
	if text == "" {
		return
	}

	// Two adjacent ASCII code points never form one grapheme cluster (CR LF
	// is the only ASCII pair, and Text cannot contain LF), so the retained
	// tail cannot be extended. This is the common path for tokens, spaces,
	// and punctuation: no copying.
	if w.tail != "" {
		ascii := w.tail[len(w.tail)-1] < utf8.RuneSelf && text[0] < utf8.RuneSelf
		if !ascii {
			var done bool
			if text, done = w.joinTail(text, tabWidth); done {
				return
			}
		}
	}

	graphemes := (displaywidth.Options{}).StringGraphemes(text)
	for graphemes.Next() {
		w.accept(graphemes.Value(), graphemes.Width(), tabWidth)
	}
}

// joinTail re-measures the grapheme cluster that straddles the boundary
// between the retained tail and text. It grows the tail one cluster of text
// at a time until the joined string splits, so only the crossing cluster is
// ever copied. It returns the unmeasured remainder of text and whether text
// was consumed entirely.
func (w *lineWidth) joinTail(text string, tabWidth int) (string, bool) {
	tailBytes := len(w.tail)
	boundary := w.tail
	prefix := (displaywidth.Options{}).StringGraphemes(text)

	for prefix.Next() {
		boundary += prefix.Value()
		joined := (displaywidth.Options{}).StringGraphemes(boundary)
		joined.Next()
		cluster := joined.Value()

		if len(cluster) < len(boundary) {
			// Only the cluster crossing the node boundary needs joining.
			// Resume at its end in the original text, even when RI pairing
			// moves that end into a cluster from the independent iterator.
			w.width -= w.tailWidth
			w.accept(cluster, joined.Width(), tabWidth)
			return text[len(cluster)-tailBytes:], false
		}
		if len(boundary)-tailBytes == len(text) {
			// All of text extends the tail into a single cluster, so there
			// is nothing left to measure independently.
			w.width -= w.tailWidth
			w.accept(cluster, joined.Width(), tabWidth)
			return "", true
		}
	}

	return text, false
}

// accept records one grapheme cluster. A tab advances to the next tab stop
// rather than a fixed width, so it must see the width accumulated so far.
func (w *lineWidth) accept(cluster string, width, tabWidth int) {
	if cluster == "\t" {
		width = tabWidth - w.width%tabWidth
	}
	w.width = addWidth(w.width, width)
	w.tail, w.tailWidth = cluster, width
}

// addWidth adds two non-negative widths and panics on overflow, which doc.go
// classifies as a configuration error rather than a layout outcome.
func addWidth(left, right int) int {
	if right > math.MaxInt-left {
		panic("document: layout width overflows int")
	}
	return left + right
}

// normalized substitutes the documented defaults for zero fields.
func (o Options) normalized() Options {
	if o.PrintWidth < 0 || o.IndentWidth < 0 || o.TabWidth < 0 {
		panic("document: negative layout option")
	}

	if o.PrintWidth == 0 {
		o.PrintWidth = defaultPrintWidth
	}
	if o.IndentWidth == 0 {
		o.IndentWidth = defaultIndentWidth
	}
	if o.TabWidth == 0 {
		o.TabWidth = defaultTabWidth
	}
	return o
}
