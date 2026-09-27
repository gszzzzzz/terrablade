package document

import (
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// renderedCell is a Cell's place in the unaligned output.
type renderedCell struct {
	column uint8
	// position is the output offset of the cell's content, where padding is
	// inserted; output[rowStart:position] is its prefix.
	position, rowStart int
	// firstRow and lastRow differ only if the content has an ordinary line break.
	firstRow, lastRow int
	// pendingIndent is indentation owed at the cell's start, not yet in output.
	pendingIndent int
	padding       int
}

// alignCells inserts alignment padding into output after layout, so padding
// never changes a line-break decision. Columns are padded in ascending order
// because a cell's prefix includes padding from lower columns on its row.
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

// bucketCells returns cell indices by column, in output order, keeping only a
// row's first cell of each column. It panics if columns decrease within a row,
// because prefix widths assume lower columns come first.
func bucketCells(cells []renderedCell) [256][]int {
	var columns [256][]int
	row, highest := -1, uint8(0)
	for i, cell := range cells {
		switch {
		case cell.firstRow != row:
			row, highest = cell.firstRow, cell.column
		case cell.column < highest:
			panic("document: Cell columns decrease within one row")
		default:
			highest = cell.column
		}

		indices := columns[cell.column]
		if len(indices) > 0 && cells[indices[len(indices)-1]].firstRow == cell.firstRow {
			continue
		}
		columns[cell.column] = append(indices, i)
	}
	return columns
}

// padChains pads one column's cells. A chain is a run of cells on consecutive
// rows, padded to its widest prefix; a cell spanning rows gets no padding and
// splits the chain. rowPadding accumulates the padding added to each row.
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

// cellWidth counts the grapheme clusters in a cell's prefix, including
// lower-column padding and owed indentation that are not yet in output.
func cellWidth(output string, cell renderedCell, rowPadding map[int]int) int {
	width := addWidth(rowPadding[cell.firstRow], cell.pendingIndent)

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
