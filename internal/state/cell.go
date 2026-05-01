package state

// Cell is a grid slot: Dead or Alive (toy "pod" present when Alive).
type Cell byte

const (
	Dead  Cell = 0
	Alive Cell = 1
)
