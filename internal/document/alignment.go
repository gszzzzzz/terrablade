package document

import (
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

type renderedCell struct {
	column             uint8
	position, rowStart int
	firstRow, lastRow  int
	pending, padding   int
}

// Layout runs only once: inserting alignment spaces cannot change an earlier
// line-break decision or oscillate between two different alignment chains.
// Each column scans its own cells, and prefix measurement scans each rendered
// row once per populated column. The finite column range bounds that work.
func alignCells(output string, cells []renderedCell) string {
	if len(cells) == 0 {
		return output
	}
	var columns [256][]int
	for i, cell := range cells {
		indices := columns[cell.column]
		if len(indices) > 0 && cells[indices[len(indices)-1]].firstRow == cell.firstRow {
			continue
		}
		columns[cell.column] = append(columns[cell.column], i)
	}
	rowPadding := make(map[int]int)
	for _, indices := range columns {
		start, maximum := 0, 0
		widths := make([]int, len(indices))
		closeChain := func(end int) {
			for j := start; j < end; j++ {
				cell := &cells[indices[j]]
				cell.padding = maximum - widths[j]
				rowPadding[cell.firstRow] = addWidth(rowPadding[cell.firstRow], cell.padding)
			}
			start, maximum = end, 0
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
			}
			widths[i] = addWidth(rowPadding[cell.firstRow], cell.pending)
			clusters := graphemes.FromString(output[cell.rowStart:cell.position])
			for clusters.Next() {
				widths[i] = addWidth(widths[i], 1)
			}
			maximum = max(maximum, widths[i])
		}
		closeChain(len(indices))
	}
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
