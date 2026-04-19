package app

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestMatchesSearchSupportsWildcardAndExact(t *testing.T) {
	candidate := "services serving.knative.dev namespaced"
	if !matchesSearch(candidate, "*serv*knative*") {
		t.Fatalf("expected wildcard match")
	}
	if !matchesSearch(candidate, "=serving.knative.dev") {
		t.Fatalf("expected exact substring match")
	}
	if matchesSearch(candidate, "=missing") {
		t.Fatalf("expected exact substring miss")
	}
}

func TestOpenResourceListKeepsCRDServicesInDetailsMode(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})

	app.openResourceList(cluster.ResourceKind{
		Display:    "services",
		Resource:   "services",
		APIGroup:   "serving.knative.dev",
		Namespaced: true,
		Custom:     true,
	})

	if got, want := app.screen, screenResourceDetails; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
}

func TestPodSortByNameDescending(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "zeta", Namespace: "default", CreationTimestamp: metav1.NewTime(time.Unix(100, 0))}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "default", CreationTimestamp: metav1.NewTime(time.Unix(200, 0))}})

	app.podSort = listSortState{key: sortKeyName, reverse: true}
	app.refreshPods(time.Now())
	rows := app.podWindow(0, 2, time.Now())
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if got, want := rows[0].Name, "zeta"; got != want {
		t.Fatalf("row[0] = %q, want %q", got, want)
	}
	if got, want := rows[1].Name, "alpha"; got != want {
		t.Fatalf("row[1] = %q, want %q", got, want)
	}
}

func TestScalePromptActivatesAndRunsCommand(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	replicas := int32(3)
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.activeDeployment = state.DeploymentDetails{
		Row: state.DeploymentRow{Cluster: "dev", Namespace: "default", Name: "frontend"},
		Deployment: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		},
	}

	cmd := app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd != nil {
		t.Fatalf("expected scale key to open prompt, not run immediately")
	}
	if !app.filter.Active() {
		t.Fatalf("expected prompt active")
	}
	if got, want := app.inputMode, inputModeScale; got != want {
		t.Fatalf("inputMode = %d, want %d", got, want)
	}

	app.filter.SetValue("5")
	cmd = app.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected scale exec command")
	}
}
