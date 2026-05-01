package state

// Node owns a fixed set of grid cell indices (scheduler / taint region).
type Node struct {
	ID int

	Cells []int // grid indices (row-major), contiguous partition

	Tainted   bool
	TaintType string // "", "NoSchedule", "NoExecute"

	// Draining marks a node as being drained (cordon + evict) by the toy drain command.
	// It is used for both rendering (gray region) and scheduling decisions.
	Draining bool

	// Cordoned blocks new scheduling onto this node (like kubectl cordon).
	// Drain sets this to true and it stays until uncordon.
	Cordoned bool
}

const (
	TaintNoSchedule = "NoSchedule"
	TaintNoExecute  = "NoExecute"
)

// AliveOnNode counts Alive cells among this node's Cells.
func (n *Node) AliveOnNode(g *Grid) int {
	c := 0
	for _, idx := range n.Cells {
		if idx >= 0 && idx < len(g.Cells) && g.Cells[idx] == Alive {
			c++
		}
	}
	return c
}
