package sim

import (
	"math/rand"

	"kube-life/internal/state"
)

// CanSpawnAt reports whether reconcile may flip cell idx from Dead to Alive.
func CanSpawnAt(c *state.Cluster, idx int, rng *rand.Rand) bool {
	if c == nil || c.Grid == nil || idx < 0 || idx >= len(c.Grid.Cells) {
		return false
	}
	if c.Grid.Cells[idx] != state.Dead {
		return false
	}
	owner := c.CellOwner[idx]
	if owner < 0 || owner >= len(c.Nodes) {
		return false
	}
	n := &c.Nodes[owner]
	if n.Draining || n.Cordoned {
		return false
	}
	if n.Tainted && (n.TaintType == state.TaintNoSchedule || n.TaintType == state.TaintNoExecute) {
		return false
	}
	return true
}
