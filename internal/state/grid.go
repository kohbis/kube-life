package state

// BirthSource indicates why a cell became Alive on the current tick.
// It is a transient marker cleared each tick by the TUI.
type BirthSource uint8

const (
	BirthNone       BirthSource = 0
	BirthGoL        BirthSource = 1
	BirthDeployment BirthSource = 2
	BirthDrain      BirthSource = 3
)

// Grid is a width×height row-major cell array (index = y*Width + x).
// Rev is parallel to Cells: revision id for Alive cells; -1 when Dead or no pod.
type Grid struct {
	Width, Height int
	Cells         []Cell
	Rev           []int
	// Birth is parallel to Cells and marks newly born cells on the current tick.
	Birth []BirthSource
}

func NewGrid(width, height int) *Grid {
	n := width * height
	rev := make([]int, n)
	for i := range rev {
		rev[i] = -1
	}
	return &Grid{
		Width:  width,
		Height: height,
		Cells:  make([]Cell, n),
		Rev:    rev,
		Birth:  make([]BirthSource, n),
	}
}

func (g *Grid) Index(x, y int) int {
	return y*g.Width + x
}

func (g *Grid) InBounds(x, y int) bool {
	return x >= 0 && x < g.Width && y >= 0 && y < g.Height
}

func (g *Grid) Get(x, y int) Cell {
	if !g.InBounds(x, y) {
		return Dead
	}
	return g.Cells[g.Index(x, y)]
}

func (g *Grid) Set(x, y int, c Cell) {
	if !g.InBounds(x, y) {
		return
	}
	idx := g.Index(x, y)
	g.Cells[idx] = c
	if len(g.Rev) != len(g.Cells) {
		g.Rev = make([]int, len(g.Cells))
		for i := range g.Rev {
			g.Rev[i] = -1
		}
	}
	if len(g.Birth) != len(g.Cells) {
		g.Birth = make([]BirthSource, len(g.Cells))
	}
	if c == Dead {
		g.Rev[idx] = -1
		g.Birth[idx] = BirthNone
		return
	}
	// Alive: tests / manual Set use revision 0 unless already set
	if g.Rev[idx] < 0 {
		g.Rev[idx] = 0
	}
}

func (g *Grid) ClearBirthMarkers() {
	if g == nil {
		return
	}
	for i := range g.Birth {
		g.Birth[i] = BirthNone
	}
}

func (g *Grid) AliveCount() int {
	n := 0
	for _, c := range g.Cells {
		if c == Alive {
			n++
		}
	}
	return n
}
