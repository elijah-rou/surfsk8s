package app

import (
	"os"
	"path/filepath"
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

func TestModelScenarioLogSave(t *testing.T) {
	t.Run("enter writes file and preserves filter", func(t *testing.T) {
		h := newModelHarness(t)
		app := h.app
		app.screen = screenLogs
		app.logTarget = logTargetPod
		app.width = 120
		app.logFilterQuery = "rendered"
		app.logEntries = []logEntry{{UniqueKey: "1", Message: "rendered-log-line"}}

		path := filepath.Join(t.TempDir(), "out.log")
		h.RunAll(app.openLogSavePrompt())
		if !app.filter.Active() || app.inputMode != inputModeLogSavePath {
			t.Fatalf("expected active log-save prompt, mode=%d active=%v", app.inputMode, app.filter.Active())
		}
		app.filter.SetValue(path)
		h.Key(tea.KeyMsg{Type: tea.KeyEnter})

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected save write: %v", err)
		}
		if !strings.Contains(string(content), "rendered-log-line") {
			t.Fatalf("saved content missing logs: %q", content)
		}
		if got, want := app.logFilterQuery, "rendered"; got != want {
			t.Fatalf("logFilterQuery = %q, want %q", got, want)
		}
	})

	t.Run("esc leaves filter unchanged and writes nothing", func(t *testing.T) {
		h := newModelHarness(t)
		app := h.app
		app.screen = screenLogs
		app.logTarget = logTargetPod
		app.width = 120
		app.logFilterQuery = "=keep-esc"
		app.logEntries = []logEntry{{UniqueKey: "1", Message: "line"}}

		path := filepath.Join(t.TempDir(), "nope.log")
		h.RunAll(app.openLogSavePrompt())
		app.filter.SetValue(path)
		h.Key(tea.KeyMsg{Type: tea.KeyEsc})

		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected no file write, stat err=%v", err)
		}
		if got, want := app.logFilterQuery, "=keep-esc"; got != want {
			t.Fatalf("logFilterQuery = %q, want %q", got, want)
		}
		if app.filter.Active() {
			t.Fatalf("expected filter deactivated after esc")
		}
	})
}

func TestModelScenarioCtrlCQuitsActivePrompt(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*modelHarness)
	}{
		{
			name: "search",
			setup: func(h *modelHarness) {
				h.app.screen = screenPods
				h.app.openFilter()
			},
		},
		{
			name: "command",
			setup: func(h *modelHarness) {
				h.app.screen = screenPodDetails
				h.app.openCommands()
			},
		},
		{
			name: "resource finder",
			setup: func(h *modelHarness) {
				h.app.screen = screenCatalog
				h.RunAll(h.app.openResourceFinder())
			},
		},
		{
			name: "scale",
			setup: func(h *modelHarness) {
				h.app.screen = screenResourceDetails
				h.app.activeResource = cluster.ResourceKind{Display: "Deployments", Resource: "deployments", APIGroup: "apps"}
				replicas := int32(1)
				h.app.activeDeployment = state.DeploymentDetails{
					Row:        state.DeploymentRow{Cluster: "dev", Namespace: "default", Name: "api"},
					Deployment: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}},
				}
				h.RunAll(h.app.openScalePrompt())
			},
		},
		{
			name: "local-port",
			setup: func(h *modelHarness) {
				h.app.screen = screenPodDetails
				h.app.actionReturnScreen = screenPodDetails
				h.RunAll(h.app.openLocalPortPrompt(pendingActionPortForwardPod, 8080))
			},
		},
		{
			name: "scope",
			setup: func(h *modelHarness) {
				h.app.screen = screenPods
				h.app.namespaces = []string{"default", "kube-system"}
				h.RunAll(h.app.openNamespacePicker())
			},
		},
		{
			name: "table-filter",
			setup: func(h *modelHarness) {
				h.app.screen = screenPods
				h.app.refreshPods(h.app.lastTick)
				h.RunAll(h.app.openTableFilterValuePrompt(0, "NAME"))
			},
		},
		{
			name: "exact-log-filter",
			setup: func(h *modelHarness) {
				h.app.screen = screenLogs
				h.app.logTarget = logTargetPod
				h.RunAll(h.app.openLogExactFilter())
			},
		},
		{
			name: "log-save",
			setup: func(h *modelHarness) {
				h.app.screen = screenLogs
				h.app.logTarget = logTargetPod
				h.RunAll(h.app.openLogSavePrompt())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newModelHarness(t)
			tc.setup(h)
			if !h.app.filter.Active() {
				t.Fatalf("expected active prompt for %s", tc.name)
			}
			cmd := h.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
			if cmd == nil {
				t.Fatalf("expected quit command for %s", tc.name)
			}
			msg := h.Run(cmd)
			if _, ok := msg.(tea.QuitMsg); !ok {
				t.Fatalf("got %T, want tea.QuitMsg for %s", msg, tc.name)
			}
		})
	}
}

func TestModelScenarioSingleOptionActionReturnsToOrigin(t *testing.T) {
	t.Run("one-container exec missing context stays on detail", func(t *testing.T) {
		h := newModelHarness(t)
		app := h.app
		app.screen = screenPodDetails
		app.actionReturnScreen = screenContexts // stale
		app.activePod = state.PodDetails{
			Row: state.PodRow{Cluster: "", Namespace: "default", Name: "toolbox"},
			Pod: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}},
		}
		h.RunAll(app.runExecPod())
		if got, want := app.screen, screenPodDetails; got != want {
			t.Fatalf("screen = %d, want %d", got, want)
		}
		if got, want := app.statusMessage, "missing cluster context"; got != want {
			t.Fatalf("status = %q, want %q", got, want)
		}
	})

	t.Run("one-port pod forward esc stays on detail", func(t *testing.T) {
		h := newModelHarness(t)
		app := h.app
		app.screen = screenPodDetails
		app.actionReturnScreen = screenContexts
		app.activePod = state.PodDetails{
			Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api"},
			Pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "main",
						Ports: []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
					}},
				},
			},
		}
		h.RunAll(app.runPortForwardPod())
		if !app.filter.Active() || app.inputMode != inputModeLocalPort {
			t.Fatalf("expected local-port prompt")
		}
		h.Key(tea.KeyMsg{Type: tea.KeyEsc})
		if got, want := app.screen, screenPodDetails; got != want {
			t.Fatalf("screen = %d, want %d", got, want)
		}
	})

	t.Run("one-port service forward esc stays on detail", func(t *testing.T) {
		h := newModelHarness(t)
		app := h.app
		app.screen = screenResourceDetails
		app.actionReturnScreen = screenContexts
		app.activeResource = cluster.ResourceKind{Display: "Services", Resource: "services", APIGroup: ""}
		app.activeService = state.ServiceDetails{
			Row: state.ServiceRow{Cluster: "dev", Namespace: "default", Name: "api"},
			Service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}},
				},
			},
		}
		h.RunAll(app.runPortForwardResource())
		if !app.filter.Active() || app.inputMode != inputModeLocalPort {
			t.Fatalf("expected local-port prompt")
		}
		h.Key(tea.KeyMsg{Type: tea.KeyEsc})
		if got, want := app.screen, screenResourceDetails; got != want {
			t.Fatalf("screen = %d, want %d", got, want)
		}
	})
}

func TestListSelectionPreservesResourceIdentity(t *testing.T) {
	t.Run("pods", func(t *testing.T) {
		h := newModelHarness(t)
		store := h.store
		now := time.Now()
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default", CreationTimestamp: metav1.NewTime(now)}})
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", CreationTimestamp: metav1.NewTime(now)}})
		h.app.screen = screenPods
		h.app.refreshPods(now)
		// select c (second row after sort by name: b, c)
		h.app.podTable.SetCursor(1)
		row, ok := h.app.podRowAt(h.app.podTable.SelectedIndex(), now)
		if !ok || row.Name != "c" {
			t.Fatalf("precondition: selected %v", row)
		}
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "default", CreationTimestamp: metav1.NewTime(now)}})
		h.app.refreshPods(now)
		row, ok = h.app.podRowAt(h.app.podTable.SelectedIndex(), now)
		if !ok || row.Name != "c" {
			t.Fatalf("selected after insert = %q, want c", row.Name)
		}
		h.RunAll(h.app.openCurrentResourceSelection(h.app.podTable.SelectedIndex(), now))
		// pods path opens details sync; generic uses cmd. For pods Enter uses openCurrentResourceSelection on pod table via updatePodKeys.
		if h.app.screen != screenPods {
			// openCurrentResourceSelection is for resource list; use pod details path:
		}
		details, ok := store.PodDetailsByKey(row.Key, now)
		if !ok {
			t.Fatalf("pod details missing")
		}
		h.app.activePod = details
		h.app.screen = screenPodDetails
		if h.app.activePod.Row.Name != "c" {
			t.Fatalf("detail target = %q, want c", h.app.activePod.Row.Name)
		}
	})

	t.Run("generic", func(t *testing.T) {
		h := newModelHarness(t)
		now := time.Now()
		b := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "b"}, Name: "b", Namespace: "default", Cluster: "dev"}
		c := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "c"}, Name: "c", Namespace: "default", Cluster: "dev"}
		aRow := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "a"}, Name: "a", Namespace: "default", Cluster: "dev"}
		h.app.screen = screenResourceList
		h.app.activeResource = testGenericKind()
		h.app.applyGenericListRows([]cluster.GenericResourceRow{b, c}, now, testGenericKind().ID)
		h.app.genericListCacheKey = ""
		h.app.renderGenericResourceList(now)
		h.app.resourceTable.SetCursor(1)
		row, ok := h.app.genericResourceRowAt(h.app.resourceTable.SelectedIndex(), now)
		if !ok || row.Name != "c" {
			t.Fatalf("precondition selected=%v", row)
		}
		h.app.applyGenericListRows([]cluster.GenericResourceRow{aRow, b, c}, now, testGenericKind().ID)
		h.app.genericListCacheKey = ""
		h.app.renderGenericResourceList(now)
		row, ok = h.app.genericResourceRowAt(h.app.resourceTable.SelectedIndex(), now)
		if !ok || row.Name != "c" {
			t.Fatalf("selected after insert = %q, want c", row.Name)
		}
	})
}
