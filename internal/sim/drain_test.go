package sim

import (
	"math/rand"
	"testing"

	"kube-life/internal/state"
)

func TestDrainNodeMovesPodsOffNode(t *testing.T) {
	c, _ := state.NewCluster(6, 2, 2, 1)
	// Ensure there are a few pods on node 0.
	for i := range c.Grid.Cells {
		c.Grid.Cells[i] = state.Dead
		c.Grid.Rev[i] = -1
	}
	for i, owner := range c.CellOwner {
		if owner == 0 {
			c.Grid.Cells[i] = state.Alive
			c.Grid.Rev[i] = 3
			break
		}
	}

	rng := rand.New(rand.NewSource(42))
	queue := DrainList(c, 0)
	if len(queue) == 0 {
		t.Fatal("expected initial pod on node 0")
	}
	moved, blocked := DrainStep(c, rng, 0, queue[0])
	if blocked {
		t.Fatal("expected move to succeed")
	}
	if !moved {
		t.Fatal("expected moved")
	}
	// Destination should be marked as drain relocation (one-tick marker).
	var sawDrain bool
	for _, src := range queue {
		_ = src
	}
	for i := range c.Grid.Cells {
		if c.Grid.Cells[i] == state.Alive && i < len(c.Grid.Birth) && c.Grid.Birth[i] == state.BirthDrain {
			sawDrain = true
			break
		}
	}
	if !sawDrain {
		t.Fatal("expected BirthDrain marker on relocated cell")
	}
	for i, owner := range c.CellOwner {
		if owner != 0 {
			continue
		}
		if c.Grid.Cells[i] == state.Alive {
			t.Fatalf("expected no pods remaining on drained node, found alive at idx=%d", i)
		}
	}
	// Ensure at least one pod exists somewhere else.
	if c.AliveCount() == 0 {
		t.Fatal("expected pod to be moved to another node")
	}
}

func TestDrainNodeRespectsNoScheduleOnTargets(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	// Clear and place one pod on node 0.
	for i := range c.Grid.Cells {
		c.Grid.Cells[i] = state.Dead
		c.Grid.Rev[i] = -1
	}
	var src = -1
	for i, owner := range c.CellOwner {
		if owner == 0 {
			src = i
			break
		}
	}
	if src < 0 {
		t.Fatal("expected node 0 cell")
	}
	c.Grid.Cells[src] = state.Alive
	c.Grid.Rev[src] = 1

	// Taint node 1 so nothing can be scheduled there.
	n1 := c.NodeByID(1)
	n1.Tainted = true
	n1.TaintType = state.TaintNoSchedule

	rng := rand.New(rand.NewSource(1))
	queue := DrainList(c, 0)
	if len(queue) == 0 {
		t.Fatal("expected pod on node 0")
	}
	_, blocked := DrainStep(c, rng, 0, queue[0])
	// Since node 1 is blocked, the pod cannot move; it should remain on node 0.
	if !blocked {
		t.Fatal("expected blocked move")
	}
	if c.Grid.Cells[src] != state.Alive {
		t.Fatal("expected pod to remain when no target nodes are schedulable")
	}
}

func TestCanSpawnAtCordonedBlocks(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	// pick a dead cell owned by node 0
	var idx = -1
	for i, owner := range c.CellOwner {
		if owner == 0 {
			c.Grid.Cells[i] = state.Dead
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("expected node 0 cell")
	}
	c.NodeByID(0).Cordoned = true
	rng := rand.New(rand.NewSource(1))
	if CanSpawnAt(c, idx, rng) {
		t.Fatal("expected cordoned node to block spawn")
	}
}
