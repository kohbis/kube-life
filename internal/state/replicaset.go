package state

// DefaultWorkloadImage is the initial Deployment template image (display + rollout identity).
const DefaultWorkloadImage = "kube-life/cell:latest"

// Deployment is a toy declarative workload (owns ReplicaSets by revision).
type Deployment struct {
	Enabled           bool
	Replicas          int
	Image             string
	Generation        int // increments on template (image) change
	ActiveRevision    int // RS revision for current template
	OldRevision       int // RS revision being replaced; 0 when not rolling
	RolloutInProgress bool
}

// ReplicaSet is one revision of pods (cells tagged with matching Grid.Rev).
type ReplicaSet struct {
	Revision int
	Image    string
	Desired  int
	Enabled  bool
}
