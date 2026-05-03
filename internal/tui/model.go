package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"kube-life/internal/command"
	"kube-life/internal/sim"
	"kube-life/internal/state"
)

const (
	maxLogLines = 50
	maxInputLen = 256
	workloadCPU = "100m"
)

type tickMsg struct{}

var revColors = []lipgloss.Color{
	lipgloss.Color("2"),  // green
	lipgloss.Color("4"),  // blue
	lipgloss.Color("5"),  // magenta
	lipgloss.Color("6"),  // cyan
	lipgloss.Color("3"),  // yellow
	lipgloss.Color("1"),  // red
	lipgloss.Color("13"), // bright magenta
	lipgloss.Color("14"), // bright cyan
	lipgloss.Color("10"), // bright green
	lipgloss.Color("12"), // bright blue
}

func styleForRevision(rev int) lipgloss.Style {
	if rev <= 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	}
	c := revColors[(rev-1)%len(revColors)]
	return lipgloss.NewStyle().Foreground(c).Bold(true)
}

func styleForTaintedDead() lipgloss.Style {
	// Use a faint gray so the tainted node region is visible at a glance.
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Faint(true)
}

// Model is the bubbletea TUI state.
type Model struct {
	cluster *state.Cluster
	rt      *command.Runtime

	input  string
	logs   []string
	width  int
	tickMS int
	rng    *rand.Rand
	// golTicks counts completed Game of Life steps (incremented when not paused).
	golTicks int
	// deployPhase is the index in sim.DeploymentControlCycle (idle→observe→apply) while Deployment is enabled.
	deployPhase int
	// lastDeployCtrl labels what the deployment controller did on the last tick ("idle"|"observe"|"apply"|"-").
	lastDeployCtrl string

	// Initial config for sim reset.
	initW     int
	initH     int
	initNodes int
	initSeed  int64

	// Command feedback (sticky): last entered command and its output lines.
	lastCmd     string
	lastCmdLogs []string
	// Important internal event (sticky): e.g. drain done, scale-up throttled.
	lastEvent string
	// Drain progress (status): updated each tick while draining.
	lastDrainStatus string

	// Drain state: when active, we move one pod per tick.
	drainActive  bool
	drainNodeID  int
	drainQueue   []int
	drainMoved   int
	drainBlocked int
}

// NewModel builds a TUI model. initLogs are appended to the log ring.
func NewModel(c *state.Cluster, rt *command.Runtime, tickMS int, rng *rand.Rand, termWidth int, initLogs []string, w, h, nodes int, seed int64) *Model {
	m := &Model{
		cluster:   c,
		rt:        rt,
		tickMS:    tickMS,
		rng:       rng,
		width:     termWidth,
		initW:     w,
		initH:     h,
		initNodes: nodes,
		initSeed:  seed,
	}
	m.logs = append(m.logs, initLogs...)
	m.pushLog("kube-life: virtual cluster (no kube API). q or Ctrl+C to quit.")
	m.lastCmd = "(start)"
	m.lastCmdLogs = append([]string(nil), m.logs...)
	return m
}

func (m *Model) doReset() {
	c, initLogs := state.NewCluster(m.initW, m.initH, m.initNodes, m.initSeed)
	m.cluster = c
	m.golTicks = 0
	m.deployPhase = 0
	m.lastDeployCtrl = "-"
	// reset reconcile RNG stream too
	m.rng = rand.New(rand.NewSource(m.initSeed + 1337))
	// Clear historical logs so reset feedback doesn't include older commands.
	m.logs = nil
	for _, l := range initLogs {
		m.pushLog(l)
	}
	m.pushLog("sim reset")
	// Command feedback should show only reset output (not the whole log ring).
	out := append([]string(nil), initLogs...)
	out = append(out, "sim reset")
	m.setCmdFeedback("sim reset", out)
	m.lastEvent = ""
	m.lastDrainStatus = ""
}

func (m *Model) pushLog(s string) {
	if s == "" {
		return
	}
	m.logs = append(m.logs, s)
	if len(m.logs) > maxLogLines {
		m.logs = m.logs[len(m.logs)-maxLogLines:]
	}
}

func (m *Model) setCmdFeedback(cmd string, logs []string) {
	m.lastCmd = cmd
	if len(logs) > maxLogLines {
		logs = logs[len(logs)-maxLogLines:]
	}
	m.lastCmdLogs = append([]string(nil), logs...)
}

func (m *Model) Init() tea.Cmd {
	return tea.Tick(time.Duration(m.tickMS)*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) nextTick() tea.Cmd {
	return tea.Tick(time.Duration(m.tickMS)*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tickMsg:
		if !m.rt.Paused {
			// Track rollout completion event.
			prevRolling := false
			if m.cluster != nil {
				prevRolling = m.cluster.Deploy.RolloutInProgress
			}

			if m.cluster != nil && m.cluster.Grid != nil {
				m.cluster.Grid.ClearBirthMarkers()
			}
			if !m.rt.GoLPaused {
				var prev []state.Cell
				if m.cluster != nil && m.cluster.Grid != nil {
					prev = append([]state.Cell(nil), m.cluster.Grid.Cells...)
				}
				sim.GameOfLifeStep(m.cluster)
				if m.cluster != nil && m.cluster.Grid != nil && len(prev) == len(m.cluster.Grid.Cells) {
					for i := range m.cluster.Grid.Cells {
						if prev[i] == state.Dead && m.cluster.Grid.Cells[i] == state.Alive {
							if i < len(m.cluster.Grid.Birth) {
								m.cluster.Grid.Birth[i] = state.BirthGoL
							}
						}
					}
				}
				m.golTicks++
			}
			// NoExecute eviction: keep the node region empty (toy approximation of eviction controller).
			for i := range m.cluster.Nodes {
				n := &m.cluster.Nodes[i]
				if n.Tainted && n.TaintType == state.TaintNoExecute {
					// If eviction actually clears pods, surface an event.
					before := n.AliveOnNode(m.cluster.Grid)
					m.cluster.ClearAliveOnNode(n.ID)
					after := n.AliveOnNode(m.cluster.Grid)
					if before > 0 && after == 0 {
						m.lastEvent = fmt.Sprintf("eviction: node=%d NoExecute cleared=%d", n.ID, before)
					}
				}
			}

			// Drain: move one pod per tick to make the effect visible.
			if m.drainActive && m.cluster != nil {
				if n := m.cluster.NodeByID(m.drainNodeID); n != nil {
					n.Draining = true
					n.Cordoned = true
				}
				// Try to move exactly one pod per tick. Keep the queue intact on "blocked".
				for len(m.drainQueue) > 0 {
					src := m.drainQueue[0] // peek
					moved, blocked := sim.DrainStep(m.cluster, m.rng, m.drainNodeID, src)
					if moved {
						m.drainMoved++
						m.drainQueue = m.drainQueue[1:] // pop only on success
						break
					}
					if blocked {
						m.drainBlocked++
						m.lastEvent = fmt.Sprintf("drain blocked: node=%d remainingPods=%d", m.drainNodeID, len(sim.DrainList(m.cluster, m.drainNodeID)))
						// Stop draining; keep cordoned. Queue is preserved for a future retry.
						if n := m.cluster.NodeByID(m.drainNodeID); n != nil {
							n.Draining = false
							n.Cordoned = true
						}
						m.drainActive = false
						break
					}
					// src was already dead; drop it and keep looking this tick
					m.drainQueue = m.drainQueue[1:]
				}
				remaining := 0
				if m.cluster != nil {
					remaining = len(sim.DrainList(m.cluster, m.drainNodeID))
				}
				m.lastDrainStatus = fmt.Sprintf("drain: node=%d remaining=%d moved=%d blocked=%d", m.drainNodeID, remaining, m.drainMoved, m.drainBlocked)
				// Drain is done only when there are no pods remaining on the node.
				if remaining == 0 {
					if n := m.cluster.NodeByID(m.drainNodeID); n != nil {
						n.Draining = false
						// Keep cordoned until explicit uncordon.
						n.Cordoned = true
					}
					m.drainActive = false
					ev := fmt.Sprintf("drain done: node=%d moved=%d blocked=%d", m.drainNodeID, m.drainMoved, m.drainBlocked)
					m.lastEvent = ev
					m.pushLog(ev)
				}
			} else {
				m.lastDrainStatus = ""
			}
			if m.cluster.Deploy.Enabled {
				switch m.deployPhase % sim.DeploymentControlCycle {
				case 0:
					m.lastDeployCtrl = "idle"
				case 1:
					m.lastDeployCtrl = "observe"
				default:
					m.lastDeployCtrl = "apply"
				}
				if logs := sim.ReconcileForPhase(m.cluster, m.rng, m.deployPhase); len(logs) > 0 {
					for _, l := range logs {
						// Surface only important internal events to the UI.
						if strings.Contains(l, "scale-up throttled") {
							m.lastEvent = l
						}
						m.pushLog(l)
					}
				}
				// rollout completion event
				if prevRolling && !m.cluster.Deploy.RolloutInProgress {
					m.lastEvent = fmt.Sprintf("rollout done: gen=%d activeRSRev=%d", m.cluster.Deploy.Generation, m.cluster.Deploy.ActiveRevision)
				}
				m.deployPhase = (m.deployPhase + 1) % sim.DeploymentControlCycle
			} else {
				m.deployPhase = 0
				m.lastDeployCtrl = "-"
			}
		}
		return m, m.nextTick()
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter":
			line := strings.TrimSpace(m.input)
			m.input = ""
			if line == "" {
				return m, nil
			}
			res := command.Exec(line, m.cluster, m.rt)
			if res.DrainNodeID != nil {
				// Start draining (one pod per tick); immediately mark region as draining for visuals.
				m.drainActive = true
				m.drainNodeID = *res.DrainNodeID
				m.drainQueue = sim.DrainList(m.cluster, m.drainNodeID)
				m.drainMoved = 0
				m.drainBlocked = 0
				if n := m.cluster.NodeByID(m.drainNodeID); n != nil {
					n.Draining = true
					n.Cordoned = true
				}
				res.Logs = append(res.Logs, fmt.Sprintf("drain: node=%d queued=%d (moving 1 per tick)", m.drainNodeID, len(m.drainQueue)))
			}
			if res.UncordonNodeID != nil {
				// If we were actively draining this node, cancel draining visuals/queue.
				if m.drainActive && m.drainNodeID == *res.UncordonNodeID {
					m.drainActive = false
					m.drainQueue = nil
					if n := m.cluster.NodeByID(m.drainNodeID); n != nil {
						n.Draining = false
					}
				}
			}
			for _, l := range res.Logs {
				m.pushLog(l)
			}
			m.setCmdFeedback(line, res.Logs)
			if res.Reset {
				m.doReset()
			}
			if res.SetPaused != nil {
				m.rt.Paused = *res.SetPaused
			}
			if res.SetGoLPaused != nil {
				m.rt.GoLPaused = *res.SetGoLPaused
			}
			if res.TickMS != nil {
				m.tickMS = *res.TickMS
				m.rt.TickMS = *res.TickMS
			}
			return m, nil
		case " ", "space":
			if len(m.input) < maxInputLen {
				m.input += " "
			}
			return m, nil
		case "backspace":
			if m.input != "" {
				_, size := utf8.DecodeLastRuneInString(m.input)
				if size > 0 {
					m.input = m.input[:len(m.input)-size]
				}
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes {
				for _, r := range msg.Runes {
					if len(m.input) >= maxInputLen {
						break
					}
					m.input += string(r)
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) View() string {
	maxW := m.width
	if maxW <= 0 {
		maxW = 80
	}
	g := m.cluster.Grid
	maxCols := maxW - 1
	if maxCols < 8 {
		maxCols = 8
	}
	// Grid rendering adds a 1-char separator between cells to make node boundaries visible:
	// width ~= 2*cols - 1, so clamp cols accordingly.
	cols := g.Width
	if cols > (maxCols+1)/2 {
		cols = (maxCols + 1) / 2
	}
	if cols < 1 {
		cols = 1
	}

	var b strings.Builder
	for y := 0; y < g.Height; y++ {
		if y > 0 {
			prevStart := g.Index(0, y-1)
			curStart := g.Index(0, y)
			if prevStart < len(m.cluster.CellOwner) && curStart < len(m.cluster.CellOwner) {
				if m.cluster.CellOwner[prevStart] != m.cluster.CellOwner[curStart] {
					// Horizontal node boundary (row-major partitions often change at row boundaries).
					b.WriteString(strings.Repeat("-", 2*cols-1))
					b.WriteByte('\n')
				}
			}
		}
		for x := 0; x < cols; x++ {
			idx := g.Index(x, y)
			tainted := false
			if idx >= 0 && idx < len(m.cluster.CellOwner) {
				owner := m.cluster.CellOwner[idx]
				if n := m.cluster.NodeByID(owner); n != nil && n.Tainted {
					if n.TaintType == state.TaintNoSchedule || n.TaintType == state.TaintNoExecute {
						tainted = true
					}
				}
				if n := m.cluster.NodeByID(owner); n != nil && (n.Draining || n.Cordoned) {
					tainted = true
				}
			}
			if g.Cells[idx] == state.Alive {
				rev := -1
				if idx < len(g.Rev) {
					rev = g.Rev[idx]
				}
				ch := "#"
				if idx < len(g.Birth) {
					switch g.Birth[idx] {
					case state.BirthGoL:
						ch = "*"
					case state.BirthDeployment:
						ch = "@"
					case state.BirthDrain:
						ch = "^"
					}
				}
				b.WriteString(styleForRevision(rev).Render(ch))
			} else {
				if tainted {
					b.WriteString(styleForTaintedDead().Render("."))
				} else {
					b.WriteByte('.')
				}
			}
			if x < cols-1 {
				sep := byte(' ')
				if idx+1 < len(m.cluster.CellOwner) {
					if m.cluster.CellOwner[idx] != m.cluster.CellOwner[idx+1] {
						sep = '|'
					}
				}
				b.WriteByte(sep)
			}
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	golLine := "GoL ticks=" + itoa(m.golTicks)
	if m.rt != nil && m.rt.GoLPaused {
		golLine += " (paused)"
	}
	if cols < g.Width {
		golLine += " gridCols=" + itoa(cols) + "/" + itoa(g.Width)
	}
	b.WriteString(truncateLine(golLine, maxW))
	b.WriteByte('\n')
	b.WriteString(truncateLine("Legend: *=GoL birth  @=Deployment spawn  ^=drain relocate", maxW))
	b.WriteByte('\n')
	ev := m.lastEvent
	if ev == "" {
		ev = "(none)"
	}
	b.WriteString(truncateLine("Event: "+ev, maxW))
	b.WriteByte('\n')
	ds := m.lastDrainStatus
	if ds == "" {
		ds = "(none)"
	}
	b.WriteString(truncateLine("Status: "+ds, maxW))
	b.WriteByte('\n')
	b.WriteByte('\n')

	for _, line := range RenderNodeTiles(m.cluster, maxW) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	img := m.cluster.Deploy.Image
	wl := fmt.Sprintf("workload  image=%s cpu=%s maxPods=%d (display only)", img, workloadCPU, m.cluster.MaxPods())
	b.WriteString(truncateLine(wl, maxW))
	b.WriteByte('\n')

	cur := m.cluster.AliveCount()
	d := m.cluster.Deploy
	if !d.Enabled {
		b.WriteString(truncateLine("Deployment: off  replicas=-  effective=-  current="+itoa(cur), maxW))
	} else {
		eff := sim.EffectiveDesired(m.cluster)
		ctrl := m.lastDeployCtrl
		if ctrl == "" {
			ctrl = "-"
		}
		// Split fast-changing fields onto separate lines to reduce visual churn.
		line1 := fmt.Sprintf("Deployment: on  image=%s  gen=%d  replicas=%d  activeRSRev=%d",
			d.Image, d.Generation, d.Replicas, d.ActiveRevision)
		// Keep the status line visually stable by using fixed-width fields.
		// Width is based on maxPods so current/effective don't shift as they change.
		numW := len(itoa(m.cluster.MaxPods()))
		line2 := fmt.Sprintf("  status: current=%*d  effective=%*d  ctrl=%-7s", numW, cur, numW, eff, ctrl)
		b.WriteString(truncateLine(line1, maxW))
		b.WriteByte('\n')
		b.WriteString(truncateLine(line2, maxW))
		b.WriteByte('\n')
		if d.RolloutInProgress && d.OldRevision > 0 {
			oldR := m.cluster.RSByRevision(d.OldRevision)
			newR := m.cluster.RSByRevision(d.ActiveRevision)
			var od, oc, nd, nc int
			if oldR != nil {
				od, oc = oldR.Desired, m.cluster.CountPodsByRevision(oldR.Revision)
			}
			if newR != nil {
				nd, nc = newR.Desired, m.cluster.CountPodsByRevision(newR.Revision)
			}
			roll := fmt.Sprintf("  rollout: oldRSRev=%d desired=%d current=%d | newRSRev=%d desired=%d current=%d",
				d.OldRevision, od, oc, d.ActiveRevision, nd, nc)
			b.WriteString(truncateLine(roll, maxW))
		}
	}
	b.WriteByte('\n')
	b.WriteByte('\n')

	b.WriteString("----------\n")
	b.WriteString(truncateLine("> "+m.lastCmd, maxW))
	b.WriteByte('\n')
	if len(m.lastCmdLogs) == 0 {
		b.WriteString(truncateLine("(no output)", maxW))
		b.WriteByte('\n')
	} else {
		maxLines := 6
		if len(m.lastCmdLogs) < maxLines {
			maxLines = len(m.lastCmdLogs)
		}
		for i := 0; i < maxLines; i++ {
			b.WriteString(truncateLine(m.lastCmdLogs[i], maxW))
			b.WriteByte('\n')
		}
		if len(m.lastCmdLogs) > maxLines {
			b.WriteString(truncateLine(fmt.Sprintf("... (%d more lines)", len(m.lastCmdLogs)-maxLines), maxW))
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	b.WriteString("> ")
	// Truncate the rendered input (keep tail) so narrow terminals don't overflow.
	avail := maxW - 2 // for "> "
	if avail < 0 {
		avail = 0
	}
	if len(m.input) <= avail {
		b.WriteString(m.input)
	} else if avail <= 3 {
		b.WriteString(m.input[len(m.input)-avail:])
	} else {
		tail := m.input[len(m.input)-(avail-3):]
		b.WriteString("..." + tail)
	}
	return b.String()
}

func itoa(i int) string { return fmt.Sprintf("%d", i) }

func truncateLine(s string, maxW int) string {
	if maxW <= 0 || len(s) <= maxW {
		return s
	}
	if maxW <= 3 {
		return s[:maxW]
	}
	return s[:maxW-3] + "..."
}
