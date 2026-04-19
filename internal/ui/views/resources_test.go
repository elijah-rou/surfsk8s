package views

import (
	"testing"

	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestPodsViewRows(t *testing.T) {
	view := NewPodsView()
	rows := view.Rows([]state.PodRow{{
		Namespace: "web",
		Name:      "frontend",
		Ready:     "1/1",
		Status:    "Running",
		Restarts:  2,
		Age:       "5m",
		Node:      "node-a",
		Cluster:   "dev",
	}})

	if got, want := len(rows), 1; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if got, want := rows[0][1], "frontend"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := rows[0][7], "dev"; got != want {
		t.Fatalf("cluster = %q, want %q", got, want)
	}
}

func TestDeploymentsViewRows(t *testing.T) {
	view := NewDeploymentsView()
	rows := view.Rows([]state.DeploymentRow{{
		Namespace: "web",
		Name:      "frontend",
		Ready:     "2/3",
		UpToDate:  3,
		Available: 2,
		Age:       "10m",
		Cluster:   "dev",
	}})

	if got, want := rows[0][1], "frontend"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := rows[0][6], "dev"; got != want {
		t.Fatalf("cluster = %q, want %q", got, want)
	}
}

func TestServicesViewRows(t *testing.T) {
	view := NewServicesView()
	rows := view.Rows([]state.ServiceRow{{
		Namespace: "web",
		Name:      "frontend",
		Type:      "ClusterIP",
		ClusterIP: "10.0.0.1",
		Ports:     "80/TCP",
		Age:       "10m",
		Cluster:   "dev",
	}})

	if got, want := rows[0][2], "ClusterIP"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
}

func TestNodesViewRows(t *testing.T) {
	view := NewNodesView()
	rows := view.Rows([]state.NodeRow{{
		Name:    "node-a",
		Status:  "Ready",
		Roles:   "worker",
		Version: "v1.32.0",
		Age:     "10d",
		Cluster: "dev",
	}})

	if got, want := rows[0][0], "node-a"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := rows[0][5], "dev"; got != want {
		t.Fatalf("cluster = %q, want %q", got, want)
	}
}
