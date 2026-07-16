package cluster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestManagerCloseCompletesWhenDiscoveryStalls(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "config")
	content := `apiVersion: v1
kind: Config
current-context: stall
clusters:
- cluster:
    server: ` + server.URL + `
  name: stall-cluster
contexts:
- context:
    cluster: stall-cluster
    user: stall-user
  name: stall
users:
- name: stall-user
  user:
    token: test
`
	if err := os.WriteFile(kubeconfigPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	store := state.NewStore()
	manager, err := NewManager(store, Config{
		KubeconfigPath:   kubeconfigPath,
		DiscoveryTimeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.Connect(ctx, []string{"stall"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatalf("discovery request did not start")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Close()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Manager.Close blocked while discovery stalled")
	}
}
