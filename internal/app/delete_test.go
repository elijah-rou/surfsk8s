package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestPodListDeleteOpensConfirmationForSelectedRow(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}})
	app.screen = screenPods
	app.height = 10
	app.refreshPods(time.Now())

	cmd := app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil {
		t.Fatalf("expected confirmation before delete command")
	}
	if got, want := app.screen, screenConfirmAction; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if !strings.Contains(app.confirmDescription, "delete pod/toolbox") {
		t.Fatalf("confirmDescription = %q", app.confirmDescription)
	}
}

func TestResourceListDeleteOpensConfirmationForSelectedRow(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertDeployment("dev", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"}})
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.screen = screenResourceList
	app.height = 10
	app.refreshResourceList(time.Now())

	cmd := app.updateResourceListKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil {
		t.Fatalf("expected confirmation before delete command")
	}
	if got, want := app.screen, screenConfirmAction; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if !strings.Contains(app.confirmDescription, "delete deployment/frontend") {
		t.Fatalf("confirmDescription = %q", app.confirmDescription)
	}
}

func TestResourceDetailDeleteOpensConfirmation(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.activeDeployment = state.DeploymentDetails{
		Row:        state.DeploymentRow{Cluster: "dev", Namespace: "default", Name: "frontend"},
		Deployment: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"}},
	}

	cmd := app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd != nil {
		t.Fatalf("expected confirmation before delete command")
	}
	if got, want := app.screen, screenConfirmAction; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if !strings.Contains(app.confirmDescription, "delete deployment/frontend") {
		t.Fatalf("confirmDescription = %q", app.confirmDescription)
	}
}
