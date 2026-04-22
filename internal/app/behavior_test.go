package app

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
	"github.com/elijahrou/surfsk8s/internal/ui/components"
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

func TestCommandPromptSupportsVimNavigationKeys(t *testing.T) {
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
	app.updateFilter(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
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

func TestOpenSelectedResourceFinderItemOpensResource(t *testing.T) {
	manager := newTestManager(t)
	app := New(state.NewStore(), manager, Config{})
	app.visibleResourceItems = []resourceFinderItem{{resource: cluster.ResourceKind{Display: "Revisions", Resource: "revisions", APIGroup: "serving.knative.dev", Version: "v1", Namespaced: true, Custom: true}}}
	app.navTable.SetRows(renderResourceFinderRows(app.visibleResourceItems, nil))
	app.screen = screenResourceFinder
	app.inputMode = inputModeResourceFinder
	app.filter.Activate()

	cmd := app.openSelectedResourceFinderItem()
	if cmd != nil {
		t.Fatalf("expected open resource command nil")
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
	app.Update(podUsageSnapshotMsg{scopeKey: app.podUsageScopeKey(), storeVersion: store.Version(), usages: map[string]cluster.PodResourceUsage{
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
	app.Update(nodeUsageSnapshotMsg{scopeKey: app.nodeUsageScopeKey(), storeVersion: store.Version(), usages: map[string]cluster.NodeResourceUsage{
		key: {Key: state.NodeKey{Cluster: "dev", Name: "node-a"}, CPUUsedMilli: 1200, CPUAllocatableMilli: 4000, HasCPUUsage: true},
	}})
	_, body, _ := app.currentView()
	if !strings.Contains(stripUsageANSI(body), "1200m/4") {
		t.Fatalf("expected usage gauge in body, got:\n%s", stripUsageANSI(body))
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
	if cmd := app.saveLogsToPath(path); cmd != nil {
		t.Fatalf("expected nil cmd")
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
