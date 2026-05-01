package command

import (
	"fmt"
	"strconv"
	"strings"

	"kube-life/internal/state"
)

const DefaultTickMS = 500

// Runtime is TUI-owned sim pacing (not part of Cluster).
type Runtime struct {
	Paused    bool
	GoLPaused bool
	TickMS    int
}

// ExecResult is returned from Exec for the TUI to apply.
type ExecResult struct {
	Logs []string

	SetPaused      *bool
	SetGoLPaused   *bool
	TickMS         *int
	Reset          bool
	DrainNodeID    *int
	UncordonNodeID *int
}

func appendLog(res *ExecResult, s string) {
	res.Logs = append(res.Logs, s)
}

// Exec parses and runs one command line against cluster and runtime.
func Exec(line string, c *state.Cluster, rt *Runtime) ExecResult {
	line = strings.TrimSpace(line)
	res := ExecResult{}
	if line == "" {
		return res
	}
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return res
	}
	// Optional "kubectl" prefix (kubectl-like commands are supported).
	if strings.EqualFold(parts[0], "kubectl") {
		parts = parts[1:]
		if len(parts) == 0 {
			appendLog(&res, "error: kubectl requires a subcommand")
			return res
		}
	}
	cmd := strings.ToLower(parts[0])

	switch cmd {
	case "sim":
		if len(parts) < 2 {
			appendLog(&res, "error: sim requires pause|resume|speed|reset|gol|deploy")
			return res
		}
		switch strings.ToLower(parts[1]) {
		case "pause":
			t := true
			res.SetPaused = &t
			appendLog(&res, "paused")
		case "resume":
			f := false
			res.SetPaused = &f
			appendLog(&res, "resumed")
		case "speed":
			if len(parts) < 3 {
				appendLog(&res, "error: sim speed requires <ms> or default")
				return res
			}
			var ms int
			if strings.ToLower(parts[2]) == "default" {
				ms = DefaultTickMS
			} else {
				var err error
				ms, err = strconv.Atoi(parts[2])
				if err != nil || ms < 10 || ms > 60_000 {
					appendLog(&res, "error: sim speed <ms> must be integer 10..60000 (or 'default')")
					return res
				}
			}
			res.TickMS = &ms
			appendLog(&res, fmt.Sprintf("tick interval set to %dms", ms))
		case "reset":
			res.Reset = true
			appendLog(&res, "reset requested")
		case "gol":
			if len(parts) < 3 {
				appendLog(&res, "error: use sim gol pause|resume")
				return res
			}
			switch strings.ToLower(parts[2]) {
			case "pause":
				t := true
				res.SetGoLPaused = &t
				appendLog(&res, "GoL paused (deployment/ops continue)")
			case "resume":
				f := false
				res.SetGoLPaused = &f
				appendLog(&res, "GoL resumed")
			default:
				appendLog(&res, "error: use sim gol pause|resume")
			}
		case "deploy", "deployment":
			// sim deploy enable|disable
			if len(parts) < 3 {
				appendLog(&res, "error: use sim deploy enable|disable")
				return res
			}
			switch strings.ToLower(parts[2]) {
			case "enable":
				applyDeploymentEnableFromCurrentAlive(c, &res)
			case "disable":
				applyDeploymentDisableKeepDesired(c, &res)
			default:
				appendLog(&res, "error: use sim deploy enable|disable")
			}
		default:
			appendLog(&res, "error: sim requires pause|resume|speed|reset|gol|deploy")
		}
	case "scale":
		// kubectl-like (resource optional):
		// - scale --replicas=3
		// - scale deployment/kube-life --replicas=3
		args := parts[1:]
		if len(args) == 0 {
			appendLog(&res, "error: use scale --replicas=<n>")
			return res
		}
		if len(args) > 0 && strings.HasPrefix(strings.ToLower(args[0]), "deployment") {
			args = args[1:]
		}
		rep, ok := parseReplicasFlag(args)
		if !ok {
			appendLog(&res, "error: use --replicas=<n> (or --replicas <n>)")
			return res
		}
		return applyScaleReplicas(rep, c, &res)
	case "rollout":
		if len(parts) < 2 {
			appendLog(&res, "error: use rollout status|restart")
			return res
		}
		switch strings.ToLower(parts[1]) {
		case "status":
			// continue below
		case "restart":
			applyRolloutRestart(c, &res)
			return res
		default:
			appendLog(&res, "error: use rollout status|restart")
			return res
		}
		// Accept (and ignore) an optional resource token, e.g. "deployment/kube-life".
		d := c.Deploy
		if !d.Enabled {
			appendLog(&res, "rollout: deployment off")
			return res
		}
		if d.RolloutInProgress {
			old := c.RSByRevision(d.OldRevision)
			newR := c.RSByRevision(d.ActiveRevision)
			var oldD, newD, oldC, newC int
			if old != nil {
				oldD, oldC = old.Desired, c.CountPodsByRevision(old.Revision)
			}
			if newR != nil {
				newD, newC = newR.Desired, c.CountPodsByRevision(newR.Revision)
			}
			appendLog(&res, fmt.Sprintf("rollout: gen=%d inProgress oldRSRev=%d desired=%d current=%d -> newRSRev=%d desired=%d current=%d",
				d.Generation, d.OldRevision, oldD, oldC, d.ActiveRevision, newD, newC))
			return res
		}
		ar := c.RSByRevision(d.ActiveRevision)
		cur := 0
		if ar != nil {
			cur = c.CountPodsByRevision(ar.Revision)
		}
		appendLog(&res, fmt.Sprintf("rollout: idle gen=%d activeRSRev=%d replicas=%d current=%d image=%s",
			d.Generation, d.ActiveRevision, d.Replicas, cur, d.Image))
	case "drain":
		// kubectl-like: drain nodes <id>
		if len(parts) < 3 || strings.ToLower(parts[1]) != "nodes" {
			appendLog(&res, "error: use drain nodes <id>")
			return res
		}
		id, err := strconv.Atoi(parts[2])
		if err != nil {
			appendLog(&res, "error: drain node id must be integer")
			return res
		}
		if c.NodeByID(id) == nil {
			appendLog(&res, fmt.Sprintf("error: unknown node id %d", id))
			return res
		}
		res.DrainNodeID = &id
		appendLog(&res, fmt.Sprintf("drain requested node=%d", id))
	case "uncordon":
		// kubectl-like: uncordon nodes <id>
		if len(parts) < 3 || strings.ToLower(parts[1]) != "nodes" {
			appendLog(&res, "error: use uncordon nodes <id>")
			return res
		}
		id, err := strconv.Atoi(parts[2])
		if err != nil {
			appendLog(&res, "error: uncordon node id must be integer")
			return res
		}
		n := c.NodeByID(id)
		if n == nil {
			appendLog(&res, fmt.Sprintf("error: unknown node id %d", id))
			return res
		}
		n.Cordoned = false
		res.UncordonNodeID = &id
		appendLog(&res, fmt.Sprintf("uncordon node=%d", id))
	case "taint":
		// kubectl-like:
		// - taint nodes <node> key=value:NoSchedule
		// - taint nodes <node> key:NoExecute
		// (key/value are ignored by this toy; effect is applied)
		if len(parts) < 4 || strings.ToLower(parts[1]) != "nodes" {
			appendLog(&res, "error: use taint nodes <id> key=value:NoSchedule|NoExecute")
			return res
		}
		id, err := strconv.Atoi(parts[2])
		if err != nil {
			appendLog(&res, "error: taint node id must be integer")
			return res
		}
		n := c.NodeByID(id)
		if n == nil {
			appendLog(&res, fmt.Sprintf("error: unknown node id %d", id))
			return res
		}
		tt, ok := parseTaintEffect(parts[3])
		if !ok {
			appendLog(&res, "error: taint effect must be NoSchedule or NoExecute (e.g. key=value:NoSchedule)")
			return res
		}
		n.Tainted = true
		n.TaintType = tt
		if tt == state.TaintNoExecute {
			c.ClearAliveOnNode(id)
		}
		appendLog(&res, fmt.Sprintf("taint node=%d %s", id, tt))
	case "untaint":
		// kubectl-like: untaint nodes <id> <key>
		if len(parts) < 4 || strings.ToLower(parts[1]) != "nodes" {
			appendLog(&res, "error: use untaint nodes <id> <key>")
			return res
		}
		id, err := strconv.Atoi(parts[2])
		if err != nil {
			appendLog(&res, "error: untaint node id must be integer")
			return res
		}
		n := c.NodeByID(id)
		if n == nil {
			appendLog(&res, fmt.Sprintf("error: unknown node id %d", id))
			return res
		}
		n.Tainted = false
		n.TaintType = ""
		appendLog(&res, fmt.Sprintf("untaint node=%d", id))
	default:
		appendLog(&res, fmt.Sprintf("unknown command: %s", cmd))
	}
	return res
}

func applyScaleReplicas(rep int, c *state.Cluster, res *ExecResult) ExecResult {
	if c.Deploy.RolloutInProgress {
		appendLog(res, "error: cannot scale while rollout in progress")
		return *res
	}
	if rep < 0 {
		appendLog(res, "error: replicas must be non-negative")
		return *res
	}
	maxp := c.MaxPods()
	if rep > maxp {
		rep = maxp
		appendLog(res, fmt.Sprintf("replicas clamped to maxPods=%d", maxp))
	}
	c.Deploy.Enabled = true
	c.Deploy.Replicas = rep
	if len(c.RSs) == 0 {
		c.RSs = []state.ReplicaSet{{
			Revision: 1,
			Image:    c.Deploy.Image,
			Desired:  rep,
			Enabled:  true,
		}}
		c.Deploy.ActiveRevision = 1
		appendLog(res, fmt.Sprintf("Deployment enabled replicas=%d RSRev=1 (effective<=%d)", rep, maxp))
		return *res
	}
	ar := c.RSByRevision(c.Deploy.ActiveRevision)
	if ar != nil {
		ar.Desired = rep
	}
	appendLog(res, fmt.Sprintf("Deployment replicas=%d (effective<=%d)", rep, maxp))
	return *res
}

func applyRolloutRestart(c *state.Cluster, res *ExecResult) {
	if !c.Deploy.Enabled {
		appendLog(res, "error: enable deployment with scale --replicas=<n> before restart")
		return
	}
	if c.Deploy.RolloutInProgress {
		appendLog(res, "error: rollout already in progress")
		return
	}
	if len(c.RSs) == 0 {
		appendLog(res, "error: no ReplicaSet (set replicas first)")
		return
	}
	// Same as set image, but keep the same image.
	prev := c.Deploy.ActiveRevision
	newRev := c.NextRevision()
	c.Deploy.Generation++
	c.Deploy.OldRevision = prev
	c.Deploy.ActiveRevision = newRev
	c.Deploy.RolloutInProgress = true
	c.RSs = append(c.RSs, state.ReplicaSet{
		Revision: newRev,
		Image:    c.Deploy.Image,
		Desired:  0,
		Enabled:  true,
	})
	appendLog(res, fmt.Sprintf("rollout restart gen=%d image=%s newRSRev=%d oldRSRev=%d", c.Deploy.Generation, c.Deploy.Image, newRev, prev))
}

func applyDeploymentEnableFromCurrentAlive(c *state.Cluster, res *ExecResult) {
	if c == nil || c.Grid == nil {
		appendLog(res, "error: no cluster/grid")
		return
	}
	cur := c.AliveCount()
	maxp := c.MaxPods()
	if cur > maxp {
		cur = maxp
	}

	// Ensure there is at least one ReplicaSet to own desired state.
	if len(c.RSs) == 0 {
		c.RSs = []state.ReplicaSet{{
			Revision: 1,
			Image:    c.Deploy.Image,
			Desired:  cur,
			Enabled:  true,
		}}
		c.Deploy.ActiveRevision = 1
	}
	if c.Deploy.ActiveRevision <= 0 {
		c.Deploy.ActiveRevision = c.RSs[0].Revision
	}
	// Keep existing desired for non-active RS (if any), but set replicas + active desired from current alive.
	c.Deploy.Replicas = cur
	if ar := c.RSByRevision(c.Deploy.ActiveRevision); ar != nil {
		ar.Desired = cur
	}
	c.Deploy.Enabled = true
	appendLog(res, fmt.Sprintf("Deployment enabled replicas=%d (from current alive)", cur))
}

func applyDeploymentDisableKeepDesired(c *state.Cluster, res *ExecResult) {
	// Disable the deployment controller without resetting desired/ReplicaSets.
	// Grid (cells/revisions) is left as-is; controller stops mutating the world.
	if !c.Deploy.Enabled {
		appendLog(res, "Deployment already off")
		return
	}
	c.Deploy.Enabled = false
	c.Deploy.RolloutInProgress = false
	c.Deploy.OldRevision = 0
	appendLog(res, fmt.Sprintf("Deployment disabled (desired kept replicas=%d)", c.Deploy.Replicas))
}

func parseReplicasFlag(args []string) (int, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--replicas=") {
			v := strings.TrimPrefix(a, "--replicas=")
			n, err := strconv.Atoi(v)
			return n, err == nil && n >= 0
		}
		if a == "--replicas" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			return n, err == nil && n >= 0
		}
	}
	return 0, false
}

func parseTaintEffect(spec string) (string, bool) {
	// spec like "key=value:NoSchedule" or "key:NoExecute"
	_, eff, ok := strings.Cut(spec, ":")
	if !ok {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(eff)) {
	case "noschedule":
		return state.TaintNoSchedule, true
	case "noexecute":
		return state.TaintNoExecute, true
	default:
		return "", false
	}
}
