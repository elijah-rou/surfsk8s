package app

import (
	"context"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
)

type blockingGenericBackend struct {
	mu sync.Mutex

	listBarrier   *ReleaseBarrier
	detailBarrier *ReleaseBarrier
	iterBarrier   *ReleaseBarrier

	listErr   error
	detailErr error
	iterErr   error

	version       uint64
	objectVersion string
	rows          []cluster.GenericResourceRow
	details       cluster.GenericResourceDetails

	listCalls   int
	detailCalls int
	iterCalls   int
}

func newBlockingGenericBackend() *blockingGenericBackend {
	return &blockingGenericBackend{
		listBarrier:   NewReleaseBarrier(),
		detailBarrier: NewReleaseBarrier(),
		iterBarrier:   NewReleaseBarrier(),
		objectVersion: "1",
		version:       1,
	}
}

func (b *blockingGenericBackend) ListGenericResource(ctx context.Context, resource cluster.ResourceKind) ([]cluster.GenericResourceRow, error) {
	b.mu.Lock()
	b.listCalls++
	b.mu.Unlock()
	b.listBarrier.Enter()
	if err := b.listBarrier.Wait(ctx); err != nil {
		return nil, err
	}
	if b.listErr != nil {
		return append([]cluster.GenericResourceRow(nil), b.rows...), b.listErr
	}
	return append([]cluster.GenericResourceRow(nil), b.rows...), nil
}

func (b *blockingGenericBackend) GenericResourceDetails(ctx context.Context, resource cluster.ResourceKind, key cluster.GenericResourceKey, now time.Time) (cluster.GenericResourceDetails, error) {
	b.mu.Lock()
	b.detailCalls++
	b.mu.Unlock()
	b.detailBarrier.Enter()
	if err := b.detailBarrier.Wait(ctx); err != nil {
		return cluster.GenericResourceDetails{}, err
	}
	if b.detailErr != nil {
		return cluster.GenericResourceDetails{}, b.detailErr
	}
	details := b.details
	if details.Row.Key == (cluster.GenericResourceKey{}) {
		details.Row.Key = key
		details.Row.Cluster = key.Cluster
		details.Row.Namespace = key.Namespace
		details.Row.Name = key.Name
	}
	if details.Object == nil {
		details.Object = &unstructured.Unstructured{}
		details.Object.SetName(key.Name)
		details.Object.SetNamespace(key.Namespace)
	}
	return details, nil
}

func (b *blockingGenericBackend) GenericResourceVersion(resourceID string) uint64 {
	return b.version
}

func (b *blockingGenericBackend) GenericResourceDelta(resourceID string, sinceVersion uint64) (uint64, []cluster.GenericResourceChange, bool) {
	return b.version, nil, false
}

func (b *blockingGenericBackend) GenericResourceObjectVersion(resource cluster.ResourceKind, key cluster.GenericResourceKey) (string, bool) {
	return b.objectVersion, b.objectVersion != ""
}

func (b *blockingGenericBackend) ForEachGenericResourceRow(ctx context.Context, resource cluster.ResourceKind, visit func(cluster.GenericResourceRow) bool) error {
	b.mu.Lock()
	b.iterCalls++
	b.mu.Unlock()
	b.iterBarrier.Enter()
	if err := b.iterBarrier.Wait(ctx); err != nil {
		return err
	}
	if b.iterErr != nil {
		return b.iterErr
	}
	for _, row := range b.rows {
		if !visit(row) {
			return nil
		}
	}
	return nil
}

func testGenericKind() cluster.ResourceKind {
	return cluster.ResourceKind{
		ID:         "example.com/widgets",
		Display:    "Widgets",
		Kind:       "Widget",
		APIGroup:   "example.com",
		Resource:   "widgets",
		Namespaced: true,
	}
}

func assertUpdateDoesNotBlock(t *testing.T, done <-chan tea.Cmd) tea.Cmd {
	t.Helper()
	select {
	case cmd := <-done:
		if cmd == nil {
			t.Fatalf("expected non-nil async command")
		}
		return cmd
	case <-time.After(2 * time.Second):
		t.Fatalf("Update blocked on backend")
		return nil
	}
}

func TestGenericListUpdateDoesNotBlock(t *testing.T) {
	h := newModelHarness(t)
	fake := newBlockingGenericBackend()
	fake.rows = []cluster.GenericResourceRow{{
		Key:       cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "w1"},
		Name:      "w1",
		Namespace: "default",
		Cluster:   "dev",
	}}
	h.app.genericBackend = fake
	h.app.screen = screenResourceList
	h.app.activeResource = testGenericKind()
	h.app.lastManagerVersion = 0
	h.app.genericRowsResourceID = ""
	h.app.lastGenericFetchAt = time.Time{}
	t.Cleanup(fake.listBarrier.Release)

	done := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := h.app.Update(tickMsg(time.Now().Add(time.Second)))
		done <- cmd
	}()
	_ = assertUpdateDoesNotBlock(t, done)
	if !h.app.genericListLoading {
		t.Fatalf("expected generic list loading state")
	}
	if fake.listCalls != 0 {
		t.Fatalf("backend list called during Update; want deferred command")
	}
}

func TestGenericDetailUpdateDoesNotBlock(t *testing.T) {
	h := newModelHarness(t)
	fake := newBlockingGenericBackend()
	row := cluster.GenericResourceRow{
		Key:             cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "w1"},
		Name:            "w1",
		Namespace:       "default",
		Cluster:         "dev",
		ResourceVersion: "1",
	}
	fake.details = cluster.GenericResourceDetails{Row: row, YAML: "kind: Widget"}
	h.app.genericBackend = fake
	h.app.screen = screenResourceList
	h.app.activeResource = testGenericKind()
	h.app.sortedGenericRows = []cluster.GenericResourceRow{row}
	h.app.resourceTable.SetWindowProvider(1, func(start int, end int) [][]string {
		return [][]string{{"dev", "default", "w1"}}
	})
	t.Cleanup(fake.detailBarrier.Release)

	done := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := h.app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		done <- cmd
	}()
	_ = assertUpdateDoesNotBlock(t, done)
	if !h.app.genericDetailLoading {
		t.Fatalf("expected generic detail loading state")
	}
	if fake.detailCalls != 0 {
		t.Fatalf("backend detail called during Update; want deferred command")
	}
}

func TestGenericActionRevalidationDoesNotBlock(t *testing.T) {
	h := newModelHarness(t)
	fake := newBlockingGenericBackend()
	obj := &unstructured.Unstructured{}
	obj.SetName("w1")
	obj.SetNamespace("default")
	row := cluster.GenericResourceRow{
		Key:       cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "w1"},
		Name:      "w1",
		Namespace: "default",
		Cluster:   "dev",
	}
	details := cluster.GenericResourceDetails{Row: row, YAML: "kind: Widget\n", Object: obj}
	fake.details = details
	h.app.genericBackend = fake
	h.app.screen = screenResourceDetails
	h.app.activeResource = testGenericKind()
	h.app.activeGenericDetails = details
	t.Cleanup(fake.detailBarrier.Release)

	h.RunAll(h.app.runDeleteGenericResource())
	if h.app.screen != screenConfirmAction {
		t.Fatalf("expected confirm screen, got %d", h.app.screen)
	}

	done := make(chan tea.Cmd, 1)
	go func() {
		_, cmd := h.app.Update(tea.KeyMsg{Type: tea.KeyEnter})
		done <- cmd
	}()
	_ = assertUpdateDoesNotBlock(t, done)
	if fake.detailCalls != 0 {
		t.Fatalf("backend detail called during Update; want deferred command")
	}
}

func TestGenericJumpUpdateDoesNotBlock(t *testing.T) {
	h := newModelHarness(t)
	fake := newBlockingGenericBackend()
	row := cluster.GenericResourceRow{
		Key:       cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "w1"},
		Name:      "w1",
		Namespace: "default",
		Cluster:   "dev",
	}
	fake.details = cluster.GenericResourceDetails{Row: row, YAML: "kind: Widget"}
	h.app.genericBackend = fake
	h.app.screen = screenResourceDetails
	h.app.activeResource = testGenericKind()
	t.Cleanup(fake.detailBarrier.Release)

	target := resourceJumpTarget{
		Label:     "Widget/w1",
		Resource:  testGenericKind(),
		Cluster:   "dev",
		Namespace: "default",
		Name:      "w1",
	}
	done := make(chan tea.Cmd, 1)
	go func() {
		done <- h.app.openResourceJumpTarget(target, time.Now())
	}()
	_ = assertUpdateDoesNotBlock(t, done)
	if fake.detailCalls != 0 {
		t.Fatalf("backend detail called during openResourceJumpTarget; want deferred command")
	}
}
