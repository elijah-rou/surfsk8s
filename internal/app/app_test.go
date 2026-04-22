package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestContextToggleKeepsCursorPosition(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.contexts = []cluster.ContextInfo{{Name: "dev", Current: true}, {Name: "stage"}, {Name: "prod"}}
	app.selectedContext = map[string]bool{"dev": true}
	app.refreshContextRows()
	app.navTable.MoveDown(2)

	app.updateContextKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if got, want := app.navTable.SelectedIndex(), 2; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	if !app.selectedContext["prod"] {
		t.Fatalf("expected prod selected")
	}
}

func TestOpenContextPickerClearsActiveFilter(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPods
	app.filter.Activate()
	app.filter.SetValue("api")

	app.openContextPicker(pickerModeEnter)
	if app.filter.Active() {
		t.Fatalf("expected inactive filter")
	}
	if got, want := app.filter.Value(), ""; got != want {
		t.Fatalf("filter value = %q, want %q", got, want)
	}
}

func TestContextKeysSupportUppercaseJump(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.contexts = []cluster.ContextInfo{{Name: "dev", Current: true}, {Name: "stage"}, {Name: "prod"}}
	app.refreshContextRows()

	app.updateContextKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	if got, want := app.navTable.SelectedIndex(), 2; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	app.updateContextKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if got, want := app.navTable.SelectedIndex(), 0; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
}

func TestContextFilterPromptSupportsVimNavigationKeys(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.contexts = []cluster.ContextInfo{{Name: "dev", Current: true}, {Name: "stage"}, {Name: "prod"}}
	app.screen = screenContexts
	app.refreshContextRows()
	app.filter.Activate()

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.navTable.SelectedIndex(), 1; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	if got, want := app.navTable.SelectedIndex(), 2; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'K'}})
	if got, want := app.navTable.SelectedIndex(), 0; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	if got, want := app.filter.Value(), ""; got != want {
		t.Fatalf("filter value = %q, want %q", got, want)
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
	tempDir := t.TempDir()
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return tempDir, nil }
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
	tempDir := t.TempDir()
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return tempDir, nil }
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

func TestPodColumnWidthAdjustmentPersists(t *testing.T) {
	tempDir := t.TempDir()
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return tempDir, nil }
	defer func() { userConfigDir = previousUserConfigDir }()
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPods
	app.refreshPods(time.Now())

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if got, want := app.currentPodColumns()[0].Width, 17; got != want {
		t.Fatalf("width = %d, want %d", got, want)
	}

	app2 := New(state.NewStore(), manager, Config{})
	if got, want := app2.currentPodColumns()[0].Width, 17; got != want {
		t.Fatalf("persisted width = %d, want %d", got, want)
	}
}

func TestPodColumnCollapsePersists(t *testing.T) {
	tempDir := t.TempDir()
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return tempDir, nil }
	defer func() { userConfigDir = previousUserConfigDir }()
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPods
	app.refreshPods(time.Now())

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	if got, want := app.currentPodColumns()[0].Width, collapsedTableColumnWidth; got != want {
		t.Fatalf("collapsed width = %d, want %d", got, want)
	}

	app2 := New(state.NewStore(), manager, Config{})
	if got, want := app2.currentPodColumns()[0].Width, collapsedTableColumnWidth; got != want {
		t.Fatalf("persisted collapsed width = %d, want %d", got, want)
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
