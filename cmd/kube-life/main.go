package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"kube-life/internal/command"
	"kube-life/internal/state"
	"kube-life/internal/tui"
)

// version is set by goreleaser via -ldflags.
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	nodes := flag.Int("nodes", 1, "number of nodes (partition of grid, max 16)")
	w := flag.Int("w", 40, "grid width")
	h := flag.Int("h", 15, "grid height")
	seed := flag.Int64("seed", 1, "RNG seed for initial grid layout")
	tickMs := flag.Int("tick-ms", command.DefaultTickMS, "simulation tick interval in milliseconds")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	c, initLogs := state.NewCluster(*w, *h, *nodes, *seed)
	rt := &command.Runtime{Paused: false, GoLPaused: false, TickMS: *tickMs}
	// Separate RNG stream for reconcile / shuffle (not the same as NewCluster's internal rng).
	rng := rand.New(rand.NewSource(*seed + 1337))

	m := tui.NewModel(c, rt, *tickMs, rng, 0, initLogs, *w, *h, *nodes, *seed)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
