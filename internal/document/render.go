package document

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
)

// Options controls layout. Zero fields use defaults; negative fields panic.
type Options struct {
	PrintWidth  int // Preferred line width in display columns; default 80.
	IndentWidth int // Spaces per indentation level; default 2.
	TabWidth    int // Distance between tab stops; default 8.
}

const (
	defaultPrintWidth  = 80
	defaultIndentWidth = 2
	defaultTabWidth    = 8
)

// Render lays out doc as text. The result is deterministic.
func Render(doc Doc, options Options) string {
	r := renderer{options: options.normalized(), stack: []command{{doc: doc}}}

	for len(r.stack) > 0 {
		last := len(r.stack) - 1
		current := r.stack[last]
		r.stack = r.stack[:last]

		// A cell-end marker pops once the cell's content has rendered, so the
		// current row is the cell's last.
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

// renderer is the mutable state of one Render call.
type renderer struct {
	options Options
	output  strings.Builder
	line    lineWidth
	// pendingIndent is indentation owed to the current line, written before
	// its first text so that empty lines stay unpadded.
	pendingIndent int
	// stack holds pending commands, top last; probeScratch is reused by fits.
	stack, probeScratch []command
	cells               []renderedCell
	// row counts line breaks and rowStart is the current row's output offset.
	// LiteralLine advances neither: literal text stays in one alignment row.
	row, rowStart int
}

// command is pending render work: a Doc with its indentation and mode, or a
// cell-end marker.
type command struct {
	doc    Doc
	indent int
	// flat selects flat lines and IfBreak branches; expanded commands inherit it.
	flat bool
	// cellEnd is the one-based index of the cell whose content ends here, or
	// zero. Pushed below the content, it finds a cell's last row without
	// recursion.
	cellEnd int
}

// flushIndent writes the pending indentation.
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

	// A literal line owes no indentation and does not start an alignment row.
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
		candidate := command{doc: n.child, indent: current.indent, flat: true}

		// Owed indentation counts toward the probe's starting width.
		start := r.line
		if r.pendingIndent > 0 {
			start.width = r.pendingIndent
		}
		current.flat, r.probeScratch = fits(candidate, r.stack, start, r.options, r.probeScratch)
	}

	current.doc = n.child
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

	current.doc = n.child
	r.stack = append(r.stack, current)
}

// fits reports whether candidate, rendered flat from line, fits in the print
// width together with the continuation up to its first line break, so that a
// following delimiter or operator is counted. The continuation is read, not
// copied, to keep probing linear. fits returns scratch emptied for reuse.
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
			// Candidate descendants inherit flat mode. Continuation groups
			// stay broken, so their line breaks can end the probe.
			current.doc = n.child
			stack = append(stack, current)
		default:
			stack = expand(stack, current, options.IndentWidth)
		}
	}

	return line.width <= options.PrintWidth, stack[:0]
}

// expand pushes the children of a concat, indent, ForceFlat, or IfBreak node
// in render order.
func expand(stack []command, current command, indentWidth int) []command {
	n := current.doc.node

	switch n.kind {
	case concatKind:
		for i := len(n.children) - 1; i >= 0; i-- {
			stack = append(stack, command{doc: n.children[i], indent: current.indent, flat: current.flat})
		}
	case indentKind:
		current.indent = addWidth(current.indent, indentWidth)
		current.doc = n.child
		stack = append(stack, current)
	case forceFlatKind:
		current.flat = true
		current.doc = n.child
		stack = append(stack, current)
	case ifBreakKind:
		index := 0
		if current.flat {
			index = 1
		}
		current.doc = n.children[index]
		stack = append(stack, current)
	default:
		// Callers handle every other kind.
		panic("document: unhandled kind")
	}

	return stack
}

// lineWidth measures the display width of an output line across Text nodes.
// It keeps the last grapheme cluster because the next Text may extend it, as
// Text("\u200d💻") extends Text("👩"). Copies are independent.
type lineWidth struct {
	width int
	// tail is the last grapheme cluster and tailWidth its width.
	tail      string
	tailWidth int
}

// measure adds text to the current line's width.
func (w *lineWidth) measure(text string, tabWidth int) {
	if text == "" {
		return
	}

	// Adjacent ASCII characters never join into one cluster (CR LF would,
	// but Text has no LF), so the common case skips joinTail.
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

// joinTail re-measures the grapheme cluster that straddles tail and text,
// copying only that cluster. It returns the unmeasured rest of text and
// whether text was consumed entirely.
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
			// Resume after the joined cluster, even if regional-indicator
			// pairing ends it inside one of text's own clusters.
			w.width -= w.tailWidth
			w.accept(cluster, joined.Width(), tabWidth)
			return text[len(cluster)-tailBytes:], false
		}
		if len(boundary)-tailBytes == len(text) {
			// All of text joins the tail's cluster.
			w.width -= w.tailWidth
			w.accept(cluster, joined.Width(), tabWidth)
			return "", true
		}
	}

	return text, false
}

// accept records one grapheme cluster, expanding a tab to the next tab stop.
func (w *lineWidth) accept(cluster string, width, tabWidth int) {
	if cluster == "\t" {
		width = tabWidth - w.width%tabWidth
	}
	w.width = addWidth(w.width, width)
	w.tail, w.tailWidth = cluster, width
}

// addWidth adds two non-negative widths, panicking on overflow.
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
