package sim

import "kube-life/internal/state"

// countNeighborsMoore8 counts alive Moore neighbors (8), out-of-bounds = Dead.
func countNeighborsMoore8(g *state.Grid, x, y int) int {
	dirs := [8][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}}
	n := 0
	for _, d := range dirs {
		if g.Get(x+d[0], y+d[1]) == state.Alive {
			n++
		}
	}
	return n
}

func neighborRevisionMajority(g *state.Grid, x, y int, fallback int) int {
	dirs := [8][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}}
	counts := map[int]int{}
	for _, d := range dirs {
		nx, ny := x+d[0], y+d[1]
		if !g.InBounds(nx, ny) {
			continue
		}
		idx := g.Index(nx, ny)
		if g.Cells[idx] != state.Alive {
			continue
		}
		rev := -1
		if idx < len(g.Rev) {
			rev = g.Rev[idx]
		}
		if rev < 0 {
			continue
		}
		counts[rev]++
	}
	if len(counts) == 0 {
		return fallback
	}
	bestRev := -1
	bestCnt := -1
	for r, cnt := range counts {
		if cnt > bestCnt || (cnt == bestCnt && (bestRev < 0 || r < bestRev)) {
			bestCnt = cnt
			bestRev = r
		}
	}
	return bestRev
}

// GameOfLifeStep applies one Conway generation with fixed dead boundary (no torus).
// Alive cells keep their revision when surviving; new births get majority neighbor revision
// (tie: smallest revision), or ActiveDeploymentRevision if no neighbor revisions.
func GameOfLifeStep(c *state.Cluster) {
	if c == nil || c.Grid == nil || len(c.Grid.Cells) == 0 {
		return
	}
	g := c.Grid
	if len(g.Rev) != len(g.Cells) {
		g.Rev = make([]int, len(g.Cells))
		for i := range g.Rev {
			g.Rev[i] = -1
		}
	}
	fallback := c.ActiveDeploymentRevision()
	next := make([]state.Cell, len(g.Cells))
	nextRev := make([]int, len(g.Cells))
	for i := range nextRev {
		nextRev[i] = -1
	}
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			idx := g.Index(x, y)
			// If the owning node is draining/cordoned, freeze this cell (GoL does not apply).
			if idx >= 0 && idx < len(c.CellOwner) {
				owner := c.CellOwner[idx]
				if owner >= 0 && owner < len(c.Nodes) && (c.Nodes[owner].Draining || c.Nodes[owner].Cordoned) {
					next[idx] = g.Cells[idx]
					if idx < len(g.Rev) {
						nextRev[idx] = g.Rev[idx]
					}
					continue
				}
			}
			neighbors := countNeighborsMoore8(g, x, y)
			cur := g.Cells[idx]
			curRev := -1
			if idx < len(g.Rev) {
				curRev = g.Rev[idx]
			}
			switch cur {
			case state.Alive:
				if neighbors == 2 || neighbors == 3 {
					next[idx] = state.Alive
					if curRev >= 0 {
						nextRev[idx] = curRev
					} else {
						nextRev[idx] = fallback
					}
				} else {
					next[idx] = state.Dead
					nextRev[idx] = -1
				}
			default:
				if neighbors == 3 {
					next[idx] = state.Alive
					nextRev[idx] = neighborRevisionMajority(g, x, y, fallback)
				} else {
					next[idx] = state.Dead
					nextRev[idx] = -1
				}
			}
		}
	}
	copy(g.Cells, next)
	copy(g.Rev, nextRev)
}
