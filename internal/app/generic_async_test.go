package app

import (
	"context"
	"strings"
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

func TestResourceFinderGenericSelectionStartsListFetch(t *testing.T) {
	h := newModelHarness(t)
	fake := newBlockingGenericBackend()
	h.app.genericBackend = fake
	resource := testGenericKind()
	h.app.visibleResourceItems = []resourceFinderItem{{resource: resource, label: "Widgets"}}
	h.app.setNavTable("RESOURCES", [][]string{{"Widgets"}})
	t.Cleanup(fake.listBarrier.Release)

	cmd := h.app.openSelectedResourceFinderItem()
	if cmd == nil {
		t.Fatal("generic resource finder selection did not return list fetch command")
	}
	if !h.app.genericListLoading {
		t.Fatal("generic resource finder selection did not mark list loading")
	}
}

func TestGroupedCatalogGenericSelectionExecutesInitialListFetch(t *testing.T) {
	h := newModelHarness(t)
	fake := newBlockingGenericBackend()
	fake.rows = []cluster.GenericResourceRow{{
		Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "grouped"}, Name: "grouped", Namespace: "default", Cluster: "dev",
	}}
	fake.listBarrier.Release()
	h.app.genericBackend = fake
	h.app.screen = screenGroupResources
	h.app.visibleResources = []cluster.ResourceKind{testGenericKind()}
	h.app.setNavTable("RESOURCES", [][]string{{"Widgets"}})

	cmd := h.app.updateGroupKeys(tea.KeyMsg{Type: tea.KeyEnter})
	h.RunAll(cmd)

	if fake.listCalls != 1 {
		t.Fatalf("generic list calls = %d, want 1", fake.listCalls)
	}
	if h.app.genericListLoading {
		t.Fatal("generic list remained loading after grouped catalog fetch completed")
	}
	if len(h.app.sortedGenericRows) != 1 || h.app.sortedGenericRows[0].Name != "grouped" {
		t.Fatalf("generic rows = %#v, want fetched grouped row", h.app.sortedGenericRows)
	}
}

func TestRestoredGenericSessionExecutesInitialListFetch(t *testing.T) {
	h := newModelHarness(t)
	fake := newBlockingGenericBackend()
	fake.rows = []cluster.GenericResourceRow{{
		Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "restored"}, Name: "restored", Namespace: "default", Cluster: "dev",
	}}
	fake.listBarrier.Release()
	h.app.genericBackend = fake
	h.app.resumeOnStart = true
	h.app.resumeSession = sessionPreference{Screen: "resource-list", ResourceID: "apps/statefulsets", Namespace: "default", Query: "restored"}

	_, cmd := h.app.Update(connectResultMsg{contexts: []string{"dev"}})
	h.RunAll(cmd)

	if fake.listCalls != 1 {
		t.Fatalf("generic list calls = %d, want 1", fake.listCalls)
	}
	if h.app.genericListLoading {
		t.Fatal("generic list remained loading after restored-session fetch completed")
	}
	if len(h.app.sortedGenericRows) != 1 || h.app.sortedGenericRows[0].Name != "restored" {
		t.Fatalf("generic rows = %#v, want fetched restored row", h.app.sortedGenericRows)
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
	t.Run("open generic jump target", func(t *testing.T) {
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
	})

	t.Run("owner key on unsynced generic list", func(t *testing.T) {
		h := newModelHarness(t)
		fake := newBlockingGenericBackend()
		row := cluster.GenericResourceRow{
			Key:       cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "w1"},
			Name:      "w1",
			Namespace: "default",
			Cluster:   "dev",
			// Object nil: forces GenericResourceDetails fallback
		}
		h.app.genericBackend = fake
		h.app.screen = screenResourceList
		h.app.activeResource = testGenericKind()
		h.app.sortedGenericRows = []cluster.GenericResourceRow{row}
		h.app.resourceTable.SetWindowProvider(1, func(start int, end int) [][]string {
			return [][]string{{"dev", "default", "w1"}}
		})
		t.Cleanup(fake.detailBarrier.Release)
		t.Cleanup(fake.iterBarrier.Release)

		done := make(chan tea.Cmd, 1)
		go func() {
			_, cmd := h.app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
			done <- cmd
		}()
		_ = assertUpdateDoesNotBlock(t, done)
		if fake.detailCalls != 0 {
			t.Fatalf("backend detail called during Update for owner jump; want deferred command")
		}
		if fake.iterCalls != 0 {
			t.Fatalf("backend iter called during Update for owner jump; want deferred command")
		}
	})

	t.Run("dependent key on unsynced generic details", func(t *testing.T) {
		h := newModelHarness(t)
		fake := newBlockingGenericBackend()
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion("example.com/v1")
		obj.SetKind("Widget")
		obj.SetName("w1")
		obj.SetNamespace("default")
		row := cluster.GenericResourceRow{
			Key:       cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "w1"},
			Name:      "w1",
			Namespace: "default",
			Cluster:   "dev",
			Object:    obj,
		}
		h.app.genericBackend = fake
		h.app.screen = screenResourceDetails
		h.app.activeResource = testGenericKind()
		h.app.activeGenericDetails = cluster.GenericResourceDetails{Row: row, Object: obj, YAML: "kind: Widget"}
		t.Cleanup(fake.detailBarrier.Release)
		t.Cleanup(fake.iterBarrier.Release)

		done := make(chan tea.Cmd, 1)
		go func() {
			_, cmd := h.app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
			done <- cmd
		}()
		_ = assertUpdateDoesNotBlock(t, done)
		if fake.detailCalls != 0 {
			t.Fatalf("backend detail called during Update for dependent jump; want deferred command")
		}
		if fake.iterCalls != 0 {
			t.Fatalf("backend iter called during Update for dependent jump; want deferred command")
		}
	})
}

func TestGenericListResultPreservesRowsOnError(t *testing.T) {
	h := newModelHarness(t)
	now := time.Now()
	cached := []cluster.GenericResourceRow{{
		Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "keep"}, Name: "keep", Namespace: "default", Cluster: "dev",
	}}
	h.app.screen = screenResourceList
	h.app.activeResource = testGenericKind()
	h.app.applyGenericListRows(cached, now, testGenericKind().ID)
	h.app.genericListCacheKey = ""
	h.app.renderGenericResourceList(now)
	token := h.app.nextAsyncTokenValue()
	h.app.genericListToken = token
	h.app.genericListLoading = true

	h.RunAll(func() tea.Msg {
		return genericListResultMsg{
			Token:      token,
			ResourceID: testGenericKind().ID,
			Rows:       []cluster.GenericResourceRow{{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "partial"}, Name: "partial"}},
			Err:        context.DeadlineExceeded,
		}
	})
	if len(h.app.sortedGenericRows) != 1 || h.app.sortedGenericRows[0].Name != "keep" {
		t.Fatalf("rows replaced on error: %#v", h.app.sortedGenericRows)
	}
	if !strings.Contains(h.app.statusMessage, "deadline") && h.app.statusMessage == "" {
		t.Fatalf("expected error status, got %q", h.app.statusMessage)
	}
}

func TestGenericListResultOrderMatrix(t *testing.T) {
	t.Run("stale token ignored", func(t *testing.T) {
		h := newModelHarness(t)
		h.app.screen = screenResourceList
		h.app.activeResource = testGenericKind()
		h.app.genericListToken = 9
		h.app.genericListLoading = true
		h.RunAll(func() tea.Msg {
			return genericListResultMsg{Token: 8, ResourceID: testGenericKind().ID, Rows: []cluster.GenericResourceRow{{Name: "stale"}}}
		})
		if len(h.app.sortedGenericRows) != 0 {
			t.Fatalf("stale result applied: %#v", h.app.sortedGenericRows)
		}
		if !h.app.genericListLoading {
			t.Fatal("stale result cleared loading")
		}
	})

	t.Run("success applies matching token", func(t *testing.T) {
		h := newModelHarness(t)
		h.app.screen = screenResourceList
		h.app.activeResource = testGenericKind()
		token := h.app.nextAsyncTokenValue()
		h.app.genericListToken = token
		h.app.genericListLoading = true
		row := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "ok"}, Name: "ok", Namespace: "default", Cluster: "dev"}
		h.RunAll(func() tea.Msg {
			return genericListResultMsg{Token: token, ResourceID: testGenericKind().ID, Rows: []cluster.GenericResourceRow{row}}
		})
		if len(h.app.sortedGenericRows) != 1 || h.app.sortedGenericRows[0].Name != "ok" {
			t.Fatalf("success not applied: %#v", h.app.sortedGenericRows)
		}
		if h.app.genericListLoading {
			t.Fatal("loading stuck")
		}
	})

	t.Run("reverse completion keeps latest", func(t *testing.T) {
		h := newModelHarness(t)
		h.app.screen = screenResourceList
		h.app.activeResource = testGenericKind()
		oldToken := h.app.nextAsyncTokenValue()
		newToken := h.app.nextAsyncTokenValue()
		h.app.genericListToken = newToken
		h.app.genericListLoading = true
		oldRow := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "old"}, Name: "old", Namespace: "default", Cluster: "dev"}
		newRow := cluster.GenericResourceRow{Key: cluster.GenericResourceKey{Cluster: "dev", Namespace: "default", Name: "new"}, Name: "new", Namespace: "default", Cluster: "dev"}
		// Latest completes first.
		h.RunAll(func() tea.Msg {
			return genericListResultMsg{Token: newToken, ResourceID: testGenericKind().ID, Rows: []cluster.GenericResourceRow{newRow}}
		})
		// Stale older result arrives later.
		h.RunAll(func() tea.Msg {
			return genericListResultMsg{Token: oldToken, ResourceID: testGenericKind().ID, Rows: []cluster.GenericResourceRow{oldRow}}
		})
		if len(h.app.sortedGenericRows) != 1 || h.app.sortedGenericRows[0].Name != "new" {
			t.Fatalf("reverse completion overwrote latest: %#v", h.app.sortedGenericRows)
		}
	})
}
