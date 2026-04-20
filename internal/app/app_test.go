package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestNewReturnsPointerModel(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	if app == nil {
		t.Fatalf("expected app pointer")
	}
}

func TestContextEnterStartsAsyncConnect(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	cmd := app.updateContextKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected connect command")
	}
	if !app.connecting {
		t.Fatalf("expected connecting state")
	}
	if got, want := app.activity, "connecting contexts"; got != want {
		t.Fatalf("activity = %q, want %q", got, want)
	}
}

func TestCommandRunMutatesOriginalApp(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails

	app.commands[1].Run(app)

	if got, want := app.screen, screenCatalog; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
}

func TestNewLoadsPersistedSelectedContexts(t *testing.T) {
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return t.TempDir(), nil }
	defer func() { userConfigDir = previousUserConfigDir }()
	if err := savePreferences(preferences{SelectedContexts: []string{"dev"}}); err != nil {
		t.Fatalf("savePreferences error: %v", err)
	}
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	if !app.selectedContext["dev"] {
		t.Fatalf("expected persisted selected context")
	}
}

func TestNewLoadsPersistedFavoriteResources(t *testing.T) {
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return t.TempDir(), nil }
	defer func() { userConfigDir = previousUserConfigDir }()
	if err := savePreferences(preferences{FavoriteResources: []string{"apps/deployments"}, FavoriteResourcesSet: true}); err != nil {
		t.Fatalf("savePreferences error: %v", err)
	}
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	if !app.favoriteResources["apps/deployments"] {
		t.Fatalf("expected persisted favorite resource")
	}
}

func newTestManager(t *testing.T) *cluster.Manager {
	t.Helper()
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: dev
clusters:
- cluster:
    server: https://example.invalid
  name: dev-cluster
contexts:
- context:
    cluster: dev-cluster
    user: dev-user
  name: dev
users:
- name: dev-user
  user:
    token: test
`
	if err := os.WriteFile(kubeconfigPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	manager, err := cluster.NewManager(state.NewStore(), cluster.Config{KubeconfigPath: kubeconfigPath})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(manager.Close)
	return manager
}
