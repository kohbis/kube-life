package sim

import (
	"math/rand"

	"kube-life/internal/state"
)

// DrainList returns current alive indices owned by nodeID (snapshot order).
func DrainList(c *state.Cluster, nodeID int) []int {
	if c == nil || c.Grid == nil {
		return nil
	}
	n := c.NodeByID(nodeID)
	if n == nil {
		return nil
	}
	out := make([]int, 0)
	for _, idx := range n.Cells {
		if idx < 0 || idx >= len(c.Grid.Cells) {
			continue
		}
		if c.Grid.Cells[idx] == state.Alive {
			out = append(out, idx)
		}
	}
	return out
}

// DrainStep moves at most one pod (alive cell) from srcIdx (owned by nodeID) to another node.
// Returns moved=true if a move happened; blocked=true if the pod couldn't be moved this step.
func DrainStep(c *state.Cluster, rng *rand.Rand, nodeID int, srcIdx int) (moved bool, blocked bool) {
	if c == nil || c.Grid == nil || rng == nil {
		return false, true
	}
	if srcIdx < 0 || srcIdx >= len(c.Grid.Cells) || c.Grid.Cells[srcIdx] != state.Alive {
		return false, false // nothing to do
	}
	if srcIdx >= len(c.CellOwner) || c.CellOwner[srcIdx] != nodeID {
		return false, true
	}

	rev := -1
	if srcIdx < len(c.Grid.Rev) {
		rev = c.Grid.Rev[srcIdx]
	}
	if rev < 0 {
		rev = c.ActiveDeploymentRevision()
	}

	total := len(c.Grid.Cells)
	K := maxScaleUpAttempts(total)
	dst := -1
	for attempts := 0; attempts < K; attempts++ {
		cand := rng.Intn(total)
		if c.CellOwner[cand] == nodeID {
			continue
		}
		if !CanSpawnAt(c, cand, rng) {
			continue
		}
		dst = cand
		break
	}
	if dst < 0 {
		return false, true
	}

	// Move: source dead, destination alive (same revision).
	c.Grid.Cells[srcIdx] = state.Dead
	if srcIdx < len(c.Grid.Rev) {
		c.Grid.Rev[srcIdx] = -1
	}
	if srcIdx < len(c.Grid.Birth) {
		c.Grid.Birth[srcIdx] = state.BirthNone
	}

	c.Grid.Cells[dst] = state.Alive
	if dst < len(c.Grid.Rev) {
		c.Grid.Rev[dst] = rev
	}
	if dst < len(c.Grid.Birth) {
		c.Grid.Birth[dst] = state.BirthDrain
	}
	return true, false
}
