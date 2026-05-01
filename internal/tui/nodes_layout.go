package tui

import (
	"fmt"
	"math"
	"strings"

	"kube-life/internal/state"
)

// NodeTileLines returns fixed-width lines for one node (for tiling).
func nodeTileLines(n *state.Node, g *state.Grid, tileW int) []string {
	taint := "<none>"
	if n.Tainted {
		if n.TaintType != "" {
			taint = n.TaintType
		} else {
			taint = "tainted"
		}
	}

	// scheduling status tokens
	var st []string
	if n.Draining {
		st = append(st, "draining")
	}
	if n.Cordoned {
		st = append(st, "cordoned")
	}
	status := "ok"
	if len(st) > 0 {
		status = strings.Join(st, ",")
	}
	alive := n.AliveOnNode(g)
	capacity := len(n.Cells)
	line1 := truncate(fmt.Sprintf("node %d", n.ID), tileW)
	line2 := truncate(fmt.Sprintf("taint=%s", taint), tileW)
	line3 := truncate(fmt.Sprintf("sched=%s", status), tileW)
	line4 := truncate(fmt.Sprintf("alive=%d/%d", alive, capacity), tileW)
	lines := []string{pad(line1, tileW), pad(line2, tileW), pad(line3, tileW), pad(line4, tileW)}
	return lines
}

func pad(s string, w int) string {
	if len(s) >= w {
		return s[:w]
	}
	return s + strings.Repeat(" ", w-len(s))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// RenderNodeTiles lays out N nodes in a tiled grid, row-major by node ID.
func RenderNodeTiles(c *state.Cluster, maxTermWidth int) []string {
	if c == nil || len(c.Nodes) == 0 {
		return []string{"(no nodes)"}
	}
	n := len(c.Nodes)
	colsMax := int(math.Ceil(math.Sqrt(float64(n))))
	if colsMax < 1 {
		colsMax = 1
	}
	gap := 2
	sep := " | "

	// Choose cols and tileW to fit the terminal width. Prefer more columns.
	cols := colsMax
	tileW := 18
	if maxTermWidth > 0 {
		bestCols, bestW := 1, 12
		for tryCols := colsMax; tryCols >= 1; tryCols-- {
			tryW := 18
			for tryW > 12 && tryCols*(tryW+gap)+(tryCols-1)*len(sep)-gap > maxTermWidth {
				tryW--
			}
			totalW := tryCols*(tryW+gap) + (tryCols-1)*len(sep) - gap
			if totalW <= maxTermWidth {
				bestCols, bestW = tryCols, tryW
				break
			}
			// If nothing fits, keep the smallest layout (1 col, min width).
		}
		cols, tileW = bestCols, bestW
	}

	rows := int(math.Ceil(float64(n) / float64(cols)))

	tileH := 4
	tiles := make([][]string, n)
	for i := 0; i < n; i++ {
		tiles[i] = nodeTileLines(&c.Nodes[i], c.Grid, tileW)
		for len(tiles[i]) < tileH {
			tiles[i] = append(tiles[i], strings.Repeat(" ", tileW))
		}
	}

	var out []string
	for r := 0; r < rows; r++ {
		for lh := 0; lh < tileH; lh++ {
			var sb strings.Builder
			for col := 0; col < cols; col++ {
				idx := r*cols + col
				if idx >= n {
					sb.WriteString(strings.Repeat(" ", tileW))
				} else {
					sb.WriteString(tiles[idx][lh])
				}
				if col < cols-1 {
					sb.WriteString(strings.Repeat(" ", gap))
					sb.WriteString(sep)
					sb.WriteString(strings.Repeat(" ", gap))
				}
			}
			out = append(out, sb.String())
		}
		// blank line between tile rows so boundaries are obvious
		if r != rows-1 {
			out = append(out, "")
		}
	}
	return out
}
