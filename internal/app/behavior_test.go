package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
)

func drainCmd(t *testing.T, app *App, cmd tea.Cmd) {
	t.Helper()
	const maxSteps = 64
	for step := 0; cmd != nil && step < maxSteps; step++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, nested := range batch {
				drainCmd(t, app, nested)
			}
			return
		}
		model, next := app.Update(msg)
		updated, ok := model.(*App)
		if !ok || updated == nil {
			t.Fatalf("Update returned %T", model)
		}
		*app = *updated
		cmd = next
	}
}

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

func TestOpenResourceListOpensGenericCRDList(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})

	app.openResourceList(cluster.ResourceKind{
		Display:    "services",
		Resource:   "services",
		APIGroup:   "serving.knative.dev",
		Version:    "v1",
		Namespaced: true,
		Custom:     true,
	})

	if got, want := app.screen, screenResourceList; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
}

func TestPodSortByNameDescending(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "zeta", Namespace: "default", CreationTimestamp: metav1.NewTime(time.Unix(100, 0))}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: "default", CreationTimestamp: metav1.NewTime(time.Unix(200, 0))}})

	app.screen = screenPods
	app.height = 10
	app.podTableSorts = []tableSortCriterion{{ID: 1, ColumnIndex: 2, ColumnTitle: "NAME", Desc: true, Enabled: true}}
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

func TestScalePromptRequiresConfirmation(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
	}
	store.UpsertDeployment("dev", deployment)
	details, ok := store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: "dev", Namespace: "default", Name: "frontend"}, time.Now())
	if !ok {
		t.Fatalf("expected deployment details")
	}
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.activeDeployment = details
	app.screen = screenResourceDetails

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
	if cmd != nil {
		t.Fatalf("expected confirmation screen before scale command")
	}
	if got, want := app.screen, screenConfirmAction; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	cmd = app.updateConfirmActionKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected confirmed scale exec command")
	}
}

func TestContextPickerEscTogglesAllSelections(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenContexts
	app.contextQuery = ""
	app.selectedContext = map[string]bool{"dev": true}

	app.updateContextKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if got, want := len(app.selectedContext), 0; got != want {
		t.Fatalf("selected count = %d, want %d", got, want)
	}

	app.updateContextKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if got, want := len(app.selectedContext), len(app.contexts); got != want {
		t.Fatalf("selected count = %d, want %d", got, want)
	}
}

func TestOpenFilterResetsPreviousQuery(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenCatalog
	app.catalogQuery = "pods"
	app.openFilter()
	if got, want := app.catalogQuery, ""; got != want {
		t.Fatalf("catalogQuery = %q, want %q", got, want)
	}
	if got, want := app.filter.Value(), ""; got != want {
		t.Fatalf("filter.Value = %q, want %q", got, want)
	}
}

func TestOpenResourceFinderResetsPreviousQuery(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.resourceFinderQuery = "revisions"
	app.openResourceFinder()
	if got, want := app.resourceFinderQuery, ""; got != want {
		t.Fatalf("resourceFinderQuery = %q, want %q", got, want)
	}
	if got, want := app.filter.Value(), ""; got != want {
		t.Fatalf("filter.Value = %q, want %q", got, want)
	}
}

func TestQuitPersistsResumeSession(t *testing.T) {
	previousUserConfigDir := userConfigDir
	configDir := t.TempDir()
	userConfigDir = func() (string, error) { return configDir, nil }
	defer func() { userConfigDir = previousUserConfigDir }()

	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPods
	app.namespace = "kube-system"
	app.contextScope = "dev"
	app.podQuery = "coredns"
	cmd := app.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatalf("expected quit command")
	}
	prefs, err := loadPreferences()
	if err != nil {
		t.Fatalf("load preferences: %v", err)
	}
	if got, want := prefs.LastSession.Screen, "pods"; got != want {
		t.Fatalf("screen = %q, want %q", got, want)
	}
	if got, want := prefs.LastSession.Query, "coredns"; got != want {
		t.Fatalf("query = %q, want %q", got, want)
	}
	if got, want := prefs.LastSession.Namespace, "kube-system"; got != want {
		t.Fatalf("namespace = %q, want %q", got, want)
	}
}

func TestApplyResumeSessionRestoresPodsView(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	app := New(store, manager, Config{})
	app.resumeSession = sessionPreference{Screen: "pods", Namespace: "kube-system", ContextScope: "dev", Query: "core"}

	app.applyResumeSession(time.Now())
	if got, want := app.screen, screenPods; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.namespace, "kube-system"; got != want {
		t.Fatalf("namespace = %q, want %q", got, want)
	}
	if got, want := app.podQuery, "core"; got != want {
		t.Fatalf("podQuery = %q, want %q", got, want)
	}
	if got, want := app.visibleRows, 1; got != want {
		t.Fatalf("visibleRows = %d, want %d", got, want)
	}
}

func TestGroupResourcePlusAddsFavorite(t *testing.T) {
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return t.TempDir(), nil }
	defer func() { userConfigDir = previousUserConfigDir }()

	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.refreshCatalog()
	for _, group := range app.catalog {
		if group.Name != "Workloads" {
			continue
		}
		app.activeGroup = group
		break
	}
	app.screen = screenGroupResources
	app.refreshGroupResources()
	resourceIndex := -1
	for idx, resource := range app.visibleResources {
		if resource.ID == "apps/statefulsets" {
			resourceIndex = idx
			break
		}
	}
	if resourceIndex < 0 {
		t.Fatalf("expected statefulsets in visible resources")
	}
	app.navTable.MoveDown(resourceIndex)
	resource := app.visibleResources[resourceIndex]
	app.updateGroupKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	if !app.favoriteResources[resource.ID] {
		t.Fatalf("expected resource %q added to favorites", resource.ID)
	}
}

func TestResourceFinderMinusRemovesFavorite(t *testing.T) {
	previousUserConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return t.TempDir(), nil }
	defer func() { userConfigDir = previousUserConfigDir }()

	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.favoriteResourceIDs = append(app.favoriteResourceIDs, "apps/deployments")
	if app.favoriteResources == nil {
		app.favoriteResources = make(map[string]bool, 8)
	}
	app.favoriteResources["apps/deployments"] = true
	app.openResourceFinder()
	for idx, item := range app.visibleResourceItems {
		if item.resource.ID == "apps/deployments" {
			app.navTable.MoveDown(idx)
			break
		}
	}
	app.updateResourceFinderPrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	if app.favoriteResources["apps/deployments"] {
		t.Fatalf("expected deployments removed from favorites")
	}
}

func TestPodColumnFilterAddFlowFiltersRows(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}})
	app.screen = screenPods
	app.height = 10
	app.refreshPods(time.Now())

	cmd := app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if cmd != nil {
		t.Fatalf("expected nil cmd opening filter picker")
	}
	if got, want := app.screen, screenTableFilterColumnPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	app.updateTableFilterColumnPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	app.updateTableFilterColumnPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	app.updateTableFilterColumnPickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := app.inputMode, inputModeTableFilterValue; got != want {
		t.Fatalf("inputMode = %d, want %d", got, want)
	}
	app.filter.SetValue("api")
	app.updateTableFilterValuePrompt(tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := app.screen, screenPods; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.visibleRows, 1; got != want {
		t.Fatalf("visibleRows = %d, want %d", got, want)
	}
	if got, want := len(app.podColumnFilters), 1; got != want {
		t.Fatalf("filter count = %d, want %d", got, want)
	}
}

func TestTableSortManagerReordersAndRemovesSorts(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}})
	app.screen = screenPods
	app.height = 10
	app.podTableSorts = []tableSortCriterion{{ID: 1, ColumnIndex: 2, ColumnTitle: "NAME", Enabled: true}, {ID: 2, ColumnIndex: 0, ColumnTitle: "CONTEXT", Desc: true, Enabled: true}}
	app.refreshPods(time.Now())

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'O'}})
	if got, want := app.screen, screenTableSortManager; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	app.updateTableSortManagerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	if got, want := app.podTableSorts[0].ColumnTitle, "CONTEXT"; got != want {
		t.Fatalf("sort[0] = %q, want %q", got, want)
	}
	app.updateTableSortManagerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if app.podTableSorts[1].Enabled {
		t.Fatalf("expected sort toggled off")
	}
	app.updateTableSortManagerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if got, want := len(app.podTableSorts), 1; got != want {
		t.Fatalf("sort count = %d, want %d", got, want)
	}
}

func TestTableFilterManagerTogglesAndRemovesFilters(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}})
	app.screen = screenPods
	app.height = 10
	app.podColumnFilters = []tableColumnFilter{{ID: 1, ColumnIndex: 2, ColumnTitle: "NAME", Query: "api", Enabled: true}}
	app.refreshPods(time.Now())
	if got, want := app.visibleRows, 1; got != want {
		t.Fatalf("visibleRows = %d, want %d", got, want)
	}

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	if got, want := app.screen, screenTableFilterManager; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	app.updateTableFilterManagerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	app.updateTableFilterManagerKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if got, want := app.screen, screenPods; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.visibleRows, 2; got != want {
		t.Fatalf("visibleRows after disable = %d, want %d", got, want)
	}

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	app.updateTableFilterManagerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	app.updateTableFilterManagerKeys(tea.KeyMsg{Type: tea.KeyEsc})
	if got, want := len(app.podColumnFilters), 0; got != want {
		t.Fatalf("filter count = %d, want %d", got, want)
	}
}

func TestCommandPromptEnterRunsHighlightedCommand(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.openCommands()
	app.commandQuery = "catalog"
	app.filter.SetValue("catalog")
	app.refreshCommands()

	cmd := app.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected command palette enter to run command directly")
	}
	if app.filter.Active() {
		t.Fatalf("expected command prompt closed after run")
	}
	if got, want := app.screen, screenCatalog; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
}

func TestCommandPromptSupportsHomeEndNavigationKeys(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.openCommands()
	app.filter.SetValue("")
	app.refreshCommands()

	app.updateFilter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.navTable.SelectedIndex(), 1; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	app.updateFilter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if got, want := app.navTable.SelectedIndex(), 0; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	app.updateFilter(tea.KeyMsg{Type: tea.KeyEnd})
	if got, want := app.navTable.SelectedIndex(), len(app.visibleCommands)-1; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
}

func TestRunExecPodOpensContainerPickerForMultiContainerPod(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "toolbox"},
		Pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}, {Name: "sidecar"}}}},
	}

	cmd := app.runExecPod()
	if cmd != nil {
		t.Fatalf("expected picker, not exec command")
	}
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if len(app.actionPickerOptions) != 2 {
		t.Fatalf("picker options = %d, want 2", len(app.actionPickerOptions))
	}
}

func TestRunExecPodReturnsStatusForMissingContext(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "", Namespace: "default", Name: "toolbox"},
		Pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}},
	}

	cmd := app.runExecPod()
	if cmd != nil {
		t.Fatalf("expected status error, not exec command")
	}
	if got, want := app.statusMessage, "missing cluster context"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestRunExecResourceReturnsStatusForMissingNodeContext(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	app.activeNode = state.NodeDetails{
		Row:  state.NodeRow{Cluster: "", Name: "node-a"},
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
	}

	cmd := app.runExecResource()
	if cmd != nil {
		t.Fatalf("expected status error, not exec command")
	}
	if got, want := app.statusMessage, "missing cluster context"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestRunPortForwardPodOpensPortPickerForMultiPortPod(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "toolbox"},
		Pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "main",
			Ports: []corev1.ContainerPort{{ContainerPort: 8080}, {ContainerPort: 9090}},
		}}}},
	}

	cmd := app.runPortForwardPod()
	if cmd != nil {
		t.Fatalf("expected picker, not port-forward command")
	}
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
}

func TestPodTableSupportsColumnSelectionAndYank(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}})
	app.screen = screenPods
	app.height = 10
	app.refreshPods(time.Now())

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if got, want := app.podTable.SelectedColumnIndex(), 1; got != want {
		t.Fatalf("selected column = %d, want %d", got, want)
	}

	captured := ""
	previousClipboard := writeClipboard
	writeClipboard = func(value string) error {
		captured = value
		return nil
	}
	defer func() { writeClipboard = previousClipboard }()

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !strings.Contains(captured, "| dev | default | toolbox |") {
		t.Fatalf("captured = %q", captured)
	}
}

func TestPodTableYanksFullFilteredTableWithCapitalY(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	app.screen = screenPods
	app.height = 10
	app.refreshPods(time.Now())

	captured := ""
	previousClipboard := writeClipboard
	writeClipboard = func(value string) error {
		captured = value
		return nil
	}
	defer func() { writeClipboard = previousClipboard }()

	app.updatePodKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
	if !strings.Contains(captured, "CONTEXT,NAMESPACE,NAME,READY,STATUS,CPU,MEMORY,EPHEMERAL,GPU,RESTARTS,AGE,NODE") {
		t.Fatalf("missing header in captured table: %q", captured)
	}
	if !strings.Contains(captured, "toolbox") || !strings.Contains(captured, "api") {
		t.Fatalf("missing rows in captured table: %q", captured)
	}
}

func TestRunPortForwardServicePromptsForEditableLocalPort(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Services", Resource: "services", APIGroup: "", Namespaced: true}
	app.activeService = state.ServiceDetails{
		Row:     state.ServiceRow{Cluster: "dev", Namespace: "default", Name: "gateway"},
		Service: &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8080}, {Name: "metrics", Port: 9090}}}},
	}

	cmd := app.runPortForwardResource()
	if cmd != nil {
		t.Fatalf("expected port picker first")
	}
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}

	cmd = app.updateActionPickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected local-port prompt after port selection")
	}
	if got, want := app.inputMode, inputModeLocalPort; got != want {
		t.Fatalf("inputMode = %d, want %d", got, want)
	}
	if !app.filter.Active() {
		t.Fatalf("expected local-port prompt active")
	}
	if got, want := app.filter.Value(), "8080"; got != want {
		t.Fatalf("local-port default = %q, want %q", got, want)
	}
	if label, _, _ := app.statusInputState(); label != "local-port" {
		t.Fatalf("status label = %q, want local-port", label)
	}

	app.filter.SetValue("9000")
	cmd = app.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("expected port-forward command after local-port confirm")
	}
}

func TestRunEditPodOpensConfirmation(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "toolbox"},
		Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}},
	}

	cmd := app.runEditPod()
	if cmd != nil {
		t.Fatalf("expected confirmation before edit command")
	}
	if got, want := app.screen, screenConfirmAction; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if !strings.Contains(app.confirmDescription, "edit pod/toolbox") {
		t.Fatalf("confirmDescription = %q", app.confirmDescription)
	}
}

func TestConfirmEditPodRevalidatesTarget(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.screen = screenPodDetails
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "toolbox", Namespace: "default"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}}
	store.UpsertPod("dev", pod)
	details, ok := store.PodDetailsByKey(state.PodKey{Cluster: "dev", Namespace: "default", Name: "toolbox"}, time.Now())
	if !ok {
		t.Fatalf("expected pod details")
	}
	app.activePod = details

	cmd := app.runEditPod()
	if cmd != nil {
		t.Fatalf("expected confirmation before edit command")
	}
	store.DeletePod("dev", pod)

	cmd = app.updateConfirmActionKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("expected no command after target deletion")
	}
	if got, want := app.statusMessage, "pod vanished during refresh"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestRunRestartResourceOpensConfirmation(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.activeDeployment = state.DeploymentDetails{
		Row:        state.DeploymentRow{Cluster: "dev", Namespace: "default", Name: "frontend"},
		Deployment: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"}},
	}

	cmd := app.runRestartResource()
	if cmd != nil {
		t.Fatalf("expected confirmation before restart command")
	}
	if got, want := app.screen, screenConfirmAction; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
}

func TestResourceDetailsSupportViewportScrolling(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 100
	app.height = 10
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "revisions", Resource: "revisions", APIGroup: "serving.knative.dev", Version: "v1", Namespaced: true, Custom: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row:    cluster.GenericResourceRow{Name: "api-0001", Namespace: "serving", Cluster: "dev", Ready: "True", Status: "Ready", Age: "5m"},
		YAML:   strings.Join([]string{"a: 1", "b: 2", "c: 3", "d: 4", "e: 5", "f: 6", "g: 7", "h: 8", "i: 9"}, "\n"),
		Object: &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "serving.knative.dev/v1"}},
	}

	_ = app.View()
	if got, want := app.textViewport.YOffset, 0; got != want {
		t.Fatalf("initial offset = %d, want %d", got, want)
	}
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.textViewport.YOffset, 1; got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
}

func TestRenderPodDetailsShowsRichCoreFields(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	started := metav1.NewTime(time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC))
	runtimeClass := "gvisor"
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api", Ready: "1/1", Status: "Running", Restarts: 2, Node: "node-a", Age: "5m"},
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", Labels: map[string]string{"app": "api"}, Annotations: map[string]string{"checksum/config": "123"}},
			Spec: corev1.PodSpec{
				ServiceAccountName: "api-sa",
				NodeName:           "node-a",
				NodeSelector:       map[string]string{"topology.kubernetes.io/zone": "use1a"},
				RuntimeClassName:   &runtimeClass,
				Affinity:           &corev1.Affinity{},
				Tolerations:        []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "gpu", Effect: corev1.TaintEffectNoSchedule}},
				Containers: []corev1.Container{{
					Name:  "main",
					Image: "ghcr.io/acme/api:1.2.3",
					Ports: []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")},
					},
				}},
			},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				QOSClass:   corev1.PodQOSBurstable,
				StartTime:  &started,
				PodIP:      "10.0.0.12",
				HostIP:     "192.168.0.10",
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue, Reason: "ContainersReady"}},
			},
		},
	}
	app.activePodUsage = cluster.PodResourceUsage{CPUUsedMilli: 120, CPURequestMilli: 250, HasCPUUsage: true}
	app.podUsageFetchedAt = time.Now()

	rendered := stripUsageANSI(app.renderPodDetails())
	for _, fragment := range []string{"Pod", "Service account:", "api-sa", "QoS:", "Burstable", "Conditions", "Ready=True", "Scheduling", "Node selector:", "Containers", "requests=cpu=250m", "Annotations", "checksum/config=123"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestRenderDeploymentDetailsShowsStrategyContainersAndPods(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	replicas := int32(3)
	maxUnavailable := intstr.FromString("25%")
	maxSurge := intstr.FromString("1")
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default", Labels: map[string]string{"app": "frontend"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}},
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RollingUpdateDeploymentStrategyType, RollingUpdate: &appsv1.RollingUpdateDeployment{MaxUnavailable: &maxUnavailable, MaxSurge: &maxSurge}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "frontend"}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx:1.27", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")}}}}},
			},
		},
		Status: appsv1.DeploymentStatus{UpdatedReplicas: 3, AvailableReplicas: 2, ReadyReplicas: 2, Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable"}}},
	}
	store.UpsertDeployment("dev", deployment)
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default", Labels: map[string]string{"app": "frontend"}}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default", Labels: map[string]string{"app": "other"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}})
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	details, ok := store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: "dev", Namespace: "default", Name: "frontend"}, time.Now())
	if !ok {
		t.Fatalf("expected deployment details")
	}
	app.activeDeployment = details
	app.refreshAssociatedPods(time.Now())

	rendered := stripUsageANSI(app.renderDeploymentDetails())
	for _, fragment := range []string{"Strategy:", "RollingUpdate", "maxUnavailable=25%", "Rollout", "Conditions", "MinimumReplicasAvailable", "Template resources", "Runtime usage", "Containers", "web", "nginx:1.27", "Requests:", "cpu=250m", "Pod spec"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
	if strings.Contains(rendered, "other") {
		t.Fatalf("unexpected non-associated pod in\n%s", rendered)
	}
}

func TestRenderServiceDetailsShowsPoliciesPortsAndPods(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	internalPolicy := corev1.ServiceInternalTrafficPolicyLocal
	appProtocol := "http"
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default", Labels: map[string]string{"app": "frontend"}},
		Spec: corev1.ServiceSpec{
			Type:                  corev1.ServiceTypeLoadBalancer,
			Selector:              map[string]string{"app": "frontend"},
			ClusterIP:             "10.96.0.10",
			ClusterIPs:            []string{"10.96.0.10"},
			ExternalIPs:           []string{"34.1.2.3"},
			ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyLocal,
			InternalTrafficPolicy: &internalPolicy,
			SessionAffinity:       corev1.ServiceAffinityClientIP,
			Ports:                 []corev1.ServicePort{{Name: "http", Port: 80, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(8080), AppProtocol: &appProtocol}},
		},
	}
	store.UpsertService("dev", service)
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default", Labels: map[string]string{"app": "frontend"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.activeResource = cluster.ResourceKind{Display: "Services", Resource: "services", Namespaced: true}
	details, ok := store.ServiceDetailsByKey(state.ServiceKey{Cluster: "dev", Namespace: "default", Name: "frontend"}, time.Now())
	if !ok {
		t.Fatalf("expected service details")
	}
	app.activeService = details
	app.refreshAssociatedPods(time.Now())

	rendered := stripUsageANSI(app.renderServiceDetails())
	for _, fragment := range []string{"Overview", "Service", "Traffic", "Type:", "LoadBalancer", "Cluster IP:", "10.96.0.10", "External IPs:", "34.1.2.3", "Traffic policy:", "external=Local", "internal=Local", "Ports", "http", "Port:  80/TCP", "Target:  8080", "App:  http"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestRenderDeploymentDetailsUseResponsiveGrid(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 160
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default", Labels: map[string]string{"app": "frontend"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "frontend"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "nginx:1.27"}}}},
		},
		Status: appsv1.DeploymentStatus{UpdatedReplicas: 3, AvailableReplicas: 2, ReadyReplicas: 2, Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable"}}},
	}
	store.UpsertDeployment("dev", deployment)
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default", Labels: map[string]string{"app": "frontend"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	details, ok := store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: "dev", Namespace: "default", Name: "frontend"}, time.Now())
	if !ok {
		t.Fatalf("expected deployment details")
	}
	app.activeDeployment = details
	app.refreshAssociatedPods(time.Now())

	rendered := stripUsageANSI(app.renderDeploymentDetails())
	foundTopRow := false
	foundContainerSection := strings.Contains(rendered, "Containers")
	foundDenseSummaryRow := false
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "Rollout") && strings.Contains(line, "Template resources") && strings.Contains(line, "Runtime usage") {
			foundTopRow = true
		}
		if strings.Contains(line, "Name:") && strings.Contains(line, "Namespace:") {
			foundDenseSummaryRow = true
		}
	}
	if !foundTopRow {
		t.Fatalf("expected deployment glance cards to share top row:\n%s", rendered)
	}
	if !foundContainerSection {
		t.Fatalf("expected deployment container section:\n%s", rendered)
	}
	if !foundDenseSummaryRow {
		t.Fatalf("expected dense deployment summary row in wide layout:\n%s", rendered)
	}
}

func TestDeploymentDetailScaleHintIsVisibleAt160Columns(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 160
	app.height = 44
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}

	rendered := stripUsageANSI(app.View())
	for _, line := range strings.Split(rendered, "\n") {
		index := strings.Index(line, "s scale")
		if index < 0 {
			continue
		}
		if lipgloss.Width(line[:index+len("s scale")]) > app.width {
			t.Fatalf("scale hint ends outside 160-column viewport: %q", line)
		}
		return
	}
	t.Fatalf("deployment detail view has no visible scale hint at width 160:\n%s", rendered)
}

func TestRenderDeploymentDetailsFitAvailableWidth(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default", Labels: map[string]string{"app": "frontend"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "frontend"}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name:  "web",
					Image: "nginx:1.27",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m")},
						Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
					},
				}}},
			},
		},
		Status: appsv1.DeploymentStatus{UpdatedReplicas: 3, AvailableReplicas: 2, ReadyReplicas: 2, Conditions: []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue, Reason: "MinimumReplicasAvailable"}}},
	}
	store.UpsertDeployment("dev", deployment)
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	details, ok := store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: "dev", Namespace: "default", Name: "frontend"}, time.Now())
	if !ok {
		t.Fatalf("expected deployment details")
	}
	app.activeDeployment = details

	rendered := stripUsageANSI(app.renderDeploymentDetails())
	if got, want := maxRenderedLineWidth(rendered), app.resourceDetailContentWidth(); got > want {
		t.Fatalf("deployment layout width = %d, want <= %d\n%s", got, want, rendered)
	}
}

func TestCustomResourceDetailsFitAvailableWidth(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 120
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Widgets", Kind: "Widget", Resource: "widgets", APIGroup: "apps.example.com", Version: "v1", Namespaced: true, Custom: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "blue-widget", Namespace: "default", Cluster: "dev", Ready: "True", Status: "Ready", Age: "8m"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "apps.example.com/v1",
			"kind":       "Widget",
			"metadata": map[string]interface{}{
				"name":        "blue-widget",
				"namespace":   "default",
				"generation":  int64(7),
				"annotations": map[string]interface{}{"owner": "team-a"},
				"labels":      map[string]interface{}{"app": "widget"},
			},
			"spec": map[string]interface{}{
				"replicas": int64(3),
				"image":    "ghcr.io/acme/widget:v1",
			},
			"status": map[string]interface{}{
				"phase":              "Ready",
				"url":                "https://widget.dev",
				"observedGeneration": int64(7),
				"conditions": []interface{}{
					map[string]interface{}{"type": "Ready", "status": "True", "reason": "Healthy"},
				},
			},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	if got, want := maxRenderedLineWidth(rendered), app.resourceDetailContentWidth(); got > want {
		t.Fatalf("custom resource layout width = %d, want <= %d\n%s", got, want, rendered)
	}
}

func TestRenderServiceDetailsSplitPortsLabelsAnnotationsColumns(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 180
	app.activeResource = cluster.ResourceKind{Display: "Services", Resource: "services", Namespaced: true}
	app.activeService = state.ServiceDetails{
		Row:     state.ServiceRow{Cluster: "dev", Namespace: "argocd", Name: "metrics", Age: "91d"},
		Service: &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "metrics", Namespace: "argocd", Labels: map[string]string{"app": "metrics"}, Annotations: map[string]string{"meta.helm.sh/release-name": "argocd", "argocd.argoproj.io/tracking-id": "argocd:/Service:argocd/metrics"}}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, Selector: map[string]string{"app": "metrics"}, Ports: []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}, {Name: "https", Port: 8443, Protocol: corev1.ProtocolTCP}}}},
	}

	rendered := stripUsageANSI(app.renderServiceDetails())
	for _, fragment := range []string{"Overview", "Service", "Traffic", "Selector", "Labels", "Annotations", "Ports", "http", "https"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestRenderNodeDetailsShowsSystemInfoAndScheduledPods(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{"node.kubernetes.io/instance-type": "m5.large"}},
		Spec: corev1.NodeSpec{
			ProviderID: "aws:///us-east-1a/i-123",
			PodCIDR:    "10.0.0.0/24",
			Taints:     []corev1.Taint{{Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule}},
		},
		Status: corev1.NodeStatus{
			Addresses:   []corev1.NodeAddress{{Type: corev1.NodeHostName, Address: "ip-10-0-0-1"}, {Type: corev1.NodeInternalIP, Address: "10.0.0.1"}, {Type: corev1.NodeExternalIP, Address: "54.0.0.1"}},
			NodeInfo:    corev1.NodeSystemInfo{OSImage: "Ubuntu 24.04", KernelVersion: "6.8.0", KubeletVersion: "v1.31.0", KubeProxyVersion: "v1.31.0", ContainerRuntimeVersion: "containerd://2.0.0", Architecture: "arm64"},
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue, Reason: "KubeletReady"}},
			Capacity:    corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16Gi")},
			Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("3900m"), corev1.ResourceMemory: resource.MustParse("15Gi")},
		},
	}
	store.UpsertNode("dev", node)
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "web", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("250m"), corev1.ResourceMemory: resource.MustParse("256Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi")}}}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	details, ok := store.NodeDetailsByKey(state.NodeKey{Cluster: "dev", Name: "node-a"}, time.Now())
	if !ok {
		t.Fatalf("expected node details")
	}
	app.activeNode = details
	app.activeNodeUsage = cluster.NodeResourceUsage{CPUUsedMilli: 1200, CPUAllocatableMilli: 3900, HasCPUUsage: true, MemoryUsedBytes: 2 * 1024 * 1024 * 1024, MemoryAllocatable: 15 * 1024 * 1024 * 1024, HasMemoryUsage: true}
	app.nodeUsageFetchedAt = time.Now()
	app.refreshAssociatedPods(time.Now())

	rendered := stripUsageANSI(app.renderNodeDetails())
	for _, fragment := range []string{"OS image:", "Ubuntu 24.04", "Kernel:", "6.8.0", "Kubelet:", "v1.31.0", "Utilization", "CPU", "Memory", "Requests:", "250m / 3900m", "1.0Gi / 15Gi", "Taints", "dedicated=gpu:NoSchedule", "Conditions", "Ready=True"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestResourceDetailPodPaneCappedToBottomThird(t *testing.T) {
	tableHeight := resourceDetailPodTableHeight(26, 30, 40, 4)
	if got, wantMax := tableHeight, 7; got > wantMax {
		t.Fatalf("table height = %d, want <= %d", got, wantMax)
	}
}

func TestResourceDetailsLazyRefreshesDeploymentPodPane(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	app.height = 24
	replicas := int32(1)
	store.UpsertDeployment("dev", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}}}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default", Labels: map[string]string{"app": "frontend"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	details, ok := store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: "dev", Namespace: "default", Name: "frontend"}, time.Now())
	if !ok {
		t.Fatalf("expected deployment details")
	}
	app.activeDeployment = details
	app.screen = screenResourceDetails

	view := stripUsageANSI(app.View())
	if !strings.Contains(view, "Pods pane (1)") {
		t.Fatalf("expected lazy deployment pod pane refresh in\n%s", view)
	}
}

func TestResourceDetailsEmbeddedPodTableOpensSelectedPod(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	app.height = 24
	replicas := int32(1)
	store.UpsertDeployment("dev", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}}}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default", Labels: map[string]string{"app": "frontend"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	details, ok := store.DeploymentDetailsByKey(state.DeploymentKey{Cluster: "dev", Namespace: "default", Name: "frontend"}, time.Now())
	if !ok {
		t.Fatalf("expected deployment details")
	}
	app.activeDeployment = details
	app.activeDeploymentPods = app.deploymentAssociatedPods(time.Now())
	app.screen = screenResourceDetails

	_ = app.View()
	if !strings.Contains(stripUsageANSI(app.View()), "Pods pane (1)") {
		t.Fatalf("expected embedded pod pane")
	}
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyTab})
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := app.screen, screenPodDetails; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.activePod.Row.Name, "frontend-abc"; got != want {
		t.Fatalf("pod = %q, want %q", got, want)
	}
	if got, want := app.podDetailReturnScreen, screenResourceDetails; got != want {
		t.Fatalf("return screen = %d, want %d", got, want)
	}
}

func TestResourceDetailsViewShowsEmbeddedPodTableFocusState(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	app.height = 24
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	details, ok := store.NodeDetailsByKey(state.NodeKey{Cluster: "dev", Name: "node-a"}, time.Now())
	if !ok {
		t.Fatalf("expected node details")
	}
	app.activeNode = details
	app.activeNodePods = app.nodeAssociatedPods(time.Now())
	app.screen = screenResourceDetails

	view := stripUsageANSI(app.View())
	if !strings.Contains(view, "● Details pane") || !strings.Contains(view, "○ Pods pane (1)") {
		t.Fatalf("expected distinct pane headers in\n%s", view)
	}
	if !strings.Contains(view, "╭") || !strings.Contains(view, "╰") {
		t.Fatalf("expected boxed panes in\n%s", view)
	}
	if !strings.Contains(view, "active pane: details") {
		t.Fatalf("expected active pane in title:\n%s", view)
	}
	if !strings.Contains(view, "j/k scroll") || !strings.Contains(view, "enter open-pod") {
		t.Fatalf("expected per-pane key hints in\n%s", view)
	}
	if !strings.Contains(view, "Focus: details") || !strings.Contains(view, "Pods: 1") {
		t.Fatalf("expected pane status row in\n%s", view)
	}
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	view = stripUsageANSI(app.View())
	if !strings.Contains(view, "○ Details pane") || !strings.Contains(view, "● Pods pane (1)") {
		t.Fatalf("expected pod-table focus header in\n%s", view)
	}
	if !strings.Contains(view, "active pane: pods") {
		t.Fatalf("expected pod active pane in title:\n%s", view)
	}
}

func TestResourceDetailPaneSwitchIsExplicit(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	app.height = 24
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	for idx := 0; idx < 2; idx++ {
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("frontend-%d", idx), Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	}
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	details, ok := store.NodeDetailsByKey(state.NodeKey{Cluster: "dev", Name: "node-a"}, time.Now())
	if !ok {
		t.Fatalf("expected node details")
	}
	app.activeNode = details
	app.activeNodePods = app.nodeAssociatedPods(time.Now())
	app.screen = screenResourceDetails
	_ = app.View()

	drainCmd(t, app, app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}))
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.detailFocus, detailFocusContent; got != want {
		t.Fatalf("focus = %d, want %d", got, want)
	}
	app.updateActionPickerKeys(tea.KeyMsg{Type: tea.KeyEsc})

	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if got, want := app.detailFocus, detailFocusPods; got != want {
		t.Fatalf("focus = %d, want %d", got, want)
	}
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if got, want := app.detailFocus, detailFocusPods; got != want {
		t.Fatalf("focus changed unexpectedly = %d, want %d", got, want)
	}
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	if got, want := app.detailFocus, detailFocusContent; got != want {
		t.Fatalf("focus = %d, want %d", got, want)
	}
}

func TestPodDetailJumpToOwnerOffersOwnerChain(t *testing.T) {
	manager := newTestManager(t)
	manager.SetDiscoveredResourcesForTest("dev", []cluster.ResourceKind{{APIGroup: "apps", Version: "v1", Resource: "replicasets", Kind: "ReplicaSet", Namespaced: true}})
	store := state.NewStore()
	app := New(store, manager, Config{})

	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	replicas := int32(1)
	store.UpsertDeployment("dev", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}})
	manager.SetGenericResourceFixtureForTest("apps/replicasets", "dev", &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "ReplicaSet",
		"metadata": map[string]interface{}{
			"name":            "frontend-7d8f6d4c5b",
			"namespace":       "default",
			"resourceVersion": "1",
			"ownerReferences": []interface{}{map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "name": "frontend"}},
		},
	}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "frontend-7d8f6d4c5b"}}}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "web"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	details, ok := store.PodDetailsByKey(state.PodKey{Cluster: "dev", Namespace: "default", Name: "frontend-abc"}, time.Now())
	if !ok {
		t.Fatalf("expected pod details")
	}
	app.activePod = details
	app.screen = screenPodDetails

	drainCmd(t, app, app.updatePodDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}))
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.pendingAction, pendingActionResourceJump; got != want {
		t.Fatalf("pendingAction = %d, want %d", got, want)
	}
	labels := make(map[string]bool, len(app.actionPickerOptions))
	for _, option := range app.actionPickerOptions {
		labels[option.Label] = true
	}
	for _, want := range []string{
		"ReplicaSet default/frontend-7d8f6d4c5b · dev",
		"Deployment default/frontend · dev",
		"Node node-a · dev",
	} {
		if !labels[want] {
			t.Fatalf("missing %q in %#v", want, labels)
		}
	}
}

func TestDeploymentListJumpToDependentsOpensSelectedPod(t *testing.T) {
	manager := newTestManager(t)
	manager.SetDiscoveredResourcesForTest("dev", []cluster.ResourceKind{{APIGroup: "apps", Version: "v1", Resource: "replicasets", Kind: "ReplicaSet", Namespaced: true}})
	store := state.NewStore()
	app := New(store, manager, Config{})

	replicas := int32(1)
	store.UpsertDeployment("dev", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}}}})
	manager.SetGenericResourceFixtureForTest("apps/replicasets", "dev", &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "ReplicaSet",
		"metadata": map[string]interface{}{
			"name":            "frontend-7d8f6d4c5b",
			"namespace":       "default",
			"resourceVersion": "1",
			"ownerReferences": []interface{}{map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "name": "frontend"}},
		},
	}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default", Labels: map[string]string{"app": "frontend"}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "frontend-7d8f6d4c5b"}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})

	app.activeResource = builtinDeploymentResourceKind()
	app.screen = screenResourceList
	app.refreshResourceList(time.Now())

	drainCmd(t, app, app.updateResourceListKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}}))
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	labels := make([]string, 0, len(app.actionPickerOptions))
	podIndex := -1
	for idx, option := range app.actionPickerOptions {
		labels = append(labels, option.Label)
		if option.Label == "Pod default/frontend-abc · dev" {
			podIndex = idx
		}
	}
	if podIndex < 0 {
		t.Fatalf("missing pod target in %#v", labels)
	}
	for step := 0; step < podIndex; step++ {
		app.updateActionPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	app.updateActionPickerKeys(tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := app.screen, screenPodDetails; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.activePod.Row.Name, "frontend-abc"; got != want {
		t.Fatalf("pod = %q, want %q", got, want)
	}
}

func TestResourceDetailDetailsPaneNavigationWorks(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	app.height = 16
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-abc", Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	details, ok := store.NodeDetailsByKey(state.NodeKey{Cluster: "dev", Name: "node-a"}, time.Now())
	if !ok {
		t.Fatalf("expected node details")
	}
	app.activeNode = details
	app.activeNodePods = app.nodeAssociatedPods(time.Now())
	app.screen = screenResourceDetails
	_ = app.View()

	if got, want := app.textViewport.YOffset, 0; got != want {
		t.Fatalf("initial offset = %d, want %d", got, want)
	}
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.textViewport.YOffset, 1; got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
}

func TestResourceDetailPodsPaneNavigationWorks(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	app.height = 24
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	for idx := 0; idx < 3; idx++ {
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("frontend-%d", idx), Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	}
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	details, ok := store.NodeDetailsByKey(state.NodeKey{Cluster: "dev", Name: "node-a"}, time.Now())
	if !ok {
		t.Fatalf("expected node details")
	}
	app.activeNode = details
	app.activeNodePods = app.nodeAssociatedPods(time.Now())
	app.screen = screenResourceDetails
	_ = app.View()
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if got, want := app.detailPodTable.SelectedIndex(), 0; got != want {
		t.Fatalf("initial selected index = %d, want %d", got, want)
	}
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.detailPodTable.SelectedIndex(), 1; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
}

func TestResourceDetailPaneSwitchPreservesPaneState(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.width = 120
	app.height = 16
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	for idx := 0; idx < 3; idx++ {
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("frontend-%d", idx), Namespace: "default"}, Spec: corev1.PodSpec{NodeName: "node-a"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	}
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	details, ok := store.NodeDetailsByKey(state.NodeKey{Cluster: "dev", Name: "node-a"}, time.Now())
	if !ok {
		t.Fatalf("expected node details")
	}
	app.activeNode = details
	app.activeNodePods = app.nodeAssociatedPods(time.Now())
	app.screen = screenResourceDetails
	_ = app.View()

	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.textViewport.YOffset, 1; got != want {
		t.Fatalf("details offset = %d, want %d", got, want)
	}

	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.detailPodTable.SelectedIndex(), 1; got != want {
		t.Fatalf("pod index = %d, want %d", got, want)
	}

	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	if got, want := app.textViewport.YOffset, 1; got != want {
		t.Fatalf("details offset after pane switch = %d, want %d", got, want)
	}

	app.updateResourceDetailKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	if got, want := app.detailPodTable.SelectedIndex(), 1; got != want {
		t.Fatalf("pod index after pane switch = %d, want %d", got, want)
	}
}

func TestTypedGenericDaemonSetDetailsRender(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "DaemonSets", Kind: "DaemonSet", Resource: "daemonsets", APIGroup: "apps", Version: "v1", Namespaced: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "node-agent", Namespace: "kube-system", Cluster: "dev", Ready: "3/3", Status: "Running", Age: "15m"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "DaemonSet",
			"metadata": map[string]interface{}{
				"name":      "node-agent",
				"namespace": "kube-system",
				"labels":    map[string]interface{}{"app": "node-agent"},
			},
			"spec": map[string]interface{}{
				"selector":       map[string]interface{}{"matchLabels": map[string]interface{}{"app": "node-agent"}},
				"updateStrategy": map[string]interface{}{"type": "RollingUpdate"},
				"template": map[string]interface{}{
					"metadata": map[string]interface{}{"labels": map[string]interface{}{"app": "node-agent"}},
					"spec":     map[string]interface{}{"containers": []interface{}{map[string]interface{}{"name": "agent", "image": "ghcr.io/acme/agent:1.0.0"}}},
				},
			},
			"status": map[string]interface{}{"desiredNumberScheduled": int64(3), "currentNumberScheduled": int64(3), "updatedNumberScheduled": int64(3), "numberReady": int64(3)},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	for _, fragment := range []string{"DaemonSet", "Update strategy:", "RollingUpdate", "Rollout", "Desired:", "Pod template", "Containers", "agent", "Resource context"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestTypedGenericConfigMapDetailsRender(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "ConfigMaps", Kind: "ConfigMap", Resource: "configmaps", Version: "v1", Namespaced: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "app-config", Namespace: "default", Cluster: "dev", Status: "Active", Age: "5m"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "app-config",
				"namespace": "default",
				"labels":    map[string]interface{}{"app": "web"},
			},
			"immutable": true,
			"data": map[string]interface{}{
				"app.yaml": "port: 8080\nmode: prod\n",
				"feature":  "enabled",
			},
			"binaryData": map[string]interface{}{
				"ca.crt": "YWJjZA==",
			},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	for _, fragment := range []string{"ConfigMap", "Immutable:", "true", "Data", "app.yaml", "Binary data", "ca.crt", "Resource context"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestTypedGenericHorizontalPodAutoscalerDetailsRender(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "HorizontalPodAutoscalers", Kind: "HorizontalPodAutoscaler", Resource: "horizontalpodautoscalers", APIGroup: "autoscaling", Version: "v2", Namespaced: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "web", Namespace: "default", Cluster: "dev", Status: "ScalingActive", Age: "12m"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "autoscaling/v2",
			"kind":       "HorizontalPodAutoscaler",
			"metadata": map[string]interface{}{
				"name":      "web",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"scaleTargetRef": map[string]interface{}{"apiVersion": "apps/v1", "kind": "Deployment", "name": "web"},
				"minReplicas":    int64(2),
				"maxReplicas":    int64(6),
				"metrics": []interface{}{
					map[string]interface{}{"type": "Resource", "resource": map[string]interface{}{"name": "cpu", "target": map[string]interface{}{"type": "Utilization", "averageUtilization": int64(70)}}},
				},
				"behavior": map[string]interface{}{
					"scaleUp": map[string]interface{}{"stabilizationWindowSeconds": int64(60), "selectPolicy": "Max", "policies": []interface{}{map[string]interface{}{"type": "Pods", "value": int64(4), "periodSeconds": int64(60)}}},
				},
			},
			"status": map[string]interface{}{
				"currentReplicas": int64(3),
				"desiredReplicas": int64(4),
				"currentMetrics": []interface{}{
					map[string]interface{}{"type": "Resource", "resource": map[string]interface{}{"name": "cpu", "current": map[string]interface{}{"averageUtilization": int64(55), "averageValue": "220m"}}},
				},
				"conditions": []interface{}{
					map[string]interface{}{"type": "AbleToScale", "status": "True", "reason": "ScaleDownStabilized"},
				},
			},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	for _, fragment := range []string{"HorizontalPodAutoscaler", "Scale target:", "Deployment/web", "Metrics", "cpu", "current=", "Behavior", "scaleUp", "Conditions"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestTypedGenericPodDisruptionBudgetDetailsRender(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "PodDisruptionBudgets", Kind: "PodDisruptionBudget", Resource: "poddisruptionbudgets", APIGroup: "policy", Version: "v1", Namespaced: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "web-pdb", Namespace: "default", Cluster: "dev", Status: "Healthy", Age: "20m"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "policy/v1",
			"kind":       "PodDisruptionBudget",
			"metadata": map[string]interface{}{
				"name":      "web-pdb",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"selector":                   map[string]interface{}{"matchLabels": map[string]interface{}{"app": "web"}},
				"minAvailable":               "80%",
				"unhealthyPodEvictionPolicy": "AlwaysAllow",
			},
			"status": map[string]interface{}{
				"currentHealthy":     int64(5),
				"desiredHealthy":     int64(4),
				"expectedPods":       int64(5),
				"disruptionsAllowed": int64(1),
				"disruptedPods":      map[string]interface{}{"web-1": "2026-04-22T10:00:00Z"},
			},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	for _, fragment := range []string{"PodDisruptionBudget", "Min available:", "80%", "Status", "Disruptions allowed:", "Resource context"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestTypedGenericPersistentVolumeClaimDetailsRender(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "PersistentVolumeClaims", Kind: "PersistentVolumeClaim", Resource: "persistentvolumeclaims", Version: "v1", Namespaced: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "data-web-0", Namespace: "default", Cluster: "dev", Status: "Bound", Age: "1h"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "PersistentVolumeClaim",
			"metadata": map[string]interface{}{
				"name":      "data-web-0",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"accessModes":      []interface{}{"ReadWriteOnce"},
				"storageClassName": "fast-ssd",
				"volumeMode":       "Filesystem",
				"volumeName":       "pvc-1234",
				"resources":        map[string]interface{}{"requests": map[string]interface{}{"storage": "10Gi"}},
				"selector":         map[string]interface{}{"matchLabels": map[string]interface{}{"app": "db"}},
				"dataSource":       map[string]interface{}{"apiGroup": "snapshot.storage.k8s.io", "kind": "VolumeSnapshot", "name": "db-snap"},
			},
			"status": map[string]interface{}{
				"phase":    "Bound",
				"capacity": map[string]interface{}{"storage": "10Gi"},
				"conditions": []interface{}{
					map[string]interface{}{"type": "FileSystemResizePending", "status": "True", "reason": "Waiting"},
				},
			},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	for _, fragment := range []string{"PersistentVolumeClaim", "Access modes:", "ReadWriteOnce", "Binding/source", "VolumeSnapshot/db-snap", "Conditions", "Resource context"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
}

func TestCustomResourceDetailsSeparateStatusSpecMetadata(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 140
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Widgets", Kind: "Widget", Resource: "widgets", APIGroup: "apps.example.com", Version: "v1", Namespaced: true, Custom: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "blue-widget", Namespace: "default", Cluster: "dev", Ready: "True", Status: "Ready", Age: "8m"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "apps.example.com/v1",
			"kind":       "Widget",
			"metadata": map[string]interface{}{
				"name":            "blue-widget",
				"namespace":       "default",
				"generation":      int64(7),
				"resourceVersion": "42",
				"annotations":     map[string]interface{}{"owner": "team-a"},
				"labels":          map[string]interface{}{"app": "widget"},
				"managedFields": []interface{}{
					map[string]interface{}{
						"manager": "kubectl",
						"fieldsV1": map[string]interface{}{
							"f:spec":   map[string]interface{}{},
							"f:status": map[string]interface{}{},
						},
					},
				},
			},
			"spec": map[string]interface{}{
				"replicas": int64(3),
				"image":    "ghcr.io/acme/widget:v1",
			},
			"status": map[string]interface{}{
				"phase":              "Ready",
				"url":                "https://widget.dev",
				"observedGeneration": int64(7),
				"conditions": []interface{}{
					map[string]interface{}{"type": "Ready", "status": "True", "reason": "Healthy"},
				},
			},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	for _, fragment := range []string{"Status", "Phase:", "Ready", "Url:", "https://widget.dev", "Spec", "replicas: 3", "Metadata", "Generation:", "Resource version:"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in\n%s", fragment, rendered)
		}
	}
	if strings.Contains(rendered, "managedFields") || strings.Contains(rendered, "fieldsV1") || strings.Contains(rendered, "f:spec") {
		t.Fatalf("expected managed fields stripped from custom resource details:\n%s", rendered)
	}
	statusIndex := strings.Index(rendered, "Status")
	specIndex := strings.Index(rendered, "Spec")
	metadataIndex := strings.Index(rendered, "Metadata")
	if !(statusIndex >= 0 && specIndex > statusIndex && metadataIndex > statusIndex) {
		t.Fatalf("expected status before spec and metadata in\n%s", rendered)
	}
}

func TestCustomResourceDetailsUseResponsiveGrid(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 160
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Widgets", Kind: "Widget", Resource: "widgets", APIGroup: "apps.example.com", Version: "v1", Namespaced: true, Custom: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row: cluster.GenericResourceRow{Name: "blue-widget", Namespace: "default", Cluster: "dev", Ready: "True", Status: "Ready", Age: "8m"},
		Object: &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "apps.example.com/v1",
			"kind":       "Widget",
			"metadata": map[string]interface{}{
				"name":        "blue-widget",
				"namespace":   "default",
				"generation":  int64(7),
				"annotations": map[string]interface{}{"owner": "team-a"},
				"labels":      map[string]interface{}{"app": "widget"},
			},
			"spec": map[string]interface{}{
				"replicas": int64(3),
				"image":    "ghcr.io/acme/widget:v1",
			},
			"status": map[string]interface{}{
				"phase":              "Ready",
				"url":                "https://widget.dev",
				"observedGeneration": int64(7),
				"conditions": []interface{}{
					map[string]interface{}{"type": "Ready", "status": "True", "reason": "Healthy"},
				},
			},
		}},
	}

	rendered := stripUsageANSI(app.renderGenericResourceDetails())
	for _, fragment := range []string{"Overview", "Runtime", "Status", "Conditions", "Metadata", "Context"} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected grouped custom resource layout fragment %q:\n%s", fragment, rendered)
		}
	}
	metadataIndex := strings.Index(rendered, "Metadata")
	specIndex := strings.Index(rendered, "Spec")
	if !(metadataIndex > 0 && specIndex > metadataIndex) {
		t.Fatalf("expected full-width metadata section before spec:\n%s", rendered)
	}
}

func TestConfirmActionSupportsViewportScrolling(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 100
	app.height = 8
	app.screen = screenResourceDetails
	app.openConfirmAction(strings.Join([]string{"line1", "line2", "line3", "line4", "line5", "line6", "line7"}, "\n"), func() tea.Cmd { return nil })

	_ = app.View()
	app.updateConfirmActionKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got, want := app.textViewport.YOffset, 1; got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
}

func TestMouseWheelScrollsTextViewport(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 100
	app.height = 8
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "revisions", Resource: "revisions", APIGroup: "serving.knative.dev", Version: "v1", Namespaced: true, Custom: true}
	app.activeGenericDetails = cluster.GenericResourceDetails{
		Row:    cluster.GenericResourceRow{Name: "api-0001", Namespace: "serving", Cluster: "dev", Ready: "True", Status: "Ready", Age: "5m"},
		YAML:   strings.Join([]string{"a: 1", "b: 2", "c: 3", "d: 4", "e: 5", "f: 6", "g: 7", "h: 8", "i: 9"}, "\n"),
		Object: &unstructured.Unstructured{Object: map[string]interface{}{"apiVersion": "serving.knative.dev/v1"}},
	}

	_ = app.View()
	app.updateMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if got, want := app.textViewport.YOffset, 3; got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
}

func TestMouseWheelScrollsResourceTable(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	for idx := 0; idx < 10; idx++ {
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend-" + strconv.Itoa(idx), Namespace: "default"}}
		store.UpsertDeployment("dev", deployment)
	}
	app.height = 10
	app.refreshResourceList(time.Now())

	app.updateMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if got, want := app.resourceTable.SelectedIndex(), 3; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
}

func TestOpenNamespacePickerUsesCurrentScreenNamespaces(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.screen = screenPods
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "alpha"}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "beta"}})

	cmd := app.openNamespacePicker()
	if cmd != nil {
		t.Fatalf("expected picker command nil")
	}
	if got, want := app.screen, screenScopePicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.visibleScopeOptions[1].Value, "alpha"; got != want {
		t.Fatalf("scope option = %q, want %q", got, want)
	}
}

func TestContextScopeFiltersPodRows(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default"}})
	store.UpsertPod("prod", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default"}})
	app.contextScope = "dev"
	app.screen = screenPods
	app.refreshPods(time.Now())

	if got, want := app.visibleRows, 1; got != want {
		t.Fatalf("visibleRows = %d, want %d", got, want)
	}
	if got, want := app.currentContextLabel(), "dev"; got != want {
		t.Fatalf("context label = %q, want %q", got, want)
	}
}

func TestBuildResourceFinderItemsDeduplicatesResources(t *testing.T) {
	items := buildResourceFinderItems([]cluster.ResourceGroup{{Name: "Favourites", Resources: []cluster.ResourceKind{{ID: "/pods", Display: "Pods", Resource: "pods", Namespaced: true}}}, {Name: "Workloads", Resources: []cluster.ResourceKind{{ID: "/pods", Display: "Pods", Resource: "pods", Namespaced: true}, {ID: "serving.knative.dev/revisions", Display: "Revisions", Resource: "revisions", APIGroup: "serving.knative.dev", Namespaced: true}}}})
	if got, want := len(items), 2; got != want {
		t.Fatalf("items = %d, want %d", got, want)
	}
}

func TestFuzzyResourcesSortsStrongestResourceMatchFirst(t *testing.T) {
	resources := []cluster.ResourceKind{
		{ID: "example.dev/services", Display: "Backends", Kind: "Backend", Resource: "backends", APIGroup: "services.example.dev"},
		{ID: "/services", Display: "Services", Kind: "Service", Resource: "services"},
	}
	matched := fuzzyResources(resources, "serv")
	if got, want := matched[0].ID, "/services"; got != want {
		t.Fatalf("first resource = %q, want %q", got, want)
	}
}

func TestResourceFinderSortsStrongestResourceMatchFirst(t *testing.T) {
	items := []resourceFinderItem{
		{resource: cluster.ResourceKind{ID: "example.dev/services", Display: "Backends", Kind: "Backend", Resource: "backends", APIGroup: "services.example.dev"}, group: "Custom"},
		{resource: cluster.ResourceKind{ID: "/services", Display: "Services", Kind: "Service", Resource: "services"}, group: "Network"},
	}
	matched := fuzzyResourceFinderItems(items, "serv")
	if got, want := matched[0].resource.ID, "/services"; got != want {
		t.Fatalf("first resource = %q, want %q", got, want)
	}
}

func TestGroupResourceFuzzyFilterMovesCursorToStrongestMatch(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenGroupResources
	app.activeGroup = cluster.ResourceGroup{Name: "Network", Resources: []cluster.ResourceKind{
		{ID: "example.dev/services", Display: "Backends", Kind: "Backend", Resource: "backends", APIGroup: "services.example.dev"},
		{ID: "/services", Display: "Services", Kind: "Service", Resource: "services"},
	}}
	app.refreshGroupResources()
	app.navTable.MoveDown(1)
	app.filter.Activate()
	app.updateSearchPrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	app.updateSearchPrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	app.updateSearchPrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	app.updateSearchPrompt(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if got, want := app.navTable.SelectedIndex(), 0; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
	if got, want := app.visibleResources[0].ID, "/services"; got != want {
		t.Fatalf("first resource = %q, want %q", got, want)
	}
}

func TestOpenSelectedResourceFinderItemOpensResource(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.visibleResourceItems = []resourceFinderItem{{resource: cluster.ResourceKind{Display: "Revisions", Resource: "revisions", APIGroup: "serving.knative.dev", Version: "v1", Namespaced: true, Custom: true}}}
	app.navTable.SetRows(renderResourceFinderRows(app.visibleResourceItems, nil))
	app.screen = screenResourceFinder
	app.inputMode = inputModeResourceFinder
	app.filter.Activate()

	cmd := app.openSelectedResourceFinderItem()
	if cmd == nil {
		t.Fatal("expected generic resource list fetch command")
	}
	if !app.genericListLoading {
		t.Fatal("expected generic resource list loading state")
	}
	if got, want := app.screen, screenResourceList; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.activeResource.Resource, "revisions"; got != want {
		t.Fatalf("resource = %q, want %q", got, want)
	}
}

func TestMatchesStructuredFilterSupportsNegation(t *testing.T) {
	if !matchesStructuredFilter("running", "!error") {
		t.Fatalf("expected negated contains match")
	}
	if matchesStructuredFilter("running", "!run") {
		t.Fatalf("expected negated contains miss")
	}
	if !matchesStructuredFilter("ready", "!=error") {
		t.Fatalf("expected not-exact match")
	}
	if matchesStructuredFilter("ready", "!=ready") {
		t.Fatalf("expected not-exact miss")
	}
	if !matchesStructuredFilter("frontend-api", "!*job*") {
		t.Fatalf("expected negated wildcard match")
	}
	if matchesStructuredFilter("frontend-api", "!*api*") {
		t.Fatalf("expected negated wildcard miss")
	}
}

func TestSortDirectionPickerAcceptsAAndD(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenTableSortDirectionPicker
	app.prevScreen = screenPods
	app.pendingSortColumnIndex = 2
	app.pendingSortColumnTitle = "NAME"

	app.updateTableSortDirectionPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if got, want := len(app.podTableSorts), 1; got != want {
		t.Fatalf("sort count = %d, want %d", got, want)
	}
	if app.podTableSorts[0].Desc {
		t.Fatalf("expected ascending sort")
	}

	app.screen = screenTableSortDirectionPicker
	app.prevScreen = screenPods
	app.pendingSortColumnIndex = 0
	app.pendingSortColumnTitle = "CONTEXT"
	app.updateTableSortDirectionPickerKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if got, want := len(app.podTableSorts), 2; got != want {
		t.Fatalf("sort count = %d, want %d", got, want)
	}
	if !app.podTableSorts[1].Desc {
		t.Fatalf("expected descending sort")
	}
}

func maxRenderedLineWidth(rendered string) int {
	width := 0
	for _, line := range strings.Split(stripUsageANSI(rendered), "\n") {
		width = max(width, lipgloss.Width(line))
	}
	return width
}

func TestListScreensExposeFooterHints(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPods
	_, _, footer := app.currentView()
	if !strings.Contains(footer, "Y CSV") || !strings.Contains(footer, "f add-filter") {
		t.Fatalf("unexpected pod footer: %q", footer)
	}

	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	_, _, footer = app.currentView()
	if !strings.Contains(footer, "S scale") || !strings.Contains(footer, "R restart") {
		t.Fatalf("unexpected resource footer: %q", footer)
	}
}

func TestListHeaderShowsUsageStatus(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPods
	app.podUsageListLoading = true
	header := app.listScreenCombinedHeader(components.StatusBarState{})
	if !strings.Contains(header, "usage:loading") {
		t.Fatalf("expected usage loading in header: %q", header)
	}

	app.podUsageListLoading = false
	app.podUsageListFetchedAt = time.Now().Add(-8 * time.Second)
	header = app.listScreenCombinedHeader(components.StatusBarState{})
	if !strings.Contains(header, "usage:8s") {
		t.Fatalf("expected usage age in header: %q", header)
	}
}

func TestPodUsageSnapshotRefreshRendersTableGauge(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	app.width = 160
	app.height = 20
	app.screen = screenPods
	app.refreshPods(time.Now())

	key := state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}.String()
	app.podUsageListGeneration = 1
	app.Update(podUsageSnapshotMsg{scopeKey: app.podUsageScopeKey(), generation: app.podUsageListGeneration, usages: map[string]cluster.PodResourceUsage{
		key: {Key: state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}, CPUUsedMilli: 120, CPULimitMilli: 500, HasCPUUsage: true},
	}})
	_, body, _ := app.currentView()
	plain := stripUsageANSI(body)
	if !strings.Contains(plain, "120m/") || !strings.Contains(plain, "█") {
		t.Fatalf("expected usage gauge in body, got:\n%s", plain)
	}
}

func TestNodeUsageSnapshotRefreshRendersTableGauge(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	app.width = 160
	app.height = 20
	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	app.refreshResourceList(time.Now())

	key := state.NodeKey{Cluster: "dev", Name: "node-a"}.String()
	app.nodeUsageListGeneration = 1
	app.Update(nodeUsageSnapshotMsg{scopeKey: app.nodeUsageScopeKey(), generation: app.nodeUsageListGeneration, usages: map[string]cluster.NodeResourceUsage{
		key: {Key: state.NodeKey{Cluster: "dev", Name: "node-a"}, CPUUsedMilli: 1200, CPUAllocatableMilli: 4000, HasCPUUsage: true},
	}})
	_, body, _ := app.currentView()
	if !strings.Contains(stripUsageANSI(body), "1200m/4") {
		t.Fatalf("expected usage gauge in body, got:\n%s", stripUsageANSI(body))
	}
}

func TestNodeUsageSnapshotAppliesUnderUnrelatedPodChurn(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})
	app.width = 160
	app.height = 20
	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	app.refreshResourceList(time.Now())

	key := state.NodeKey{Cluster: "dev", Name: "node-a"}.String()
	app.nodeUsageListGeneration = 1
	msg := nodeUsageSnapshotMsg{scopeKey: app.nodeUsageScopeKey(), generation: app.nodeUsageListGeneration, usages: map[string]cluster.NodeResourceUsage{
		key: {Key: state.NodeKey{Cluster: "dev", Name: "node-a"}, CPUUsedMilli: 1200, CPUAllocatableMilli: 4000, HasCPUUsage: true},
	}}

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})

	app.Update(msg)
	if app.nodeUsageListFetchedAt.IsZero() {
		t.Fatalf("expected node usage snapshot accepted under unrelated pod churn")
	}
	_, body, _ := app.currentView()
	if !strings.Contains(stripUsageANSI(body), "1200m/4") {
		t.Fatalf("expected usage gauge in body, got:\n%s", stripUsageANSI(body))
	}
}

func TestPodUsageSnapshotAppliesUnderUnrelatedNodeChurn(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	app.width = 160
	app.height = 20
	app.screen = screenPods
	app.refreshPods(time.Now())

	key := state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}.String()
	app.podUsageListGeneration = 1
	msg := podUsageSnapshotMsg{scopeKey: app.podUsageScopeKey(), generation: app.podUsageListGeneration, usages: map[string]cluster.PodResourceUsage{
		key: {Key: state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}, CPUUsedMilli: 120, CPULimitMilli: 500, HasCPUUsage: true},
	}}

	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}})

	app.Update(msg)
	if app.podUsageListFetchedAt.IsZero() {
		t.Fatalf("expected pod usage snapshot accepted under unrelated node churn")
	}
	row, ok := app.podRowAt(0, time.Now())
	if !ok {
		t.Fatalf("expected pod row")
	}
	if got := stripUsageANSI(app.podCPUCell(row)); !strings.Contains(got, "120m/") {
		t.Fatalf("expected rendered pod cpu cell, got %q", got)
	}
}

func TestRunOpenPodLogsOpensContainerPickerForMultiContainerPod(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "toolbox"},
		Pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}, {Name: "sidecar"}}}},
	}

	cmd := app.runOpenLogs()
	if cmd != nil {
		t.Fatalf("expected picker, not log load command")
	}
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.pendingAction, pendingActionPodLogsSources; got != want {
		t.Fatalf("pendingAction = %d, want %d", got, want)
	}
}

func TestRunOpenPodLogsStartsLoadingScreenForSingleContainerPod(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "toolbox"},
		Pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}},
	}

	cmd := app.runOpenLogs()
	if cmd == nil {
		t.Fatalf("expected async log load command")
	}
	if got, want := app.screen, screenLogs; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if !app.logLoading {
		t.Fatalf("expected log loading state")
	}
	if !strings.Contains(app.logTitle, "pod logs") {
		t.Fatalf("logTitle = %q", app.logTitle)
	}
}

func TestRunOpenDeploymentLogsPromptsForContainerWhenMultiContainer(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.activeDeployment = state.DeploymentDetails{
		Row:        state.DeploymentRow{Cluster: "dev", Namespace: "default", Name: "frontend"},
		Deployment: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}, {Name: "metrics"}}}}}},
	}

	cmd := app.runOpenLogs()
	if cmd != nil {
		t.Fatalf("expected picker, not log load command")
	}
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := app.pendingAction, pendingActionDeploymentLogsSources; got != want {
		t.Fatalf("pendingAction = %d, want %d", got, want)
	}
}

func TestLogsResultMsgReplacesLoadingPlaceholder(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.activePod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "toolbox"},
		Pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}},
	}
	cmd := app.runOpenLogs()
	if cmd == nil {
		t.Fatalf("expected async log load command")
	}
	token := app.logRequestToken

	app.Update(logsResultMsg{Token: token, Replace: true, Entries: []logEntry{{UniqueKey: "k1", Message: "line1"}, {UniqueKey: "k2", Message: "line2"}}})
	if app.logLoading {
		t.Fatalf("expected loading cleared")
	}
	if got, want := len(app.logEntries), 2; got != want {
		t.Fatalf("entry count = %d, want %d", got, want)
	}
}

func TestLogsResultAutoFollowsWhenViewportAtBottom(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenLogs
	app.width = 120
	app.height = 8
	app.logRequestToken = 7
	app.logEntries = []logEntry{{UniqueKey: "k1", Message: "line1"}, {UniqueKey: "k2", Message: "line2"}, {UniqueKey: "k3", Message: "line3"}, {UniqueKey: "k4", Message: "line4"}, {UniqueKey: "k5", Message: "line5"}}
	app.logEntriesVersion = 1
	_ = app.renderTextViewport(app.renderLogs(), 2)
	app.textViewport.GotoBottom()
	if !app.textViewport.AtBottom() {
		t.Fatalf("expected viewport at bottom before append")
	}

	app.Update(logsResultMsg{Token: 7, Replace: false, Cursor: time.Now(), Entries: []logEntry{{UniqueKey: "k6", Message: "line6"}, {UniqueKey: "k7", Message: "line7"}}})
	_ = app.renderTextViewport(app.renderLogs(), 2)
	if !app.textViewport.AtBottom() {
		t.Fatalf("expected viewport to follow appended logs")
	}
}

func TestNodeLogPickerResultOpensActionPicker(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenResourceDetails
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	app.activeNode = state.NodeDetails{
		Row:  state.NodeRow{Key: state.NodeKey{Cluster: "dev", Name: "node-a"}, Cluster: "dev", Name: "node-a"},
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
	}
	app.nodeLogPickerToken = 9

	app.Update(nodeLogPickerResultMsg{Token: 9, Key: app.activeNode.Row.Key, Title: "select node log", Entries: []cluster.NodeLogEntry{{Path: "cloud-init.log", Name: "cloud-init.log"}}})
	if got, want := app.screen, screenActionPicker; got != want {
		t.Fatalf("screen = %d, want %d", got, want)
	}
	if got, want := len(app.actionPickerOptions), 1; got != want {
		t.Fatalf("options = %d, want %d", got, want)
	}
}

func TestActionPickerRefreshMovesSelectionToTop(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPodDetails
	app.openActionPicker("select container", "", pendingActionExecPod, []actionOption{{Label: "b"}, {Label: "a"}})
	app.navTable.MoveDown(1)
	app.refreshActionPicker()
	if got, want := app.navTable.SelectedIndex(), 0; got != want {
		t.Fatalf("selected index = %d, want %d", got, want)
	}
}

func TestRenderLogsIncludesSourceLabelAndHonorsTimestampToggle(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 120
	app.logShowTimestamps = true
	app.logEntries = []logEntry{{UniqueKey: "1", TimestampText: "2026-04-22T12:00:00Z", HasTimestamp: true, SourceKey: "pod-a/api", SourceLabel: "pod-a/api", Message: "started"}}

	rendered := stripUsageANSI(app.renderLogs())
	if !strings.Contains(rendered, "2026-04-22T12:00:00Z") || !strings.Contains(rendered, "[pod-a/api]") {
		t.Fatalf("rendered = %q", rendered)
	}

	app.logShowTimestamps = false
	rendered = stripUsageANSI(app.renderLogs())
	if strings.Contains(rendered, "2026-04-22T12:00:00Z") {
		t.Fatalf("timestamp still present: %q", rendered)
	}
	if !strings.Contains(rendered, "[pod-a/api]") {
		t.Fatalf("source label missing: %q", rendered)
	}
}

func TestRenderLogBodyPrettyPrintsJSONWhenWrapEnabled(t *testing.T) {
	rendered := stripUsageANSI(renderLogBody(`{"level":"info","msg":"hello","count":2}`, true, 40))
	for _, fragment := range []string{"\n", `"count"`, `"msg"`, `"hello"`} {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("missing %q in %q", fragment, rendered)
		}
	}
}

func TestRenderLogBodyTruncatesHugeLine(t *testing.T) {
	message := strings.Repeat("x", maxRenderedLogRunes+512)
	rendered := renderLogBody(message, false, 120)
	if !strings.Contains(rendered, "[truncated]") {
		t.Fatalf("expected truncation marker")
	}
	if got, want := len([]rune(rendered)), maxRenderedLogRunes+14; got != want {
		t.Fatalf("rendered runes = %d, want %d", got, want)
	}
}

func TestFilteredLogEntriesSupportFuzzyAndExact(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.logEntries = []logEntry{{UniqueKey: "1", Message: "frontend started"}, {UniqueKey: "2", Message: "backend started"}}

	app.logFilterQuery = "front"
	if got, want := len(app.filteredLogEntries()), 1; got != want {
		t.Fatalf("fuzzy match count = %d, want %d", got, want)
	}

	app.logFilterQuery = "=backend"
	if got, want := len(app.filteredLogEntries()), 1; got != want {
		t.Fatalf("exact match count = %d, want %d", got, want)
	}
}

func TestOpenLogExactFilterStoresExactQuery(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenLogs
	app.openLogExactFilter()
	app.filter.SetValue("error")
	app.updateFilter(tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := app.logFilterQuery, "=error"; got != want {
		t.Fatalf("logFilterQuery = %q, want %q", got, want)
	}
}

func TestLogPauseToggleStopsAutoRefreshCmd(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenLogs
	app.logTarget = logTargetPod
	app.logRange = logRangeLive
	app.logFetchedAt = time.Now().Add(-2 * time.Second)
	if cmd := app.maybeRefreshLogsCmd(time.Now()); cmd == nil {
		t.Fatalf("expected live refresh cmd")
	}
	app.logAutoRefreshPaused = true
	if cmd := app.maybeRefreshLogsCmd(time.Now()); cmd != nil {
		t.Fatalf("expected paused live refresh to stop")
	}
}

func TestYankLogsCopiesRenderedLogs(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 120
	app.logEntries = []logEntry{{UniqueKey: "1", Message: "hello world"}}
	captured := ""
	previousClipboard := writeClipboard
	writeClipboard = func(value string) error {
		captured = value
		return nil
	}
	defer func() { writeClipboard = previousClipboard }()

	app.yankLogs()
	if got, want := captured, "hello world"; got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}
}

func TestSaveLogsToPathWritesRenderedLogs(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.width = 120
	app.logEntries = []logEntry{{UniqueKey: "1", Message: "saved line"}}
	path := t.TempDir() + "/logs.txt"
	cmd := app.saveLogsToPath(path)
	if cmd == nil {
		t.Fatalf("expected async save command")
	}
	msg := cmd()
	res, ok := msg.(actionResultMsg)
	if !ok {
		t.Fatalf("msg type %T", msg)
	}
	if res.err != nil {
		t.Fatalf("save error: %v", res.err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if got, want := string(content), "saved line\n"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestLogKeysUseSpaceForPauseAndDoNotUseRForRefresh(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenLogs
	app.logTarget = logTargetPod
	app.logRange = logRangeLive
	app.logAutoRefreshPaused = false

	cmd := app.updateLogKeys(tea.KeyMsg{Type: tea.KeySpace})
	if cmd != nil {
		t.Fatalf("expected nil cmd toggling pause")
	}
	if !app.logAutoRefreshPaused {
		t.Fatalf("expected paused")
	}

	cmd = app.updateLogKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd != nil {
		t.Fatalf("expected r to be noop on logs screen")
	}
}

func TestLogFooterShowsNewKeyHints(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenLogs
	app.logTarget = logTargetPod
	footer := app.logFooter()
	for _, fragment := range []string{"u refresh", "space pause", "y yank", "s save"} {
		if !strings.Contains(footer, fragment) {
			t.Fatalf("missing %q in footer %q", fragment, footer)
		}
	}
}

func TestDeploymentListShouldNotRefreshOnUnrelatedPodChurn(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	replicas := int32(3)
	store.UpsertDeployment("dev", &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "default", ResourceVersion: "1"}, Spec: appsv1.DeploymentSpec{Replicas: &replicas}, Status: appsv1.DeploymentStatus{ReadyReplicas: 3, UpdatedReplicas: 3, AvailableReplicas: 3}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "1"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "main"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})

	app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps", Namespaced: true}
	app.screen = screenResourceList
	app.refreshResourceList(time.Now())
	if app.shouldRefresh(time.Now()) {
		t.Fatalf("unexpected immediate refresh")
	}

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "2"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "main"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	if app.shouldRefresh(time.Now()) {
		t.Fatalf("deployment list should ignore pod-only churn")
	}
}

func TestGenericResourceListIgnoresUnrelatedManagerChurn(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.activeResource = cluster.ResourceKind{ID: "serving.knative.dev/services", Display: "Services", Resource: "services", APIGroup: "serving.knative.dev", Version: "v1", Kind: "Service", Namespaced: true, Custom: true}
	app.screen = screenResourceList
	app.lastManagerVersion = manager.GenericResourceVersion(app.activeResource.ID)
	manager.BumpVersionForTest()
	if app.shouldRefresh(time.Now()) {
		t.Fatalf("generic list should ignore unrelated manager churn")
	}
}

func TestGenericResourceListAppliesChurnWhenCatalogAndGenericVersionsCoincide(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	resource := cluster.ResourceKind{ID: "surfsk8s.dev/widgets", Display: "Widgets", Resource: "widgets", APIGroup: "surfsk8s.dev", Version: "v1alpha1", Kind: "Widget", Namespaced: true, Custom: true}
	app.activeResource = resource
	app.screen = screenResourceList
	now := time.Now()
	alpha := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "alpha"}, Cluster: "dev", Namespace: "default", Name: "alpha"}
	app.applyGenericListRows([]cluster.GenericResourceRow{alpha}, now, resource.ID)
	app.lastManagerVersion = 0

	beta := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "surfsk8s.dev/v1alpha1",
		"kind":       "Widget",
		"metadata": map[string]interface{}{
			"name": "beta", "namespace": "default", "resourceVersion": "2",
		},
	}}
	manager.SetGenericResourceFixtureForTest(resource.ID, "dev", beta)
	manager.BumpVersionForTest()
	if got, want := manager.Version(), manager.GenericResourceVersion(resource.ID); got != want {
		t.Fatalf("test requires coincident versions: catalog=%d generic=%d", got, want)
	}

	app.refreshResourceList(now.Add(time.Second))
	if _, ok := app.genericRowsByKey[(cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "beta"}).String()]; !ok {
		t.Fatal("matching generic churn was skipped when catalog and generic versions coincided")
	}
}

func TestGenericResourceListRefreshesOnMatchingGenericChurn(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.activeResource = cluster.ResourceKind{ID: "serving.knative.dev/services", Display: "Services", Resource: "services", APIGroup: "serving.knative.dev", Version: "v1", Kind: "Service", Namespaced: true, Custom: true}
	app.screen = screenResourceList
	app.lastManagerVersion = manager.GenericResourceVersion(app.activeResource.ID)
	manager.BumpGenericResourceVersionForTest(app.activeResource.ID)
	if !app.shouldRefresh(time.Now()) {
		t.Fatalf("generic list should refresh on matching generic resource churn")
	}
}

func TestGenericResourceDetailsIgnoreUnrelatedSameKindChurn(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	resource := cluster.ResourceKind{ID: "serving.knative.dev/services", Display: "Services", Resource: "services", APIGroup: "serving.knative.dev", Version: "v1", Kind: "Service", Namespaced: true, Custom: true}
	key := cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "api"}
	object := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":              key.Name,
			"namespace":         key.Namespace,
			"resourceVersion":   "1",
			"creationTimestamp": time.Now().Add(-time.Hour).Format(time.RFC3339),
		},
		"status": map[string]interface{}{
			"conditions": []interface{}{map[string]interface{}{"type": "Ready", "status": "True", "reason": "Ready"}},
		},
	}}
	manager.SetGenericResourceFixtureForTest(resource.ID, key.Cluster, object)
	details, err := manager.GenericResourceDetails(context.Background(), resource, key, time.Now())
	if err != nil {
		t.Fatalf("generic details: %v", err)
	}
	app.activeResource = resource
	app.activeGenericDetails = details
	app.screen = screenResourceDetails
	app.lastManagerVersion = manager.GenericResourceVersion(resource.ID)
	app.genericDetailFetchedAt = time.Time{}

	manager.BumpGenericResourceVersionForTest(resource.ID)
	app.refreshGenericResourceDetails(time.Now())
	if !app.genericDetailFetchedAt.IsZero() {
		t.Fatalf("expected detail fetch to be skipped")
	}
	if got, want := app.activeGenericDetails.Row.ResourceVersion, "1"; got != want {
		t.Fatalf("resourceVersion = %q, want %q", got, want)
	}
}

func TestGenericResourceListKeepsVisibleCacheOnOffScopeDelta(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	resource := cluster.ResourceKind{
		ID:             "serving.knative.dev/services",
		Display:        "Services",
		Resource:       "services",
		APIGroup:       "serving.knative.dev",
		Version:        "v1",
		Kind:           "Service",
		Namespaced:     true,
		Custom:         true,
		PrinterColumns: []cluster.PrinterColumn{{Name: "URL", JSONPath: ".status.url"}},
	}
	app.activeResource = resource
	app.screen = screenResourceList
	app.namespace = "default"

	visible := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":              "api",
			"namespace":         "default",
			"resourceVersion":   "1",
			"creationTimestamp": time.Now().Add(-time.Hour).Format(time.RFC3339),
		},
		"status": map[string]interface{}{"url": "https://api.example.com"},
	}}
	hidden := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":              "hidden",
			"namespace":         "kube-system",
			"resourceVersion":   "1",
			"creationTimestamp": time.Now().Add(-time.Hour).Format(time.RFC3339),
		},
		"status": map[string]interface{}{"url": "https://hidden.example.com"},
	}}
	manager.SetGenericResourceFixtureForTest(resource.ID, "dev", visible)
	manager.SetGenericResourceFixtureForTest(resource.ID, "dev", hidden)
	app.refreshGenericResourceList(time.Now())

	row, ok := app.genericResourceRowAt(0, time.Now())
	if !ok {
		t.Fatalf("expected visible generic row")
	}
	cached := app.genericPrinterValues(row)
	if len(cached) != 1 {
		t.Fatalf("expected printer cache entry")
	}
	cacheKey := genericPrinterCacheKey(row)
	if _, ok := app.genericPrinterValueCache[cacheKey]; !ok {
		t.Fatalf("expected printer cache populated")
	}

	hidden.Object["metadata"].(map[string]interface{})["resourceVersion"] = "2"
	manager.SetGenericResourceFixtureForTest(resource.ID, "dev", hidden)
	app.refreshGenericResourceList(time.Now())

	if _, ok := app.genericPrinterValueCache[cacheKey]; !ok {
		t.Fatalf("expected visible printer cache preserved for off-scope delta")
	}
	if got, want := len(app.sortedGenericRows), 1; got != want {
		t.Fatalf("sorted rows = %d, want %d", got, want)
	}
	if got, want := app.genericRowsByKey[cluster.GenericResourceKey{Cluster: "dev", Namespace: "kube-system", Name: "hidden"}.String()].ResourceVersion, "2"; got != want {
		t.Fatalf("hidden resourceVersion = %q, want %q", got, want)
	}
}

func TestMaybeRefreshPodUsageListCmdDelaysInitialFetchAfterPodsRefresh(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.screen = screenPods
	openedAt := time.Now()
	app.lastTick = openedAt
	if cmd := app.maybeRefreshPodUsageListCmd(openedAt.Add(500 * time.Millisecond)); cmd != nil {
		t.Fatalf("expected nil cmd during initial pod usage delay")
	}
	if cmd := app.maybeRefreshPodUsageListCmd(openedAt.Add(podUsageInitialDelay + 100*time.Millisecond)); cmd == nil {
		t.Fatalf("expected pod usage cmd after initial delay")
	}
}

func TestMaybeRefreshPodUsageListCmdIgnoresPodChurnUntilInterval(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.screen = screenPods
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}})
	app.podUsageListScopeKey = app.podUsageScopeKey()
	app.podUsageListFetchedAt = time.Now()

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "default", ResourceVersion: "2"}})
	if cmd := app.maybeRefreshPodUsageListCmd(time.Now().Add(time.Second)); cmd != nil {
		t.Fatalf("expected nil cmd under pod churn before refresh interval")
	}
}

func TestTickRefreshContinuesWhileFilterActive(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	app.screen = screenPods
	app.width = 160
	app.height = 20
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "1"}})
	app.refreshPods(time.Now())
	app.filter.Activate()

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "default", ResourceVersion: "2"}})
	app.Update(tickMsg(time.Now().Add(time.Second)))
	if got, want := app.visibleRows, 2; got != want {
		t.Fatalf("visibleRows = %d, want %d", got, want)
	}
}

func TestPodUsageSnapshotAppliesUnderRelatedPodChurn(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "1"}})
	app.screen = screenPods
	app.refreshPods(time.Now())

	key := state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}.String()
	app.podUsageListGeneration = 1
	msg := podUsageSnapshotMsg{scopeKey: app.podUsageScopeKey(), generation: app.podUsageListGeneration, usages: map[string]cluster.PodResourceUsage{
		key: {Key: state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}, CPUUsedMilli: 120, CPULimitMilli: 500, HasCPUUsage: true},
	}}

	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "2"}})
	app.Update(msg)
	if app.podUsageListFetchedAt.IsZero() {
		t.Fatalf("expected pod usage snapshot accepted under related pod churn")
	}
}

func TestNodeUsageSnapshotAppliesUnderRelatedNodeChurn(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", ResourceVersion: "1"}})
	app.screen = screenResourceList
	app.activeResource = cluster.ResourceKind{Display: "Nodes", Resource: "nodes", Namespaced: false}
	app.refreshResourceList(time.Now())

	key := state.NodeKey{Cluster: "dev", Name: "node-a"}.String()
	app.nodeUsageListGeneration = 1
	msg := nodeUsageSnapshotMsg{scopeKey: app.nodeUsageScopeKey(), generation: app.nodeUsageListGeneration, usages: map[string]cluster.NodeResourceUsage{
		key: {Key: state.NodeKey{Cluster: "dev", Name: "node-a"}, CPUUsedMilli: 1200, CPUAllocatableMilli: 4000, HasCPUUsage: true},
	}}

	store.UpsertNode("dev", &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", ResourceVersion: "2"}})
	app.Update(msg)
	if app.nodeUsageListFetchedAt.IsZero() {
		t.Fatalf("expected node usage snapshot accepted under related node churn")
	}
}

func TestPodUsageSnapshotDefersCellCacheUntilVisibleRowsNeedIt(t *testing.T) {
	manager := newTestManager(t)
	store := state.NewStore()
	app := New(store, manager, Config{})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default", ResourceVersion: "1"}, Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "main"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Ready: true}}}})
	app.screen = screenPods
	app.refreshPods(time.Now())
	app.podUsageListGeneration = 1
	msg := podUsageSnapshotMsg{scopeKey: app.podUsageScopeKey(), generation: app.podUsageListGeneration, usages: map[string]cluster.PodResourceUsage{
		state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"}.String(): {
			Key:             state.PodKey{Cluster: "dev", Namespace: "default", Name: "api"},
			CPUUsedMilli:    120,
			HasCPUUsage:     true,
			MemoryUsedBytes: 256 * 1024 * 1024,
			HasMemoryUsage:  true,
		},
	}}
	app.Update(msg)
	if app.podUsageCellByKey != nil {
		t.Fatalf("expected lazy pod usage cache")
	}
	row, ok := app.podRowAt(0, time.Now())
	if !ok {
		t.Fatalf("expected pod row")
	}
	if got := stripUsageANSI(app.podCPUCell(row)); !strings.Contains(got, "120m") {
		t.Fatalf("cpu cell = %q", got)
	}
	if len(app.podUsageCellByKey) != 1 {
		t.Fatalf("expected one cached visible row, got %d", len(app.podUsageCellByKey))
	}
}
