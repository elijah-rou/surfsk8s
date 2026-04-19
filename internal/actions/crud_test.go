package actions

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
