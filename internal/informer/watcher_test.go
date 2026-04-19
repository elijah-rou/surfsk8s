package informer

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestDeletedPodFromObjectHandlesTombstone(t *testing.T) {
	watcher := &Watcher{}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend", Namespace: "web"}}
	tombstone := cache.DeletedFinalStateUnknown{Key: "web/frontend", Obj: pod}

	deleted, ok := watcher.deletedPodFromObject(tombstone)
	if !ok {
		t.Fatalf("expected tombstone to decode")
	}
	if deleted != pod {
		t.Fatalf("expected original pod pointer")
	}
}

func TestDecodeErrorsCounted(t *testing.T) {
	watcher := &Watcher{}
	if _, ok := watcher.podFromObject("bad"); ok {
		t.Fatalf("expected bad object to fail decode")
	}
	if got, want := watcher.DecodeErrorCount(), uint64(1); got != want {
		t.Fatalf("decodeErrors = %d, want %d", got, want)
	}
}

func TestResyncPeriodConfigured(t *testing.T) {
	if got, want := resyncPeriod, 5*time.Minute; got != want {
		t.Fatalf("resyncPeriod = %s, want %s", got, want)
	}
}
