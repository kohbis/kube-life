package sim

import (
	"fmt"
	"math/rand"
	"strings"

	"kube-life/internal/state"
)

// DeploymentControlCycle is the number of sim ticks per reconcile cadence:
// phase 0 = idle (no controller mutations), 1 = observe (log only), 2 = apply (rollout + scale).
const DeploymentControlCycle = 3

// EffectiveDesired is min(Deployment.Replicas, MaxPods) while Deployment is enabled.
func EffectiveDesired(c *state.Cluster) int {
	if c == nil {
		return 0
	}
	if !c.Deploy.Enabled {
		return 0
	}
	d := c.Deploy.Replicas
	maxp := c.MaxPods()
	if d > maxp {
		return maxp
	}
	return d
}

// maxScaleUpAttempts returns the upper bound on random cell-pick retries
// during scale-up. Proportional to grid size with a floor of 100, so a
// nearly full grid still terminates instead of looping indefinitely.
func maxScaleUpAttempts(total int) int {
	k := 4 * total
	if k < 100 {
		return 100
	}
	return k
}

func rolloutStepSize(effectiveDesired int) int {
	if effectiveDesired <= 0 {
		return 1
	}
	return (effectiveDesired + 9) / 10
}

// rolloutStep advances one step of a rolling update (new RS up, old RS down).
// Each apply tick shifts 10% of the effective desired count, rounded up, with a minimum of 1.
func rolloutStep(c *state.Cluster) {
	d := &c.Deploy
	if !d.RolloutInProgress || d.OldRevision <= 0 {
		return
	}
	oldR := c.RSByRevision(d.OldRevision)
	newR := c.RSByRevision(d.ActiveRevision)
	if oldR == nil || newR == nil {
		return
	}
	eff := EffectiveDesired(c)
	step := rolloutStepSize(eff)
	if newR.Desired < eff {
		newR.Desired += step
		if newR.Desired > eff {
			newR.Desired = eff
		}
	}
	if oldR.Desired > 0 {
		oldR.Desired -= step
		if oldR.Desired < 0 {
			oldR.Desired = 0
		}
	}
	if oldR.Desired == 0 && c.CountPodsByRevision(oldR.Revision) == 0 {
		d.RolloutInProgress = false
		d.OldRevision = 0
		removeRSByRevision(c, oldR.Revision)
	}
}

func removeRSByRevision(c *state.Cluster, rev int) {
	out := c.RSs[:0]
	for _, rs := range c.RSs {
		if rs.Revision != rev {
			out = append(out, rs)
		}
	}
	c.RSs = out
}

// canReplaceOnNode reports whether a cell on this node can be used for an in-place replace
// (old revision -> new revision) during rollout.
func canReplaceOnNode(n *state.Node) bool {
	if n == nil {
		return false
	}
	if n.Draining || n.Cordoned {
		return false
	}
	if n.Tainted && (n.TaintType == state.TaintNoSchedule || n.TaintType == state.TaintNoExecute) {
		return false
	}
	return true
}

// rolloutReplaceStep performs in-place revision swaps (old->new) to make rollout look like a replacement.
// This keeps the total alive count stable and avoids "kill then random spawn" visuals.
func rolloutReplaceStep(c *state.Cluster, oldRev int, newRev int, rng *rand.Rand) (replaced bool) {
	if c == nil || c.Grid == nil || oldRev <= 0 || newRev <= 0 {
		return false
	}
	// Collect candidates once; small grid so it's fine.
	cands := make([]int, 0)
	for i, cell := range c.Grid.Cells {
		if cell != state.Alive {
			continue
		}
		if i >= len(c.Grid.Rev) || c.Grid.Rev[i] != oldRev {
			continue
		}
		if i < 0 || i >= len(c.CellOwner) {
			continue
		}
		owner := c.CellOwner[i]
		if owner < 0 || owner >= len(c.Nodes) {
			continue
		}
		if !canReplaceOnNode(&c.Nodes[owner]) {
			continue
		}
		cands = append(cands, i)
	}
	if len(cands) == 0 {
		return false
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(1))
	}
	idx := cands[rng.Intn(len(cands))]
	// In-place "replace": keep cell alive, swap revision tag.
	c.Grid.Rev[idx] = newRev
	if idx < len(c.Grid.Birth) {
		c.Grid.Birth[idx] = state.BirthDeployment
	}
	return true
}

// reconcileOneRS scales one ReplicaSet toward rs.Desired for its revision.
func reconcileOneRS(c *state.Cluster, rs *state.ReplicaSet, rng *rand.Rand) []string {
	var logs []string
	if c == nil || c.Grid == nil || rs == nil || !rs.Enabled {
		return logs
	}
	eff := rs.Desired
	maxp := c.MaxPods()
	if eff > maxp {
		eff = maxp
	}
	cur := c.CountPodsByRevision(rs.Revision)
	if cur < eff {
		total := len(c.Grid.Cells)
		K := maxScaleUpAttempts(total)
		attempts := 0
		for cur < eff && attempts < K {
			attempts++
			idx := rng.Intn(total)
			if c.Grid.Cells[idx] != state.Dead {
				continue
			}
			if !CanSpawnAt(c, idx, rng) {
				continue
			}
			c.Grid.Cells[idx] = state.Alive
			if idx < len(c.Grid.Rev) {
				c.Grid.Rev[idx] = rs.Revision
			}
			if idx < len(c.Grid.Birth) {
				c.Grid.Birth[idx] = state.BirthDeployment
			}
			cur++
		}
		if cur < eff && attempts >= K {
			logs = append(logs, fmt.Sprintf("scale-up throttled rs=%d after %d attempts (current=%d desired=%d)", rs.Revision, K, cur, eff))
		}
		return logs
	}
	if cur > eff {
		alive := make([]int, 0, cur)
		for i, cell := range c.Grid.Cells {
			if cell != state.Alive {
				continue
			}
			if i >= len(c.Grid.Rev) || c.Grid.Rev[i] != rs.Revision {
				continue
			}
			alive = append(alive, i)
		}
		rng.Shuffle(len(alive), func(i, j int) { alive[i], alive[j] = alive[j], alive[i] })
		kill := cur - eff
		for i := 0; i < kill && i < len(alive); i++ {
			idx := alive[i]
			c.Grid.Cells[idx] = state.Dead
			if idx < len(c.Grid.Rev) {
				c.Grid.Rev[idx] = -1
			}
		}
	}
	return logs
}

// Reconcile runs deployment rollout step (if any) and reconciles all ReplicaSets.
func Reconcile(c *state.Cluster, rng *rand.Rand) []string {
	var logs []string
	if c == nil || !c.Deploy.Enabled || c.Grid == nil {
		return logs
	}
	rolloutStep(c)

	// During rollout, prefer in-place replacement (old -> new) so it looks like pods swap.
	if c.Deploy.RolloutInProgress && c.Deploy.OldRevision > 0 && c.Deploy.ActiveRevision > 0 {
		oldR := c.RSByRevision(c.Deploy.OldRevision)
		newR := c.RSByRevision(c.Deploy.ActiveRevision)
		if oldR != nil && newR != nil {
			oldCur := c.CountPodsByRevision(oldR.Revision)
			newCur := c.CountPodsByRevision(newR.Revision)
			// Replace until both sides are within desired (bounded by desired deltas per tick).
			for newCur < newR.Desired && oldCur > oldR.Desired {
				if !rolloutReplaceStep(c, oldR.Revision, newR.Revision, rng) {
					break
				}
				oldCur--
				newCur++
			}
		}
	}

	for i := range c.RSs {
		if logs2 := reconcileOneRS(c, &c.RSs[i], rng); len(logs2) > 0 {
			logs = append(logs, logs2...)
		}
	}
	return logs
}

// ReconcileObserve logs desired vs actual after the latest GoL step; it does not mutate the grid or RS desired counts.
func ReconcileObserve(c *state.Cluster) []string {
	if c == nil || !c.Deploy.Enabled || c.Grid == nil {
		return nil
	}
	d := c.Deploy
	cur := c.AliveCount()
	eff := EffectiveDesired(c)
	var b strings.Builder
	fmt.Fprintf(&b, "reconcile observe: alive=%d effective=%d", cur, eff)
	if d.RolloutInProgress && d.OldRevision > 0 {
		fmt.Fprintf(&b, " rollout(oldRSRev=%d newRSRev=%d)", d.OldRevision, d.ActiveRevision)
	}
	var bits []string
	for i := range c.RSs {
		rs := &c.RSs[i]
		if !rs.Enabled {
			continue
		}
		cnt := c.CountPodsByRevision(rs.Revision)
		bits = append(bits, fmt.Sprintf("RSRev%d=%d/%d", rs.Revision, cnt, rs.Desired))
	}
	if len(bits) > 0 {
		b.WriteByte(' ')
		b.WriteString(strings.Join(bits, " "))
	}
	return []string{b.String()}
}

// ReconcileForPhase runs one step of the deployment controller for the given phase (use phase % DeploymentControlCycle).
// Phase 0: no-op. Phase 1: observe only. Phase 2: full Reconcile (rollout + scale).
func ReconcileForPhase(c *state.Cluster, rng *rand.Rand, phase int) []string {
	switch phase % DeploymentControlCycle {
	case 0:
		return nil
	case 1:
		return ReconcileObserve(c)
	default:
		return Reconcile(c, rng)
	}
}
