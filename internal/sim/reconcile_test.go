package sim

import (
	"math/rand"
	"strings"
	"testing"

	"kube-life/internal/state"
)

func TestReconcileOneRSSetsRevision(t *testing.T) {
	c, _ := state.NewCluster(3, 3, 1, 42)
	for i := range c.Grid.Cells {
		c.Grid.Cells[i] = state.Dead
		c.Grid.Rev[i] = -1
		if i < len(c.Grid.Birth) {
			c.Grid.Birth[i] = state.BirthNone
		}
	}
	c.Deploy.Enabled = true
	c.Deploy.Replicas = 1
	c.Deploy.ActiveRevision = 7
	c.RSs = []state.ReplicaSet{{Revision: 7, Image: "x", Desired: 1, Enabled: true}}
	rng := rand.New(rand.NewSource(1))
	Reconcile(c, rng)
	if c.AliveCount() != 1 {
		t.Fatalf("want 1 alive, got %d", c.AliveCount())
	}
	var found int
	for i, cell := range c.Grid.Cells {
		if cell == state.Alive {
			found++
			if c.Grid.Rev[i] != 7 {
				t.Fatalf("want rev 7, got %d at %d", c.Grid.Rev[i], i)
			}
			if i < len(c.Grid.Birth) && c.Grid.Birth[i] != state.BirthDeployment {
				t.Fatalf("want BirthDeployment, got %d at %d", c.Grid.Birth[i], i)
			}
		}
	}
	if found != 1 {
		t.Fatal("expected exactly one alive cell")
	}
}

func TestReconcileObserveReadOnly(t *testing.T) {
	c, _ := state.NewCluster(3, 3, 1, 42)
	c.Deploy.Enabled = true
	c.Deploy.Replicas = 9
	c.Deploy.ActiveRevision = 1
	c.RSs = []state.ReplicaSet{{Revision: 1, Image: "x", Desired: 9, Enabled: true}}
	before := c.AliveCount()
	logs := ReconcileObserve(c)
	if len(logs) != 1 || !strings.Contains(logs[0], "reconcile observe") {
		t.Fatalf("unexpected logs: %v", logs)
	}
	if c.AliveCount() != before {
		t.Fatalf("observe mutated alive: before=%d after=%d", before, c.AliveCount())
	}
}

func TestReconcileForPhaseIdleAndObserveNoMutateRSDesired(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	c.Deploy.Enabled = true
	c.Deploy.Replicas = 5
	c.Deploy.ActiveRevision = 1
	c.RSs = []state.ReplicaSet{{Revision: 1, Image: "x", Desired: 5, Enabled: true}}
	rng := rand.New(rand.NewSource(1))
	oldD := c.RSs[0].Desired
	_ = ReconcileForPhase(c, rng, 0)
	_ = ReconcileForPhase(c, rng, 1)
	if c.RSs[0].Desired != oldD {
		t.Fatal("phase 0/1 should not change RS desired")
	}
}

func TestRolloutReplacesOldWithNewInPlace(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	// Make all cells alive with old revision.
	for i := range c.Grid.Cells {
		c.Grid.Cells[i] = state.Alive
		c.Grid.Rev[i] = 1
	}
	c.Deploy.Enabled = true
	c.Deploy.Replicas = 16
	c.Deploy.ActiveRevision = 2
	c.Deploy.OldRevision = 1
	c.Deploy.RolloutInProgress = true
	c.RSs = []state.ReplicaSet{
		{Revision: 1, Image: "x", Desired: 16, Enabled: true},
		{Revision: 2, Image: "x", Desired: 0, Enabled: true},
	}
	rng := rand.New(rand.NewSource(1))

	// One reconcile (apply tick) should shift desired and then replace old->new without needing dead cells.
	Reconcile(c, rng)

	oldR := c.RSByRevision(1)
	newR := c.RSByRevision(2)
	if oldR == nil || newR == nil {
		t.Fatal("expected both RS")
	}
	// rolloutStepSize for eff=16 is ceil(16/10)=2
	if newR.Desired != 2 || oldR.Desired != 14 {
		t.Fatalf("unexpected desired after step new=%d old=%d", newR.Desired, oldR.Desired)
	}
	if c.CountPodsByRevision(2) != 2 || c.CountPodsByRevision(1) != 14 {
		t.Fatalf("expected in-place swap old=14 new=2, got old=%d new=%d", c.CountPodsByRevision(1), c.CountPodsByRevision(2))
	}
	if c.AliveCount() != 16 {
		t.Fatalf("expected alive to stay 16, got %d", c.AliveCount())
	}
}
