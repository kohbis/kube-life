package sim

import (
	"math/rand"
	"testing"

	"kube-life/internal/state"
)

func TestCanSpawnAt_NoExecuteBlocks(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	// pick a cell owned by node 0
	var idx int = -1
	for i, owner := range c.CellOwner {
		if owner == 0 {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("expected a cell owned by node 0")
	}
	c.Grid.Cells[idx] = state.Dead

	n0 := c.NodeByID(0)
	n0.Tainted = true
	n0.TaintType = state.TaintNoExecute

	rng := rand.New(rand.NewSource(1))
	if CanSpawnAt(c, idx, rng) {
		t.Fatal("expected NoExecute taint to block spawn")
	}
}
