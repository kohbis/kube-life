package state

import (
	"fmt"
	"math"
	"math/rand"
)

const (
	DefaultInitialDensity = 0.18
	// MaxNodes is the upper bound for -nodes / NewCluster numNodes (beyond this is clamped with a log).
	MaxNodes = 16
)

// Cluster is the in-process virtual cluster (not Kubernetes).
type Cluster struct {
	Grid   *Grid
	Nodes  []Node
	Deploy Deployment
	RSs    []ReplicaSet

	// CellOwner maps each grid cell index to owning node ID (0..len(Nodes)-1). MVP: all cells assigned.
	CellOwner []int
}

// MaxPods is always total grid cells (toy global upper bound for reconcile).
func (c *Cluster) MaxPods() int {
	if c.Grid == nil {
		return 0
	}
	return c.Grid.Width * c.Grid.Height
}

func (c *Cluster) AliveCount() int {
	if c.Grid == nil {
		return 0
	}
	return c.Grid.AliveCount()
}

// partitionSizes splits total into k parts as evenly as possible.
func partitionSizes(total, k int) []int {
	if k <= 0 {
		return []int{}
	}
	out := make([]int, k)
	base := total / k
	rem := total % k
	for i := 0; i < k; i++ {
		out[i] = base
		if i < rem {
			out[i]++
		}
	}
	return out
}

// PartitionGridTiles splits a 2D grid into tile-like rectangular regions.
//
// Layout uses cols=ceil(sqrt(n)), rows=ceil(n/cols), and assigns tiles in row-major order.
// If rows*cols > n, extra tiles are merged into the last node so that all cells are owned.
func PartitionGridTiles(width, height, n int) [][]int {
	if width <= 0 || height <= 0 {
		return [][]int{}
	}
	total := width * height
	if n <= 0 {
		n = 1
	}
	if n > total {
		n = total
	}

	cols := int(math.Ceil(math.Sqrt(float64(n))))
	if cols < 1 {
		cols = 1
	}
	rows := int(math.Ceil(float64(n) / float64(cols)))
	if rows < 1 {
		rows = 1
	}

	colSizes := partitionSizes(width, cols)
	rowSizes := partitionSizes(height, rows)

	colStarts := make([]int, cols)
	cur := 0
	for i, s := range colSizes {
		colStarts[i] = cur
		cur += s
	}
	rowStarts := make([]int, rows)
	cur = 0
	for i, s := range rowSizes {
		rowStarts[i] = cur
		cur += s
	}

	out := make([][]int, n)
	for r := 0; r < rows; r++ {
		y0 := rowStarts[r]
		y1 := y0 + rowSizes[r]
		for c := 0; c < cols; c++ {
			x0 := colStarts[c]
			x1 := x0 + colSizes[c]
			tileIdx := r*cols + c
			nodeID := tileIdx
			if nodeID >= n {
				nodeID = n - 1
			}
			for y := y0; y < y1; y++ {
				base := y * width
				for x := x0; x < x1; x++ {
					out[nodeID] = append(out[nodeID], base+x)
				}
			}
		}
	}
	return out
}

// NewCluster builds grid, nodes, cellOwner, random initial Alive (Bernoulli p), RS off.
func NewCluster(width, height, numNodes int, seed int64) (*Cluster, []string) {
	var logs []string
	if width <= 0 || height <= 0 {
		width, height = 40, 15
		logs = append(logs, "warning: invalid dimensions; using 40x15")
	}
	total := width * height
	n := numNodes
	if n < 1 {
		n = 1
	}
	if n > MaxNodes {
		logs = append(logs, fmt.Sprintf("nodes clamped: requested %d > maxNodes %d, using %d", numNodes, MaxNodes, MaxNodes))
		n = MaxNodes
	}
	if n > total {
		logs = append(logs, fmt.Sprintf("nodes clamped: requested %d > totalCells %d, using %d", numNodes, total, total))
		n = total
	}

	g := NewGrid(width, height)
	rng := rand.New(rand.NewSource(seed))
	for i := range g.Cells {
		if rng.Float64() < DefaultInitialDensity {
			g.Cells[i] = Alive
			if i < len(g.Rev) {
				g.Rev[i] = 1 // initial "pods" tagged rev 1 before Deployment is enabled
			}
		}
	}

	parts := PartitionGridTiles(width, height, n)
	nodes := make([]Node, n)
	owner := make([]int, total)
	for i := 0; i < n; i++ {
		nodes[i] = Node{
			ID:        i,
			Cells:     parts[i],
			Tainted:   false,
			TaintType: "",
			Draining:  false,
			Cordoned:  false,
		}
		for _, idx := range parts[i] {
			owner[idx] = i
		}
	}

	c := &Cluster{
		Grid:      g,
		Nodes:     nodes,
		Deploy:    Deployment{Image: DefaultWorkloadImage},
		RSs:       nil,
		CellOwner: owner,
	}
	return c, logs
}

func (c *Cluster) NodeByID(id int) *Node {
	if id < 0 || id >= len(c.Nodes) {
		return nil
	}
	return &c.Nodes[id]
}

// ClearAliveOnNode sets all cells in node's region to Dead (NoExecute).
func (c *Cluster) ClearAliveOnNode(nodeID int) {
	n := c.NodeByID(nodeID)
	if n == nil {
		return
	}
	for _, idx := range n.Cells {
		if idx >= 0 && idx < len(c.Grid.Cells) {
			c.Grid.Cells[idx] = Dead
			if idx < len(c.Grid.Rev) {
				c.Grid.Rev[idx] = -1
			}
			if idx < len(c.Grid.Birth) {
				c.Grid.Birth[idx] = BirthNone
			}
		}
	}
}

// RSByRevision returns a pointer to the RS with the given revision, or nil.
func (c *Cluster) RSByRevision(rev int) *ReplicaSet {
	for i := range c.RSs {
		if c.RSs[i].Revision == rev {
			return &c.RSs[i]
		}
	}
	return nil
}

// NextRevision returns a new monotonically increasing revision id.
func (c *Cluster) NextRevision() int {
	max := 0
	for _, rs := range c.RSs {
		if rs.Revision > max {
			max = rs.Revision
		}
	}
	return max + 1
}

// CountPodsByRevision counts Alive cells whose Rev matches.
func (c *Cluster) CountPodsByRevision(rev int) int {
	if c.Grid == nil {
		return 0
	}
	n := 0
	for i, cell := range c.Grid.Cells {
		if cell != Alive {
			continue
		}
		if i < len(c.Grid.Rev) && c.Grid.Rev[i] == rev {
			n++
		}
	}
	return n
}

// ActiveDeploymentRevision returns the revision used for GoL birth fallback.
func (c *Cluster) ActiveDeploymentRevision() int {
	if c.Deploy.ActiveRevision > 0 {
		return c.Deploy.ActiveRevision
	}
	return 1
}
