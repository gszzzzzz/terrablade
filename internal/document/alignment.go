package document

import (
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// renderedCell is one Cell as it appeared in the rendered output, before
// alignment padding is inserted.
type renderedCell struct {
	// column is the alignment column the cell was declared with.
	column uint8
	// position is the byte offset in the output where the cell's content
	// starts; padding is inserted there. rowStart is the byte offset of the
	// row the cell starts on, so output[rowStart:position] is its prefix.
	position, rowStart int
	// firstRow and lastRow are the structural rows the cell's content spans.
	// They differ only when the cell contains an ordinary line break.
	firstRow, lastRow int
	// pending is the indentation still owed to the row at the cell's start.
	// The renderer has not written it, so the prefix does not contain it.
	pending int
	// padding is the number of spaces alignment inserts at position.
	padding int
}

// alignCells inserts alignment padding into output. Layout runs only once:
// inserting alignment spaces cannot change an earlier line-break decision or
// oscillate between two different alignment chains. Each column scans its
// own cells, and prefix measurement scans each rendered row once per
// populated column. The finite column range bounds that work.
//
// Columns are processed in ascending order. A cell's prefix width includes
// the padding that lower columns add on the same row, so every lower column
// must be fully padded before a higher column is measured.
func alignCells(output string, cells []renderedCell) string {
	if len(cells) == 0 {
		return output
	}

	columns := bucketCells(cells)
	rowPadding := make(map[int]int)
	for _, indices := range columns {
		padChains(output, cells, indices, rowPadding)
	}

	return spliceCells(output, cells)
}

// bucketCells groups cell indices by column, in output order. Indexing by the
// uint8 column value covers every possible column and yields them in
// ascending order, which alignCells relies on.
//
// Only the first cell in each column on a row participates, as Cell documents:
// a nested cell of the same column on the same row is dropped, so a row has at
// most one padding point per column. Later nested cells still participate on
// their own starting rows. cells is in output order, so the last bucketed
// index is always the previous cell of that column.
func bucketCells(cells []renderedCell) [256][]int {
	var columns [256][]int
	for i, cell := range cells {
		indices := columns[cell.column]
		if len(indices) > 0 && cells[indices[len(indices)-1]].firstRow == cell.firstRow {
			continue
		}
		columns[cell.column] = append(indices, i)
	}
	return columns
}

// padChains sets padding for one column's cells. A chain is a run of cells on
// consecutive rows; each chain is padded to its own widest prefix. A cell that
// spans rows is ineligible, as Cell documents: it ends the chain before it,
// receives no padding itself, and the next chain starts after it. rowPadding
// accumulates, per row, the padding chosen by every column so far; it is read
// here for lower columns and updated for this one.
func padChains(output string, cells []renderedCell, indices []int, rowPadding map[int]int) {
	if len(indices) == 0 {
		return
	}

	start, maximum := 0, 0
	widths := make([]int, len(indices))
	closeChain := func(end int) {
		for j := start; j < end; j++ {
			cell := &cells[indices[j]]
			cell.padding = maximum - widths[j]
			rowPadding[cell.firstRow] = addWidth(rowPadding[cell.firstRow], cell.padding)
		}
		maximum = 0
	}

	for i, index := range indices {
		cell := cells[index]
		if cell.firstRow != cell.lastRow {
			closeChain(i)
			start = i + 1
			continue
		}
		if i > start && cell.firstRow != cells[indices[i-1]].firstRow+1 {
			closeChain(i)
			start = i
		}

		widths[i] = cellWidth(output, cell, rowPadding)
		maximum = max(maximum, widths[i])
	}
	closeChain(len(indices))
}

// cellWidth measures a cell's prefix in grapheme clusters. The rendered prefix
// alone would undercount: padding chosen by lower columns on this row has not
// been spliced in yet, and indentation owed at the cell's start has not been
// written. Both occupy one cluster per space.
func cellWidth(output string, cell renderedCell, rowPadding map[int]int) int {
	width := addWidth(rowPadding[cell.firstRow], cell.pending)

	clusters := graphemes.FromString(output[cell.rowStart:cell.position])
	for clusters.Next() {
		width = addWidth(width, 1)
	}
	return width
}

// spliceCells rebuilds output with each cell's padding inserted at its
// position. cells is in output order, so a single forward pass suffices.
func spliceCells(output string, cells []renderedCell) string {
	var aligned strings.Builder
	previous := 0
	changed := false

	for _, cell := range cells {
		if cell.padding == 0 {
			continue
		}
		aligned.WriteString(output[previous:cell.position])
		aligned.WriteString(strings.Repeat(" ", cell.padding))
		previous = cell.position
		changed = true
	}

	if !changed {
		return output
	}
	aligned.WriteString(output[previous:])
	return aligned.String()
}
