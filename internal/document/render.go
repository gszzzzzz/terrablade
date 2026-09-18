package document

import (
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

// Render returns a deterministic layout. It emits LF newlines only when asked
// by the document and does not append a final newline or trim Text whitespace.
// Automatic indentation is delayed until text, so empty lines have no padding.
func Render(doc Doc, options Options) string {
	options = options.normalized()
	var output strings.Builder
	var column lineWidth
	pending := 0
	stack := []command{{doc: doc}}
	var probe []command
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		n := current.doc.node
		if n == nil {
			continue
		}
		switch n.kind {
		case textKind:
			if pending > 0 {
				output.WriteString(strings.Repeat(" ", pending))
				column.columns = pending
				pending = 0
			}
			output.WriteString(n.text)
			column.append(n.text, options.TabWidth)
		case lineKind, softLineKind, hardLineKind, literalLineKind:
			if current.flat && (n.kind == lineKind || n.kind == softLineKind) {
				if n.kind == lineKind {
					if pending > 0 {
						output.WriteString(strings.Repeat(" ", pending))
						column.columns = pending
						pending = 0
					}
					output.WriteByte(' ')
					column.append(" ", options.TabWidth)
				}
				continue
			}
			output.WriteByte('\n')
			column = lineWidth{}
			pending = current.indent
			if n.kind == literalLineKind {
				pending = 0
			}
		case groupKind:
			if !current.flat && !n.forceBreak {
				candidate := command{doc: n.children[0], indent: current.indent, flat: true}
				start := column
				if pending > 0 {
					start.columns = pending
				}
				current.flat, probe = fits(candidate, stack, start, options, probe)
			}
			current.doc = n.children[0]
			stack = append(stack, current)
		default:
			stack = expand(stack, current, options.IndentWidth)
		}
	}
	return output.String()
}

type command struct {
	doc    Doc
	indent int
	flat   bool
}

// A probe includes the continuation, not just the candidate group. Otherwise a
// closing delimiter or following operator could overflow a line that "fits".
// It stops at the first physical newline; text beyond it uses a fresh width.
// The continuation is borrowed read-only. Copying the render stack for every
// group would make even a flat list of independent groups quadratic.
func fits(candidate command, continuation []command, column lineWidth, options Options, scratch []command) (bool, []command) {
	stack := append(scratch[:0], candidate)
	next := len(continuation) - 1
	for len(stack) > 0 || next >= 0 {
		if column.columns > options.PrintWidth {
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
			column.append(n.text, options.TabWidth)
		case lineKind, softLineKind:
			if !current.flat {
				return true, stack[:0]
			}
			if n.kind == lineKind {
				column.append(" ", options.TabWidth)
			}
		case hardLineKind, literalLineKind:
			return true, stack[:0]
		case groupKind:
			// Candidate descendants inherit flat mode. An undecided sibling
			// keeps the continuation's broken mode, so its break opportunity
			// can end this line instead of forcing an earlier group to break.
			current.doc = n.children[0]
			stack = append(stack, current)
		default:
			stack = expand(stack, current, options.IndentWidth)
		}
	}
	return column.columns <= options.PrintWidth, stack[:0]
}

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
	}
	return stack
}

// Retain the last grapheme because the next Text may extend it, e.g. separate
// Text("👩") and Text("\u200d💻"). Measuring each node independently is incorrect.
// A copy is an independent probe: the retained string is immutable.
type lineWidth struct {
	columns   int
	tail      string
	tailWidth int
}

func (w *lineWidth) append(text string, tabWidth int) {
	if text == "" {
		return
	}
	// Adjacent ASCII code points always break here (Text cannot contain LF).
	// This is the common path for tokens, spaces, and punctuation: no copying.
	if w.tail != "" && !(w.tail[len(w.tail)-1] < utf8.RuneSelf && text[0] < utf8.RuneSelf) {
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
				w.columns -= w.tailWidth
				w.accept(cluster, joined.Width(), tabWidth)
				text = text[len(cluster)-tailBytes:]
				break
			}
			if len(boundary)-tailBytes == len(text) {
				w.columns -= w.tailWidth
				w.accept(cluster, joined.Width(), tabWidth)
				return
			}
		}
	}
	graphemes := (displaywidth.Options{}).StringGraphemes(text)
	for graphemes.Next() {
		w.accept(graphemes.Value(), graphemes.Width(), tabWidth)
	}
}

func (w *lineWidth) accept(cluster string, width, tabWidth int) {
	if cluster == "\t" {
		width = tabWidth - w.columns%tabWidth
	}
	w.columns = addWidth(w.columns, width)
	w.tail, w.tailWidth = cluster, width
}

func addWidth(left, right int) int {
	if right > int(^uint(0)>>1)-left {
		panic("document: layout width overflows int")
	}
	return left + right
}

func (o Options) normalized() Options {
	if o.PrintWidth < 0 || o.IndentWidth < 0 || o.TabWidth < 0 {
		panic("document: negative layout option")
	}
	if o.PrintWidth == 0 {
		o.PrintWidth = 80
	}
	if o.IndentWidth == 0 {
		o.IndentWidth = 2
	}
	if o.TabWidth == 0 {
		o.TabWidth = 8
	}
	return o
}
