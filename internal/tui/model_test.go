package tui

import (
	"math/rand"
	"testing"

	"kube-life/internal/command"
	"kube-life/internal/state"
)

func TestDrainBlockedDoesNotMarkDone(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	// Clear grid and put one pod on node 0.
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
		t.Fatal("expected a cell on node 0")
	}
	c.Grid.Cells[src] = state.Alive
	c.Grid.Rev[src] = 1

	// Make all other nodes unschedulable so drain blocks.
	n1 := c.NodeByID(1)
	n1.Tainted = true
	n1.TaintType = state.TaintNoSchedule

	rt := &command.Runtime{Paused: false, GoLPaused: true, TickMS: 1}
	m := NewModel(c, rt, 1, rand.New(rand.NewSource(1)), 80, nil, 4, 4, 2, 1)
	m.drainActive = true
	m.drainNodeID = 0
	m.drainQueue = []int{src}

	// One tick should attempt drain and get blocked.
	m2, _ := m.Update(tickMsg{})
	mm := m2.(*Model)

	if mm.drainActive {
		t.Fatal("expected drain to stop on blocked")
	}
	if mm.cluster.AliveCount() != 1 {
		t.Fatalf("expected pod to remain, got alive=%d", mm.cluster.AliveCount())
	}
	if len(mm.lastEvent) < len("drain blocked") || mm.lastEvent[:len("drain blocked")] != "drain blocked" {
		t.Fatalf("expected drain blocked event, got %q", mm.lastEvent)
	}
}
