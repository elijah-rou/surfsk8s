package app

import (
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
	if !strings.Contains(captured, "CONTEXT,NAMESPACE,NAME,READY,STATUS,RESTARTS,AGE,NODE") {
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
	app.navTable.SetRows(renderResourceFinderRows(app.visibleResourceItems))
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
