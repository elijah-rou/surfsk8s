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
