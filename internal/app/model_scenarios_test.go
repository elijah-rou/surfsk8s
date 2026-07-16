package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	t.Run("pods keep identity through enter after insert", func(t *testing.T) {
		h := newModelHarness(t)
		store := h.store
		now := time.Now()
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default", CreationTimestamp: metav1.NewTime(now)}})
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", CreationTimestamp: metav1.NewTime(now)}})
		h.app.screen = screenPods
		h.app.refreshPods(now)
		h.app.podTable.SetCursor(1)
		h.app.syncPodSelectionFromCursor(now)
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
		h.Key(tea.KeyMsg{Type: tea.KeyEnter})
		if h.app.screen != screenPodDetails {
			t.Fatalf("screen=%d want pod details", h.app.screen)
		}
		if h.app.activePod.Row.Name != "c" {
			t.Fatalf("detail target = %q, want c", h.app.activePod.Row.Name)
		}
	})

	t.Run("pods report vanished after selected deletion", func(t *testing.T) {
		h := newModelHarness(t)
		store := h.store
		now := time.Now()
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "default", CreationTimestamp: metav1.NewTime(now)}})
		store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "default", CreationTimestamp: metav1.NewTime(now)}})
		h.app.screen = screenPods
		h.app.refreshPods(now)
		h.app.podTable.SetCursor(1)
		h.app.syncPodSelectionFromCursor(now)
		store.DeletePodByKey("dev", "default", "c")
		h.app.refreshPods(now)
		h.Key(tea.KeyMsg{Type: tea.KeyEnter})
		if h.app.screen != screenPods {
			t.Fatalf("screen=%d want pods list after vanished enter", h.app.screen)
		}
		if !strings.Contains(h.app.statusMessage, "vanished") {
			t.Fatalf("status=%q, want vanished", h.app.statusMessage)
		}
		if h.app.activePod.Row.Name == "b" {
			t.Fatalf("enter adopted neighbor b")
		}
	})

	t.Run("generic keep identity through enter after insert", func(t *testing.T) {
		h := newModelHarness(t)
		now := time.Now()
		b := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "b"}, Name: "b", Namespace: "default", Cluster: "dev"}
		c := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "c"}, Name: "c", Namespace: "default", Cluster: "dev"}
		aRow := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "a"}, Name: "a", Namespace: "default", Cluster: "dev"}
		fake := newBlockingGenericBackend()
		fake.details = cluster.GenericResourceDetails{Row: c, YAML: "kind: Widget"}
		fake.detailBarrier.Release()
		h.app.genericBackend = fake
		h.app.screen = screenResourceList
		h.app.activeResource = testGenericKind()
		h.app.applyGenericListRows([]cluster.GenericResourceRow{b, c}, now, testGenericKind().ID)
		h.app.genericListCacheKey = ""
		h.app.renderGenericResourceList(now)
		h.app.resourceTable.SetCursor(1)
		h.app.syncGenericSelectionFromCursor(now)
		h.app.applyGenericListRows([]cluster.GenericResourceRow{aRow, b, c}, now, testGenericKind().ID)
		h.app.genericListCacheKey = ""
		h.app.renderGenericResourceList(now)
		row, ok := h.app.genericResourceRowAt(h.app.resourceTable.SelectedIndex(), now)
		if !ok || row.Name != "c" {
			t.Fatalf("selected after insert = %q, want c", row.Name)
		}
		h.Key(tea.KeyMsg{Type: tea.KeyEnter})
		if h.app.genericSelectionKey.Name != "c" {
			t.Fatalf("enter target key=%q, want c", h.app.genericSelectionKey.Name)
		}
	})

	t.Run("generic report vanished after selected deletion", func(t *testing.T) {
		h := newModelHarness(t)
		now := time.Now()
		b := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "b"}, Name: "b", Namespace: "default", Cluster: "dev"}
		c := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "c"}, Name: "c", Namespace: "default", Cluster: "dev"}
		h.app.screen = screenResourceList
		h.app.activeResource = testGenericKind()
		h.app.applyGenericListRows([]cluster.GenericResourceRow{b, c}, now, testGenericKind().ID)
		h.app.genericListCacheKey = ""
		h.app.renderGenericResourceList(now)
		h.app.resourceTable.SetCursor(1)
		h.app.syncGenericSelectionFromCursor(now)
		h.app.applyGenericListRows([]cluster.GenericResourceRow{b}, now, testGenericKind().ID)
		h.app.genericListCacheKey = ""
		h.app.renderGenericResourceList(now)
		h.Key(tea.KeyMsg{Type: tea.KeyEnter})
		if !strings.Contains(h.app.statusMessage, "vanished") {
			t.Fatalf("status=%q, want vanished", h.app.statusMessage)
		}
	})
}

type blockingLogBackend struct {
	enterFirst    chan struct{}
	releaseFirst  chan struct{}
	enterSecond   chan struct{}
	releaseSecond chan struct{}
	calls         int
	mu            sync.Mutex
	lastOpts      cluster.PodLogsOptions
}

func (b *blockingLogBackend) PodLogsWithOptions(ctx context.Context, details state.PodDetails, options cluster.PodLogsOptions) (string, error) {
	b.mu.Lock()
	b.calls++
	call := b.calls
	b.lastOpts = options
	b.mu.Unlock()
	if call == 1 {
		select {
		case b.enterFirst <- struct{}{}:
		default:
		}
		select {
		case <-b.releaseFirst:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "2024-01-01T00:00:00Z first\n", nil
	}
	select {
	case b.enterSecond <- struct{}{}:
	default:
	}
	select {
	case <-b.releaseSecond:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return "2024-01-01T00:00:01Z second\n", nil
}

func (b *blockingLogBackend) NodeLogWithOptions(ctx context.Context, details state.NodeDetails, options cluster.NodeLogOptions) (string, error) {
	return "", fmt.Errorf("unused")
}

func (b *blockingLogBackend) NodeLogEntries(ctx context.Context, details state.NodeDetails, dir string) ([]cluster.NodeLogEntry, error) {
	return nil, fmt.Errorf("unused")
}

func (b *blockingLogBackend) DeploymentPodDetails(details state.DeploymentDetails, now time.Time) ([]state.PodDetails, error) {
	return nil, fmt.Errorf("unused")
}

func TestForcedLogRefreshCancelsPreviousRequest(t *testing.T) {
	h := newModelHarness(t)
	fake := &blockingLogBackend{
		enterFirst:    make(chan struct{}, 1),
		releaseFirst:  make(chan struct{}),
		enterSecond:   make(chan struct{}, 1),
		releaseSecond: make(chan struct{}),
	}
	h.app.logBackend = fake
	h.app.screen = screenLogs
	h.app.logTarget = logTargetPod
	h.app.logRange = logRangeAll
	h.app.logPod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api"},
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
		},
	}
	h.app.logSelectedContainers = map[string]bool{"main": true}

	firstCmd := h.app.refreshLogs(true)
	if firstCmd == nil {
		t.Fatal("expected first refresh command")
	}
	firstDone := make(chan tea.Msg, 1)
	go func() { firstDone <- firstCmd() }()
	select {
	case <-fake.enterFirst:
	case <-time.After(2 * time.Second):
		t.Fatal("first fetch never started")
	}

	secondCmd := h.app.refreshLogs(true)
	if secondCmd == nil {
		t.Fatal("expected forced refresh command")
	}
	secondDone := make(chan tea.Msg, 1)
	go func() { secondDone <- secondCmd() }()

	select {
	case msg := <-firstDone:
		res, ok := msg.(logsResultMsg)
		if !ok {
			t.Fatalf("first result type %T", msg)
		}
		if res.Err == nil {
			t.Fatal("expected first request canceled")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first request was not canceled by forced refresh")
	}

	select {
	case <-fake.enterSecond:
	case <-time.After(2 * time.Second):
		t.Fatal("second fetch never started")
	}
	close(fake.releaseSecond)
	secondMsg := <-secondDone
	// Complete reverse: apply stale canceled first (already done), then second.
	h.RunAll(func() tea.Msg { return secondMsg })
	if len(h.app.logEntries) == 0 || !strings.Contains(h.app.logEntries[0].Message, "second") {
		t.Fatalf("expected second result applied, got %#v", h.app.logEntries)
	}
}

func TestModelScenarioMaximumLogRangeIsBounded(t *testing.T) {
	h := newModelHarness(t)
	fake := &blockingLogBackend{
		enterFirst:    make(chan struct{}, 1),
		releaseFirst:  make(chan struct{}),
		enterSecond:   make(chan struct{}, 1),
		releaseSecond: make(chan struct{}),
	}
	close(fake.releaseFirst)
	close(fake.releaseSecond)
	h.app.logBackend = fake
	h.app.screen = screenLogs
	h.app.logTarget = logTargetPod
	h.app.logRange = logRangeAll
	h.app.logPod = state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api"},
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
			Spec: corev1.PodSpec{
				Containers: func() []corev1.Container {
					out := make([]corev1.Container, maxLogFetchSources+1)
					for i := range out {
						out[i] = corev1.Container{Name: fmt.Sprintf("c%d", i)}
					}
					return out
				}(),
			},
		},
	}
	h.app.logSelectedContainers = map[string]bool{}
	for i := 0; i < maxLogFetchSources+1; i++ {
		h.app.logSelectedContainers[fmt.Sprintf("c%d", i)] = true
	}

	if got := logRangeAll.menuLabel(); !strings.Contains(got, "1 MiB") {
		t.Fatalf("menu label=%q, want max 1 MiB", got)
	}
	if got := logRangeAll.nodeTailBytes(); got != defaultPodLogBytes {
		t.Fatalf("nodeTailBytes=%d want %d", got, defaultPodLogBytes)
	}

	h.RunAll(h.app.refreshLogs(true))
	if !strings.Contains(h.app.statusMessage, "too many log sources") {
		t.Fatalf("status=%q, want source limit", h.app.statusMessage)
	}
	if len(h.app.logEntries) > maxRetainedLogEntries {
		t.Fatalf("retained %d entries, max %d", len(h.app.logEntries), maxRetainedLogEntries)
	}
}
