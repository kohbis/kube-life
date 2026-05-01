package sim

import (
	"testing"

	"kube-life/internal/state"
)

// Horizontal blinker on row y=1 of 5x5: .#.#. middle row
func TestGameOfLifeStepBlinker(t *testing.T) {
	g := state.NewGrid(5, 5)
	g.Set(1, 2, state.Alive)
	g.Set(2, 2, state.Alive)
	g.Set(3, 2, state.Alive)
	c := &state.Cluster{
		Grid:   g,
		Deploy: state.Deployment{ActiveRevision: 1},
	}
	GameOfLifeStep(c)
	g = c.Grid
	// vertical blinker at x=2
	if g.Get(2, 1) != state.Alive || g.Get(2, 2) != state.Alive || g.Get(2, 3) != state.Alive {
		t.Fatalf("expected vertical blinker, got row1=%v row2=%v row3=%v", g.Get(2, 1), g.Get(2, 2), g.Get(2, 3))
	}
	if g.Get(1, 2) == state.Alive || g.Get(3, 2) == state.Alive {
		t.Fatal("horizontal arms should be dead")
	}
}

func TestGameOfLifeStepDoesNotApplyOnDrainingNode(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	// Clear grid.
	for i := range c.Grid.Cells {
		c.Grid.Cells[i] = state.Dead
		c.Grid.Rev[i] = -1
	}
	// Mark node 0 draining.
	n0 := c.NodeByID(0)
	n0.Draining = true

	// Pick three cells on node 0 that form a blinker; normally it would change.
	var idxs []int
	for i, owner := range c.CellOwner {
		if owner == 0 {
			idxs = append(idxs, i)
		}
		if len(idxs) == 3 {
			break
		}
	}
	if len(idxs) != 3 {
		t.Fatal("expected at least 3 cells on node 0")
	}
	for _, idx := range idxs {
		c.Grid.Cells[idx] = state.Alive
		c.Grid.Rev[idx] = 1
	}
	before := append([]state.Cell(nil), c.Grid.Cells...)
	beforeRev := append([]int(nil), c.Grid.Rev...)

	GameOfLifeStep(c)

	for _, idx := range idxs {
		if c.Grid.Cells[idx] != before[idx] || c.Grid.Rev[idx] != beforeRev[idx] {
			t.Fatalf("expected draining node cell to be frozen at idx=%d", idx)
		}
	}
}

func TestGameOfLifeStepDoesNotApplyOnCordonedNode(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	// Clear grid.
	for i := range c.Grid.Cells {
		c.Grid.Cells[i] = state.Dead
		c.Grid.Rev[i] = -1
	}
	// Mark node 0 cordoned.
	n0 := c.NodeByID(0)
	n0.Cordoned = true

	// Place a few alive cells on node 0.
	var idxs []int
	for i, owner := range c.CellOwner {
		if owner == 0 {
			idxs = append(idxs, i)
		}
		if len(idxs) == 3 {
			break
		}
	}
	if len(idxs) != 3 {
		t.Fatal("expected at least 3 cells on node 0")
	}
	for _, idx := range idxs {
		c.Grid.Cells[idx] = state.Alive
		c.Grid.Rev[idx] = 1
	}
	before := append([]state.Cell(nil), c.Grid.Cells...)
	beforeRev := append([]int(nil), c.Grid.Rev...)

	GameOfLifeStep(c)

	for _, idx := range idxs {
		if c.Grid.Cells[idx] != before[idx] || c.Grid.Rev[idx] != beforeRev[idx] {
			t.Fatalf("expected cordoned node cell to be frozen at idx=%d", idx)
		}
	}
}
