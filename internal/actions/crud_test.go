package actions

import (
	"os"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestExecPodShellInContainerBuildsKubectlCommand(t *testing.T) {
	executor := NewExecutor("/tmp/config")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-123",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sidecar"}, {Name: "api"}}},
	}

	cmd, desc, err := executor.ExecPodShellInContainer(state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api-123"},
		Pod: pod,
	}, "api")
	if err != nil {
		t.Fatalf("ExecPodShellInContainer error: %v", err)
	}
	if got, want := desc, "exec pod/api-123"; got != want {
		t.Fatalf("desc = %q, want %q", got, want)
	}
	wantArgs := []string{"kubectl", "--kubeconfig", "/tmp/config", "--context", "dev", "exec", "-it", "-n", "default", "pod/api-123", "-c", "api", "--", "sh"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestScaleDeploymentBuildsKubectlCommand(t *testing.T) {
	executor := NewExecutor("")
	replicas := int32(3)
	deployment := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &replicas}}

	cmd, desc, err := executor.ScaleDeployment(state.DeploymentDetails{
		Row:        state.DeploymentRow{Cluster: "prod", Namespace: "apps", Name: "frontend"},
		Deployment: deployment,
	}, 5)
	if err != nil {
		t.Fatalf("ScaleDeployment error: %v", err)
	}
	if got, want := desc, "scale deployment/frontend to 5"; got != want {
		t.Fatalf("desc = %q, want %q", got, want)
	}
	wantArgs := []string{"kubectl", "--context", "prod", "scale", "-n", "apps", "deployment/frontend", "--replicas", "5"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestPortForwardServiceWithPortsBuildsKubectlCommand(t *testing.T) {
	executor := NewExecutor("")
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 8080}, {Port: 9090}}},
	}

	cmd, desc, err := executor.PortForwardServiceWithPorts(state.ServiceDetails{
		Row:     state.ServiceRow{Cluster: "dev", Namespace: "mesh", Name: "gateway"},
		Service: service,
	}, 9000, 8080)
	if err != nil {
		t.Fatalf("PortForwardServiceWithPorts error: %v", err)
	}
	if got, want := desc, "port-forward service/gateway 9000:8080"; got != want {
		t.Fatalf("desc = %q, want %q", got, want)
	}
	wantArgs := []string{"kubectl", "--context", "dev", "port-forward", "-n", "mesh", "service/gateway", "9000:8080"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestExecNodeShellBuildsKubectlDebugCommand(t *testing.T) {
	executor := NewExecutor("/tmp/config")
	cmd, desc, err := executor.ExecNodeShell(state.NodeDetails{
		Row:  state.NodeRow{Cluster: "dev", Name: "ip-10-0-0-1"},
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ip-10-0-0-1"}},
	})
	if err != nil {
		t.Fatalf("ExecNodeShell error: %v", err)
	}
	if got, want := desc, "debug node/ip-10-0-0-1"; got != want {
		t.Fatalf("desc = %q, want %q", got, want)
	}
	wantArgs := []string{"kubectl", "--kubeconfig", "/tmp/config", "--context", "dev", "debug", "node/ip-10-0-0-1", "-it", "--profile=sysadmin", "--image=ubuntu:24.04", "--", "sh", "-lc", "chroot /host /bin/bash || chroot /host /bin/sh || exec bash || exec sh"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestExecPodShellReturnsErrorForEmptyContext(t *testing.T) {
	executor := NewExecutor("")
	_, _, err := executor.ExecPodShellInContainer(state.PodDetails{
		Row: state.PodRow{Cluster: "", Namespace: "default", Name: "api-123"},
		Pod: &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api-123", Namespace: "default"},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}},
		},
	}, "api")
	if err == nil {
		t.Fatalf("expected missing context error")
	}
	if got, want := err.Error(), "missing cluster context"; got != want {
		t.Fatalf("err = %q, want %q", got, want)
	}
}

func TestExecNodeShellReturnsErrorForEmptyContext(t *testing.T) {
	executor := NewExecutor("")
	_, _, err := executor.ExecNodeShell(state.NodeDetails{
		Row:  state.NodeRow{Cluster: "", Name: "ip-10-0-0-1"},
		Node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ip-10-0-0-1"}},
	})
	if err == nil {
		t.Fatalf("expected missing context error")
	}
	if got, want := err.Error(), "missing cluster context"; got != want {
		t.Fatalf("err = %q, want %q", got, want)
	}
}

func TestPodContainerNamesReturnsAllContainers(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web"}, {Name: "sidecar"}}}}
	names, err := PodContainerNames(pod)
	if err != nil {
		t.Fatalf("PodContainerNames error: %v", err)
	}
	want := []string{"web", "sidecar"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %#v, want %#v", names, want)
	}
}

func TestPodPortChoicesIncludeContainerLabels(t *testing.T) {
	pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		Name:  "web",
		Ports: []corev1.ContainerPort{{ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
	}, {
		Name:  "metrics",
		Ports: []corev1.ContainerPort{{ContainerPort: 9090, Protocol: corev1.ProtocolTCP}},
	}}}}
	choices, err := PodPortChoices(pod)
	if err != nil {
		t.Fatalf("PodPortChoices error: %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("choices = %d, want 2", len(choices))
	}
	if got, want := choices[1].Label, "9090/TCP  (metrics)"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestServicePortChoicesIncludePortName(t *testing.T) {
	service := &corev1.Service{Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP}}}}
	choices, err := ServicePortChoices(service)
	if err != nil {
		t.Fatalf("ServicePortChoices error: %v", err)
	}
	if got, want := choices[0].Label, "8080/TCP  (http)"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestDesiredReplicasFallsBackToOne(t *testing.T) {
	deployment := &appsv1.Deployment{}
	if got, want := DesiredReplicas(deployment), 1; got != want {
		t.Fatalf("desiredReplicas = %d, want %d", got, want)
	}
}

func TestEditPodBuildsEditorApplyCommandWithScrubbedManifest(t *testing.T) {
	executor := NewExecutor("/tmp/config")
	cmd, desc, err := executor.EditPod(state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api"},
		Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name:              "api",
			Namespace:         "default",
			ResourceVersion:   "42",
			UID:               "abc",
			CreationTimestamp: metav1.Now(),
			ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "kubectl"}},
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
				"keep": "yes",
			},
		}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
	})
	if err != nil {
		t.Fatalf("EditPod error: %v", err)
	}
	if got, want := desc, "edit pod/api manifest"; got != want {
		t.Fatalf("desc = %q, want %q", got, want)
	}
	if got, want := cmd.Args[0], "sh"; got != want {
		t.Fatalf("argv0 = %q, want %q", got, want)
	}
	if !strings.Contains(cmd.Args[2], "kubectl \"$@\" apply -f \"$file\"") {
		t.Fatalf("script = %q", cmd.Args[2])
	}
	content, err := os.ReadFile(cmd.Args[4])
	if err != nil {
		t.Fatalf("read temp manifest: %v", err)
	}
	manifest := string(content)
	if strings.Contains(manifest, "resourceVersion") {
		t.Fatalf("manifest still has resourceVersion: %s", manifest)
	}
	if strings.Contains(manifest, "managedFields") {
		t.Fatalf("manifest still has managedFields: %s", manifest)
	}
	if strings.Contains(manifest, "status:") {
		t.Fatalf("manifest still has status: %s", manifest)
	}
	if strings.Contains(manifest, "last-applied-configuration") {
		t.Fatalf("manifest still has last-applied annotation: %s", manifest)
	}
	if !strings.Contains(manifest, "keep:") {
		t.Fatalf("manifest dropped editable annotations: %s", manifest)
	}
	_ = os.Remove(cmd.Args[4])
}

func TestScrubManifestObjectRemovesServerManagedFields(t *testing.T) {
	manifest := scrubManifestObject(&unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "serving.knative.dev/v1",
		"kind":       "Service",
		"metadata": map[string]interface{}{
			"name":              "api",
			"namespace":         "default",
			"creationTimestamp": "2024-01-01T00:00:00Z",
			"generation":        int64(7),
			"resourceVersion":   "99",
			"uid":               "abc",
			"annotations": map[string]interface{}{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
				"keep": "yes",
			},
		},
		"spec":   map[string]interface{}{"template": map[string]interface{}{}},
		"status": map[string]interface{}{"url": "https://example.com"},
	}})
	if _, ok := manifest.Object["status"]; ok {
		t.Fatalf("status not removed")
	}
	metadata := manifest.Object["metadata"].(map[string]interface{})
	for _, key := range []string{"creationTimestamp", "generation", "resourceVersion", "uid"} {
		if _, ok := metadata[key]; ok {
			t.Fatalf("metadata.%s not removed", key)
		}
	}
	annotations := metadata["annotations"].(map[string]interface{})
	if _, ok := annotations["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		t.Fatalf("last-applied annotation not removed")
	}
	if got, want := annotations["keep"], "yes"; got != want {
		t.Fatalf("keep annotation = %v, want %v", got, want)
	}
}

func TestEditGenericResourceRequiresObject(t *testing.T) {
	executor := NewExecutor("")
	_, _, err := executor.EditGenericResource(cluster.ResourceKind{Resource: "revisions"}, cluster.GenericResourceDetails{Row: cluster.GenericResourceRow{Name: "api", Cluster: "dev"}})
	if err == nil {
		t.Fatalf("expected missing object error")
	}
	if got, want := err.Error(), "resource disappeared"; got != want {
		t.Fatalf("err = %q, want %q", got, want)
	}
}
