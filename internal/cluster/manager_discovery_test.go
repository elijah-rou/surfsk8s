package cluster

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic/fake"

	"github.com/elijahrou/surfsk8s/internal/state"
)

type discoveryResult struct {
	resources []*metav1.APIResourceList
	err       error
	wait      <-chan struct{}
}

type sequenceDiscovery struct {
	discovery.DiscoveryInterface
	mu      sync.Mutex
	results []discoveryResult
	calls   atomic.Int32
	called  chan int
}

func (d *sequenceDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	call := int(d.calls.Add(1))
	if d.called != nil {
		d.called <- call
	}
	d.mu.Lock()
	if len(d.results) == 0 {
		d.mu.Unlock()
		panic("sequenceDiscovery: exhausted results")
	}
	index := call - 1
	if index >= len(d.results) {
		index = len(d.results) - 1
	}
	result := d.results[index]
	d.mu.Unlock()
	if result.wait != nil {
		<-result.wait
	}
	return result.resources, result.err
}

func discoveryList(groupVersion string, resources ...metav1.APIResource) *metav1.APIResourceList {
	return &metav1.APIResourceList{GroupVersion: groupVersion, APIResources: resources}
}

func discoveryPartialError() error {
	return &discovery.ErrGroupDiscoveryFailed{Groups: map[schema.GroupVersion]error{
		{Group: "metrics.k8s.io", Version: "v1beta1"}: errors.New("temporarily unavailable"),
	}}
}

func newDiscoveryTestManager(client discovery.DiscoveryInterface, backoffs ...time.Duration) (*Manager, *ClusterConn) {
	manager := &Manager{
		store:                   state.NewStore(),
		conns:                   make(map[string]*ClusterConn, 1),
		resources:               make(map[string][]discoveredResource, 1),
		discoveryRetryBackoffs:  append([]time.Duration(nil), backoffs...),
		discoveryOverallTimeout: time.Second,
		genericVersions:         make(map[string]uint64),
		genericChanges:          make(map[string][]GenericResourceChange),
	}
	ctx, cancel := context.WithCancel(context.Background())
	dynamicClient := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}: "CustomResourceDefinitionList",
		{Group: "surfsk8s.dev", Version: "v1alpha1", Resource: "widgets"}:                     "WidgetList",
	})
	conn := &ClusterConn{
		ID:             "dev",
		Name:           "dev",
		Discovery:      client,
		Dynamic:        dynamicClient,
		Cancel:         cancel,
		Context:        ctx,
		genericWatches: make(map[string]*genericResourceWatch),
	}
	manager.conns[conn.Name] = conn
	manager.connOrder = []string{conn.Name}
	return manager, conn
}

func catalogHasResource(catalog []ResourceGroup, id string) bool {
	for _, group := range catalog {
		for _, resource := range group.Resources {
			if resource.ID == id {
				return true
			}
		}
	}
	return false
}

func TestDiscoveryPublishesPartialThenReplacesWithCompleteSnapshot(t *testing.T) {
	secondAttempt := make(chan struct{})
	client := &sequenceDiscovery{called: make(chan int, 2), results: []discoveryResult{
		{resources: []*metav1.APIResourceList{discoveryList("v1", metav1.APIResource{Name: "pods", Kind: "Pod", Namespaced: true})}, err: discoveryPartialError()},
		{resources: []*metav1.APIResourceList{
			discoveryList("v1", metav1.APIResource{Name: "pods", Kind: "Pod", Namespaced: true}),
			discoveryList("surfsk8s.dev/v1alpha1", metav1.APIResource{Name: "widgets", Kind: "Widget", Namespaced: true}),
		}, wait: secondAttempt},
	}}
	manager, conn := newDiscoveryTestManager(client, 0)
	done := make(chan struct{})
	go func() {
		manager.discoverResources(conn.Context, conn)
		close(done)
	}()

	if call := <-client.called; call != 1 {
		t.Fatalf("first call = %d", call)
	}
	if call := <-client.called; call != 2 {
		t.Fatalf("second call = %d", call)
	}
	if !catalogHasResource(manager.Catalog(), "/pods") {
		t.Fatal("partial Pods were not published before retry completed")
	}
	if got, _ := conn.warning.Load().(string); got != "discovery-partial" {
		t.Fatalf("partial warning = %q", got)
	}
	if got := manager.Version(); got != 1 {
		t.Fatalf("partial version = %d, want 1", got)
	}

	close(secondAttempt)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("discovery did not complete")
	}
	if !catalogHasResource(manager.Catalog(), "surfsk8s.dev/widgets") {
		t.Fatal("Widget missing after complete retry")
	}
	if got, _ := conn.warning.Load().(string); got != "" {
		t.Fatalf("complete warning = %q", got)
	}
	if got := manager.Version(); got != 2 {
		t.Fatalf("complete version = %d, want 2", got)
	}
	conn.genericMu.Lock()
	defer conn.genericMu.Unlock()
	if got := len(conn.genericWatches); got != 0 {
		t.Fatalf("discovery started %d generic watches", got)
	}
}

func TestDiscoveryPersistentEquivalentPartialUsesFourAttemptsWithoutNoisyVersions(t *testing.T) {
	partial := discoveryResult{resources: []*metav1.APIResourceList{
		discoveryList("v1",
			metav1.APIResource{Name: "services", Kind: "Service", Namespaced: true},
			metav1.APIResource{Name: "pods", Kind: "Pod", Namespaced: true},
			metav1.APIResource{Name: "pods", Kind: "Pod", Namespaced: true},
		),
	}, err: discoveryPartialError()}
	reordered := discoveryResult{resources: []*metav1.APIResourceList{
		discoveryList("v1",
			metav1.APIResource{Name: "pods", Kind: "Pod", Namespaced: true},
			metav1.APIResource{Name: "services", Kind: "Service", Namespaced: true},
		),
	}, err: discoveryPartialError()}
	client := &sequenceDiscovery{results: []discoveryResult{partial, reordered, partial, reordered}}
	manager, conn := newDiscoveryTestManager(client, 0, 0, 0)
	manager.discoverResources(conn.Context, conn)

	if got := client.calls.Load(); got != 4 {
		t.Fatalf("discovery calls = %d, want 4", got)
	}
	if got := manager.Version(); got != 1 {
		t.Fatalf("version = %d, want one semantic update", got)
	}
	if got, _ := conn.warning.Load().(string); got != "discovery-partial" {
		t.Fatalf("warning = %q", got)
	}
	resources := manager.resources[conn.Name]
	if got := len(resources); got != 2 {
		t.Fatalf("normalized resources = %d, want 2", got)
	}
	if resources[0].Resource != "pods" || resources[1].Resource != "services" {
		t.Fatalf("resources not deterministically sorted: %#v", resources)
	}
}

func TestDiscoveryRetryDoesNotStartWatchesAndDemandStartsOneWatch(t *testing.T) {
	client := &sequenceDiscovery{results: []discoveryResult{{resources: []*metav1.APIResourceList{
		discoveryList("surfsk8s.dev/v1alpha1", metav1.APIResource{Name: "widgets", Kind: "Widget", Namespaced: true}),
	}}}}
	manager, conn := newDiscoveryTestManager(client)
	defer conn.Cancel()
	manager.discoverResources(conn.Context, conn)

	resource := ResourceKind{ID: "surfsk8s.dev/widgets", APIGroup: "surfsk8s.dev", Version: "v1alpha1", Resource: "widgets", Kind: "Widget", Namespaced: true}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := manager.ListGenericResource(context.Background(), resource); err != nil {
			t.Fatalf("demand list %d: %v", attempt+1, err)
		}
	}
	conn.genericMu.Lock()
	defer conn.genericMu.Unlock()
	if got := len(conn.genericWatches); got != 1 {
		t.Fatalf("generic watches after repeated demand = %d, want 1", got)
	}
}

func TestDiscoveryErrorClassification(t *testing.T) {
	tooManyRequests := &apierrors.StatusError{ErrStatus: metav1.Status{Code: 429}}
	for name, testCase := range map[string]struct {
		err       error
		status    string
		retryable bool
	}{
		"not found": {err: apierrors.NewNotFound(schema.GroupResource{Group: "example.dev", Resource: "widgets"}, ""), status: "discovery-not-found", retryable: true},
		"timeout":   {err: context.DeadlineExceeded, status: "discovery-error", retryable: true},
		"429":       {err: tooManyRequests, status: "discovery-error", retryable: true},
		"forbidden partial": {err: &discovery.ErrGroupDiscoveryFailed{Groups: map[schema.GroupVersion]error{
			{Group: "private.dev", Version: "v1"}: apierrors.NewForbidden(schema.GroupResource{Group: "private.dev", Resource: "widgets"}, "", errors.New("denied")),
		}}, status: "discovery-partial", retryable: false},
	} {
		t.Run(name, func(t *testing.T) {
			status, retryable := classifyDiscoveryError(testCase.err)
			if status != testCase.status || retryable != testCase.retryable {
				t.Fatalf("classification = (%q, %t), want (%q, %t)", status, retryable, testCase.status, testCase.retryable)
			}
		})
	}
}

func TestDiscoveryCancellationDuringBackoffReturnsWithoutStatusMutation(t *testing.T) {
	client := &sequenceDiscovery{called: make(chan int, 1), results: []discoveryResult{{
		resources: []*metav1.APIResourceList{discoveryList("v1", metav1.APIResource{Name: "pods", Kind: "Pod"})},
		err:       discoveryPartialError(),
	}}}
	manager, conn := newDiscoveryTestManager(client, time.Hour, time.Hour, time.Hour)
	done := make(chan struct{})
	go func() {
		manager.discoverResources(conn.Context, conn)
		close(done)
	}()
	<-client.called
	publishDeadline := time.Now().Add(250 * time.Millisecond)
	for manager.Version() == 0 {
		if time.Now().After(publishDeadline) {
			t.Fatal("partial discovery result was not published")
		}
		time.Sleep(time.Millisecond)
	}
	version := manager.Version()
	warning, _ := conn.warning.Load().(string)
	started := time.Now()
	conn.Cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("cancellation did not interrupt discovery backoff")
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("cancellation took %s", elapsed)
	}
	if got := client.calls.Load(); got != 1 {
		t.Fatalf("calls after cancellation = %d", got)
	}
	currentWarning, _ := conn.warning.Load().(string)
	if manager.Version() != version || currentWarning != warning {
		t.Fatal("cancellation performed a final status mutation")
	}
}

func TestCatalogUsesConnectionOrderForFirstWinsMetadata(t *testing.T) {
	manager := &Manager{
		conns:     map[string]*ClusterConn{"alpha": {}, "zeta": {}},
		connOrder: []string{"alpha", "zeta"},
		resources: map[string][]discoveredResource{
			"zeta":  {{APIGroup: "example.dev", Version: "v2", Resource: "widgets", Kind: "WidgetV2"}},
			"alpha": {{APIGroup: "example.dev", Version: "v1", Resource: "widgets", Kind: "Widget"}},
		},
	}
	for run := 0; run < 20; run++ {
		catalog := manager.Catalog()
		found := false
		for _, group := range catalog {
			for _, resource := range group.Resources {
				if resource.ID != "example.dev/widgets" {
					continue
				}
				found = true
				if resource.Version != "v1" || resource.Kind != "Widget" {
					t.Fatalf("run %d selected nondeterministic metadata: %#v", run, resource)
				}
			}
		}
		if !found {
			t.Fatalf("run %d did not include Widget", run)
		}
	}
}
