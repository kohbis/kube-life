package command

import (
	"math/rand"
	"testing"

	"kube-life/internal/sim"
	"kube-life/internal/state"
)

func TestReplicasClamp(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	Exec("scale --replicas=999", c, rt)
	if !c.Deploy.Enabled {
		t.Fatal("Deployment should be enabled")
	}
	ar := c.RSByRevision(c.Deploy.ActiveRevision)
	if ar == nil || ar.Desired != 16 {
		t.Fatalf("active RS desired want 16, got %#v", ar)
	}
	if sim.EffectiveDesired(c) != 16 {
		t.Fatalf("effective want 16")
	}
}

func TestTaintRequiresEffect(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	res := Exec("taint nodes 0", c, rt)
	if len(res.Logs) == 0 {
		t.Fatal("expected error log")
	}
	if c.Nodes[0].Tainted {
		t.Fatal("node should not be tainted without effect")
	}
}

func TestTaintNodesNoSchedule(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	Exec("taint nodes 0 key=value:NoSchedule", c, rt)
	if !c.Nodes[0].Tainted || c.Nodes[0].TaintType != state.TaintNoSchedule {
		t.Fatalf("expected NoSchedule taint, got tainted=%v type=%s", c.Nodes[0].Tainted, c.Nodes[0].TaintType)
	}
}

func TestSpeedDefault(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{TickMS: 1234}
	res := Exec("sim speed default", c, rt)
	if res.TickMS == nil {
		t.Fatal("expected TickMS to be set")
	}
	if *res.TickMS != DefaultTickMS {
		t.Fatalf("want %d, got %d", DefaultTickMS, *res.TickMS)
	}
}

func TestSimResetSetsFlag(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	res := Exec("sim reset", c, rt)
	if !res.Reset {
		t.Fatal("expected Reset flag")
	}
}

func TestSimGoLPauseResume(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	res := Exec("sim gol pause", c, rt)
	if res.SetGoLPaused == nil || *res.SetGoLPaused != true {
		t.Fatalf("expected SetGoLPaused=true, got %#v", res.SetGoLPaused)
	}
	res = Exec("sim gol resume", c, rt)
	if res.SetGoLPaused == nil || *res.SetGoLPaused != false {
		t.Fatalf("expected SetGoLPaused=false, got %#v", res.SetGoLPaused)
	}
}

func TestRolloutRestartStartsRolloutWithoutImageChange(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	Exec("scale --replicas=5", c, rt)
	beforeImg := c.Deploy.Image

	Exec("rollout restart", c, rt)
	if !c.Deploy.RolloutInProgress {
		t.Fatal("expected rollout in progress")
	}
	if c.Deploy.Image != beforeImg {
		t.Fatalf("image should be unchanged: %s -> %s", beforeImg, c.Deploy.Image)
	}
	if c.Deploy.OldRevision != 1 || c.Deploy.ActiveRevision != 2 {
		t.Fatalf("unexpected revisions old=%d active=%d", c.Deploy.OldRevision, c.Deploy.ActiveRevision)
	}

	// verify stepwise desired shift
	newR := c.RSByRevision(2)
	oldR := c.RSByRevision(1)
	if newR == nil || oldR == nil {
		t.Fatal("expected old and new RS")
	}
	if newR.Desired != 0 || oldR.Desired != 5 {
		t.Fatalf("initial desired new=%d old=%d", newR.Desired, oldR.Desired)
	}
	rng := rand.New(rand.NewSource(42))
	sim.Reconcile(c, rng)
	if newR.Desired != 1 || oldR.Desired != 4 {
		t.Fatalf("after one rollout step new=%d old=%d", newR.Desired, oldR.Desired)
	}
}

func TestRolloutRestartShiftsTenPercentRoundedUp(t *testing.T) {
	c, _ := state.NewCluster(8, 8, 1, 1)
	rt := &Runtime{}
	Exec("scale --replicas=25", c, rt)
	Exec("rollout restart", c, rt)

	newR := c.RSByRevision(2)
	oldR := c.RSByRevision(1)
	if newR == nil || oldR == nil {
		t.Fatal("expected old and new RS")
	}

	rng := rand.New(rand.NewSource(42))
	sim.Reconcile(c, rng)
	if newR.Desired != 3 || oldR.Desired != 22 {
		t.Fatalf("after one rollout step new=%d old=%d, want new=3 old=22", newR.Desired, oldR.Desired)
	}
}

func TestScaleWithoutResource(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	Exec("scale --replicas=3", c, rt)
	if !c.Deploy.Enabled || c.Deploy.Replicas != 3 {
		t.Fatalf("expected deployment enabled replicas=3, got enabled=%v replicas=%d", c.Deploy.Enabled, c.Deploy.Replicas)
	}
}

func TestDeploymentDisableTurnsOffController(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	rt := &Runtime{}
	Exec("scale --replicas=3", c, rt)
	if !c.Deploy.Enabled {
		t.Fatal("expected deployment enabled")
	}
	Exec("sim deploy disable", c, rt)
	if c.Deploy.Enabled {
		t.Fatal("expected deployment disabled")
	}
	if len(c.RSs) == 0 {
		t.Fatal("expected ReplicaSets to remain for re-enable")
	}
}

func TestSimDeployEnableSetsReplicasFromCurrentAlive(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 1, 1)
	// Make deterministic: set all dead, then 2 alive.
	for i := range c.Grid.Cells {
		c.Grid.Cells[i] = state.Dead
		c.Grid.Rev[i] = -1
	}
	c.Grid.Cells[0] = state.Alive
	c.Grid.Cells[1] = state.Alive
	rt := &Runtime{}
	Exec("sim deploy enable", c, rt)
	if !c.Deploy.Enabled {
		t.Fatal("expected enabled")
	}
	if c.Deploy.Replicas != 2 {
		t.Fatalf("expected replicas=2, got %d", c.Deploy.Replicas)
	}
	ar := c.RSByRevision(c.Deploy.ActiveRevision)
	if ar == nil || ar.Desired != 2 {
		t.Fatalf("expected active RS desired=2, got %#v", ar)
	}
}

func TestDrainNodesParses(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	rt := &Runtime{}
	res := Exec("drain nodes 1", c, rt)
	if res.DrainNodeID == nil || *res.DrainNodeID != 1 {
		t.Fatalf("expected DrainNodeID=1, got %#v", res.DrainNodeID)
	}
}

func TestUncordonNodesClearsCordoned(t *testing.T) {
	c, _ := state.NewCluster(4, 4, 2, 1)
	n := c.NodeByID(1)
	n.Cordoned = true
	rt := &Runtime{}
	Exec("uncordon nodes 1", c, rt)
	if n.Cordoned {
		t.Fatal("expected cordon cleared")
	}
}
