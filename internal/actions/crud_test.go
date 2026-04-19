package actions

import (
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/elijahrou/surfsk8s/internal/state"
)

func TestExecPodShellBuildsKubectlCommand(t *testing.T) {
	executor := NewExecutor("/tmp/config")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "api-123",
			Namespace:   "default",
			Annotations: map[string]string{defaultContainerAnnotation: "api"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sidecar"}, {Name: "api"}}},
	}

	cmd, desc, err := executor.ExecPodShell(state.PodDetails{
		Row: state.PodRow{Cluster: "dev", Namespace: "default", Name: "api-123"},
		Pod: pod,
	})
	if err != nil {
		t.Fatalf("ExecPodShell error: %v", err)
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

func TestPortForwardServiceUsesFirstDeclaredPort(t *testing.T) {
	executor := NewExecutor("")
	service := &corev1.Service{
		Spec: corev1.ServiceSpec{Ports: []corev1.ServicePort{{Port: 8080}, {Port: 9090}}},
	}

	cmd, desc, err := executor.PortForwardService(state.ServiceDetails{
		Row:     state.ServiceRow{Cluster: "dev", Namespace: "mesh", Name: "gateway"},
		Service: service,
	})
	if err != nil {
		t.Fatalf("PortForwardService error: %v", err)
	}
	if got, want := desc, "port-forward service/gateway 8080:8080"; got != want {
		t.Fatalf("desc = %q, want %q", got, want)
	}
	wantArgs := []string{"kubectl", "--context", "dev", "port-forward", "-n", "mesh", "service/gateway", "8080:8080"}
	if !reflect.DeepEqual(cmd.Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", cmd.Args, wantArgs)
	}
}

func TestDesiredReplicasFallsBackToOne(t *testing.T) {
	deployment := &appsv1.Deployment{}
	if got, want := DesiredReplicas(deployment), 1; got != want {
		t.Fatalf("desiredReplicas = %d, want %d", got, want)
	}
}
