package views

import (
	"testing"

	"github.com/elijahrou/surfsk8s/internal/cluster"
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
	if got, want := rows[0][0], "dev"; got != want {
		t.Fatalf("context = %q, want %q", got, want)
	}
	if got, want := rows[0][2], "frontend"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
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

	if got, want := rows[0][0], "dev"; got != want {
		t.Fatalf("context = %q, want %q", got, want)
	}
	if got, want := rows[0][2], "frontend"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
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

	if got, want := rows[0][0], "dev"; got != want {
		t.Fatalf("context = %q, want %q", got, want)
	}
	if got, want := rows[0][3], "ClusterIP"; got != want {
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

	if got, want := rows[0][0], "dev"; got != want {
		t.Fatalf("context = %q, want %q", got, want)
	}
	if got, want := rows[0][1], "node-a"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}

func TestGenericResourcesViewRows(t *testing.T) {
	view := NewGenericResourcesView()
	resource := cluster.ResourceKind{Namespaced: true, PrinterColumns: []cluster.PrinterColumn{{Name: "LATESTCREATED", JSONPath: ".status.latestCreatedRevisionName"}}}
	rows := view.Rows([]cluster.GenericResourceRow{{
		Namespace:     "serving",
		Name:          "revision-0001",
		PrinterValues: []string{"revision-0001"},
		Age:           "5m",
		Cluster:       "dev",
	}}, resource)
	if got, want := rows[0][0], "dev"; got != want {
		t.Fatalf("context = %q, want %q", got, want)
	}
	if got, want := rows[0][2], "revision-0001"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if got, want := rows[0][3], "revision-0001"; got != want {
		t.Fatalf("printer value = %q, want %q", got, want)
	}
	if got, want := len(view.Columns(resource)), 5; got != want {
		t.Fatalf("columns = %d, want %d", got, want)
	}
}
