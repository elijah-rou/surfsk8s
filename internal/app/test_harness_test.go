package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

const modelHarnessDefaultDeadline = 5 * time.Second

// modelHarness drives App.Update and returned tea.Cmd values deterministically.
// It uses channels/barriers and a hard deadline; never sleeps or polls.
type modelHarness struct {
	t        *testing.T
	store    *state.Store
	manager  *cluster.Manager
	app      *App
	deadline time.Duration
	cancel   context.CancelFunc

	cmdMu      sync.Mutex
	cmdCancels []context.CancelFunc
	cmdWG      sync.WaitGroup
}

func newModelHarness(t *testing.T) *modelHarness {
	t.Helper()
	if t == nil {
		panic("newModelHarness: nil testing.T")
	}

	// Isolate preferences via XDG_CONFIG_HOME so parallel tests do not race on
	// the package-global userConfigDir indirection.
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	rootCtx, rootCancel := context.WithCancel(context.Background())
	store := state.NewStore()
	manager := newTestManagerWithStore(t, store)
	app := New(store, manager, Config{Context: rootCtx})
	app.width = 80
	app.height = 24

	h := &modelHarness{
		t:        t,
		store:    store,
		manager:  manager,
		app:      app,
		deadline: modelHarnessDefaultDeadline,
		cancel:   rootCancel,
	}
	t.Cleanup(func() {
		rootCancel()
		h.cancelAllCommands()
		h.cmdWG.Wait()
	})
	return h
}

func newTestManagerWithStore(t *testing.T, store *state.Store) *cluster.Manager {
	t.Helper()
	if store == nil {
		panic("newTestManagerWithStore: nil store")
	}
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

	manager, err := cluster.NewManager(store, cluster.Config{KubeconfigPath: kubeconfigPath})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(manager.Close)
	return manager
}

func (h *modelHarness) cancelAllCommands() {
	h.cmdMu.Lock()
	defer h.cmdMu.Unlock()
	for _, cancel := range h.cmdCancels {
		if cancel != nil {
			cancel()
		}
	}
	h.cmdCancels = nil
}

func (h *modelHarness) trackCancel(cancel context.CancelFunc) {
	h.cmdMu.Lock()
	defer h.cmdMu.Unlock()
	h.cmdCancels = append(h.cmdCancels, cancel)
}

func (h *modelHarness) Send(msg tea.Msg) tea.Cmd {
	h.t.Helper()
	if msg == nil {
		panic("modelHarness.Send: nil msg")
	}
	model, cmd := h.app.Update(msg)
	next, ok := model.(*App)
	if !ok || next == nil {
		h.t.Fatalf("Update returned %T, want *App", model)
	}
	h.app = next
	return cmd
}

func (h *modelHarness) Keys(keys ...string) {
	h.t.Helper()
	for _, key := range keys {
		if key == "" {
			panic("modelHarness.Keys: empty key")
		}
		cmd := h.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		h.RunAll(cmd)
	}
}

func (h *modelHarness) Key(msg tea.KeyMsg) {
	h.t.Helper()
	h.RunAll(h.Send(msg))
}

func (h *modelHarness) Resize(width int, height int) {
	h.t.Helper()
	if width <= 0 || height <= 0 {
		panic("modelHarness.Resize: non-positive size")
	}
	h.RunAll(h.Send(tea.WindowSizeMsg{Width: width, Height: height}))
}

func (h *modelHarness) Run(cmd tea.Cmd) tea.Msg {
	h.t.Helper()
	if cmd == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(h.app.context, h.deadline)
	h.trackCancel(cancel)
	defer cancel()

	done := make(chan tea.Msg, 1)
	h.cmdWG.Add(1)
	go func() {
		defer h.cmdWG.Done()
		done <- cmd()
	}()
	select {
	case msg := <-done:
		return msg
	case <-ctx.Done():
		if h.cancel != nil {
			h.cancel()
		}
		h.cancelAllCommands()
		h.t.Fatalf("modelHarness.Run: command exceeded deadline %s", h.deadline)
		return nil
	}
}

func (h *modelHarness) RunAll(cmd tea.Cmd) {
	h.t.Helper()
	h.runAllBounded(cmd, 0)
}

func (h *modelHarness) runAllBounded(cmd tea.Cmd, depth int) {
	h.t.Helper()
	const maxDepth = 64
	if depth > maxDepth {
		h.t.Fatalf("modelHarness.RunAll: command nesting exceeded %d", maxDepth)
	}
	if cmd == nil {
		return
	}
	msg := h.Run(cmd)
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		h.runBatchConcurrent(batch, depth+1)
		return
	}
	// SequenceMsg may appear from tea.Sequence; expand via reflection-safe type assert.
	if cmds := sequenceCmds(msg); cmds != nil {
		for _, nested := range cmds {
			h.runAllBounded(nested, depth+1)
		}
		return
	}
	next := h.Send(msg)
	h.runAllBounded(next, depth+1)
}

// runBatchConcurrent starts every batch command concurrently (matching Bubble Tea),
// then applies results in completion order through Update.
func (h *modelHarness) runBatchConcurrent(batch tea.BatchMsg, depth int) {
	h.t.Helper()
	if len(batch) == 0 {
		return
	}
	type batchResult struct {
		index int
		msg   tea.Msg
	}
	results := make(chan batchResult, len(batch))
	ctx, cancel := context.WithTimeout(context.Background(), h.deadline)
	h.trackCancel(cancel)
	defer cancel()

	for i, nested := range batch {
		if nested == nil {
			results <- batchResult{index: i, msg: nil}
			continue
		}
		h.cmdWG.Add(1)
		go func(index int, cmd tea.Cmd) {
			defer h.cmdWG.Done()
			results <- batchResult{index: index, msg: cmd()}
		}(i, nested)
	}

	pending := len(batch)
	for pending > 0 {
		select {
		case result := <-results:
			pending--
			if result.msg == nil {
				continue
			}
			if nestedBatch, ok := result.msg.(tea.BatchMsg); ok {
				h.runBatchConcurrent(nestedBatch, depth+1)
				continue
			}
			if cmds := sequenceCmds(result.msg); cmds != nil {
				for _, nested := range cmds {
					h.runAllBounded(nested, depth+1)
				}
				continue
			}
			next := h.Send(result.msg)
			h.runAllBounded(next, depth+1)
		case <-ctx.Done():
			h.cancelAllCommands()
			h.t.Fatalf("modelHarness.runBatchConcurrent: exceeded deadline %s", h.deadline)
			return
		}
	}
}

func sequenceCmds(msg tea.Msg) []tea.Cmd {
	if msg == nil {
		return nil
	}
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return nil
	}
	if v.Type().Elem() != reflect.TypeOf((tea.Cmd)(nil)) {
		return nil
	}
	cmds := make([]tea.Cmd, v.Len())
	for i := 0; i < v.Len(); i++ {
		cmds[i], _ = v.Index(i).Interface().(tea.Cmd)
	}
	return cmds
}

// ReleaseBarrier is a channel-based barrier for intentionally blocking fakes.
type ReleaseBarrier struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func NewReleaseBarrier() *ReleaseBarrier {
	return &ReleaseBarrier{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
}

func (b *ReleaseBarrier) Enter() {
	select {
	case b.entered <- struct{}{}:
	default:
	}
}

func (b *ReleaseBarrier) WaitEntered(t *testing.T, deadline time.Duration) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(deadline):
		t.Fatalf("ReleaseBarrier.WaitEntered: timed out after %s", deadline)
	}
}

func (b *ReleaseBarrier) Release() {
	b.once.Do(func() { close(b.release) })
}

func (b *ReleaseBarrier) Wait(ctx context.Context) error {
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestModelHarnessBatchRunsConcurrently(t *testing.T) {
	h := newModelHarness(t)
	startedSecond := make(chan struct{})
	releaseFirst := make(chan struct{})

	first := func() tea.Msg {
		select {
		case <-startedSecond:
		case <-time.After(2 * time.Second):
			t.Error("second batch command never started while first waited")
			return nil
		}
		close(releaseFirst)
		return nil
	}
	second := func() tea.Msg {
		close(startedSecond)
		<-releaseFirst
		return nil
	}
	// Batch whose first command waits for the second — serial RunAll would deadlock/timeout.
	h.RunAll(tea.Batch(first, second))
}
