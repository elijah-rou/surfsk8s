package cluster

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest/fake"

	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestDeploymentPodDetailsMatchesSelectorAndSorts(t *testing.T) {
	store := state.NewStore()
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-b", Namespace: "web", Labels: map[string]string{"app": "frontend"}}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-a", Namespace: "web", Labels: map[string]string{"app": "frontend"}}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-a", Namespace: "web", Labels: map[string]string{"app": "api"}}})

	manager := &Manager{store: store}
	pods, err := manager.DeploymentPodDetails(state.DeploymentDetails{
		Row:        state.DeploymentRow{Cluster: "dev", Namespace: "web", Name: "frontend"},
		Deployment: &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}}}},
	}, time.Now())
	if err != nil {
		t.Fatalf("DeploymentPodDetails error: %v", err)
	}
	got := []string{pods[0].Row.Name, pods[1].Row.Name}
	want := []string{"frontend-a", "frontend-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pods = %#v, want %#v", got, want)
	}
}

func TestDeploymentLogsPrefixesEachPodSection(t *testing.T) {
	store := state.NewStore()
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-a", Namespace: "web", Labels: map[string]string{"app": "frontend"}}})
	store.UpsertPod("dev", &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "frontend-b", Namespace: "web", Labels: map[string]string{"app": "frontend"}}})

	manager := &Manager{
		store: store,
		conns: map[string]*ClusterConn{
			"dev": {Name: "dev", Clientset: kubernetesfake.NewSimpleClientset()},
		},
	}

	content, err := manager.DeploymentLogs(context.Background(), state.DeploymentDetails{
		Row: state.DeploymentRow{Cluster: "dev", Namespace: "web", Name: "frontend"},
		Deployment: &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "frontend"}},
			},
		},
	}, "main", 50, true, time.Now())
	if err != nil {
		t.Fatalf("DeploymentLogs error: %v", err)
	}
	for _, fragment := range []string{"== pod/frontend-a ==", "== pod/frontend-b ==", "fake logs"} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("content missing %q:\n%s", fragment, content)
		}
	}
}

func TestPodLogsReadsFakeClientLogStream(t *testing.T) {
	manager := &Manager{
		conns: map[string]*ClusterConn{
			"dev": {Name: "dev", Clientset: kubernetesfake.NewSimpleClientset()},
		},
	}
	content, err := manager.PodLogs(context.Background(), state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api"},
		Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}},
	}, "main", 25, true)
	if err != nil {
		t.Fatalf("PodLogs error: %v", err)
	}
	if got, want := content, "fake logs"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestPodLogsRejectsNonPositiveLimitBytes(t *testing.T) {
	limit := int64(0)
	manager := &Manager{}
	_, err := manager.PodLogsWithOptions(context.Background(), state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api"},
		Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}},
	}, PodLogsOptions{Container: "main", LimitBytes: &limit})
	if err == nil || !strings.Contains(err.Error(), "limit bytes") {
		t.Fatalf("err = %v, want limit bytes error", err)
	}
}

func TestNodeLogUsesRangeHeaderAndPath(t *testing.T) {
	var gotPath string
	var gotRange string
	manager := &Manager{
		conns: map[string]*ClusterConn{
			"dev": {
				Name: "dev",
				REST: &fake.RESTClient{
					GroupVersion:         schema.GroupVersion{Version: "v1"},
					NegotiatedSerializer: scheme.Codecs.WithoutConversion(),
					VersionedAPIPath:     "/",
					Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
						gotPath = req.URL.Path
						gotRange = req.Header.Get("Range")
						return &http.Response{StatusCode: http.StatusPartialContent, Body: io.NopCloser(strings.NewReader("tail content")), Header: make(http.Header)}, nil
					}),
				},
			},
		},
	}

	content, err := manager.NodeLog(context.Background(), state.NodeDetails{
		Row:  state.NodeRow{Cluster: "dev", Name: "node-a"},
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
	}, "cloud-init.log", 512)
	if err != nil {
		t.Fatalf("NodeLog error: %v", err)
	}
	if got, want := content, "tail content"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got, want := gotPath, "/api/v1/nodes/node-a/proxy/logs/cloud-init.log"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := gotRange, "bytes=-512"; got != want {
		t.Fatalf("range = %q, want %q", got, want)
	}
}

func TestParseNodeLogEntriesPrioritizesUsefulDefaults(t *testing.T) {
	entries, err := parseNodeLogEntries("", []byte(`<!doctype html><pre>
<a href="README">README</a>
<a href="journal/">journal/</a>
<a href="cloud-init-output.log">cloud-init-output.log</a>
<a href="cloud-init.log">cloud-init.log</a>
<a href="containers/">containers/</a>
</pre>`))
	if err != nil {
		t.Fatalf("parseNodeLogEntries error: %v", err)
	}
	got := []string{entries[0].Path, entries[1].Path, entries[2].Path, entries[3].Path, entries[4].Path}
	want := []string{"cloud-init.log", "cloud-init-output.log", "containers/", "journal/", "README"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}

func TestCleanNodeLogPathRejectsTraversal(t *testing.T) {
	if _, err := cleanNodeLogPath("../secret"); err == nil {
		t.Fatalf("expected traversal error")
	}
	if _, err := cleanNodeLogDir("../../var/log"); err == nil {
		t.Fatalf("expected traversal error")
	}
}
