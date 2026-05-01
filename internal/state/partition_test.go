package state

import (
	"strings"
	"testing"
)

func TestNewClusterClampNodes(t *testing.T) {
	c, logs := NewCluster(3, 3, 99, 1)
	if len(c.Nodes) != 9 {
		t.Fatalf("nodes want 9, got %d", len(c.Nodes))
	}
	if len(logs) == 0 {
		t.Fatal("expected clamp log")
	}
}

func TestNewClusterClampMaxNodes(t *testing.T) {
	c, logs := NewCluster(20, 20, 99, 1)
	if len(c.Nodes) != MaxNodes {
		t.Fatalf("nodes want %d, got %d", MaxNodes, len(c.Nodes))
	}
	var sawMax bool
	for _, line := range logs {
		if strings.Contains(line, "maxNodes") {
			sawMax = true
			break
		}
	}
	if !sawMax {
		t.Fatalf("expected maxNodes clamp log, got %v", logs)
	}
}

func TestPartitionGridTiles_2x2(t *testing.T) {
	parts := PartitionGridTiles(4, 4, 4)
	if len(parts) != 4 {
		t.Fatalf("want 4 parts, got %d", len(parts))
	}
	for i := 0; i < 4; i++ {
		if len(parts[i]) != 4 { // 2x2 tile => 4 cells
			t.Fatalf("part %d size want 4, got %d", i, len(parts[i]))
		}
	}
	seen := map[int]bool{}
	for _, p := range parts {
		for _, idx := range p {
			if idx < 0 || idx >= 16 {
				t.Fatalf("idx out of range: %d", idx)
			}
			if seen[idx] {
				t.Fatalf("duplicate idx: %d", idx)
			}
			seen[idx] = true
		}
	}
	if len(seen) != 16 {
		t.Fatalf("expected full coverage, got %d", len(seen))
	}
}
