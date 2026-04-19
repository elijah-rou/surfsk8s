package actions

import (
	"fmt"
	"os/exec"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/elijahrou/surfsk8s/internal/state"
)

const defaultContainerAnnotation = "kubectl.kubernetes.io/default-container"

// Executor builds real kubectl operations against specific cluster contexts.
// Interactive actions like exec, edit, and port-forward are intended to run
// via tea.ExecProcess so Bubble Tea can suspend and restore the terminal.
type Executor struct {
	kubeconfigPath string
}

func NewExecutor(kubeconfigPath string) *Executor {
	return &Executor{kubeconfigPath: kubeconfigPath}
}

func (e *Executor) ExecPodShell(details state.PodDetails) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	container, err := defaultContainerName(details.Pod)
	if err != nil {
		return nil, "", err
	}

	args := e.baseArgs(details.Row.Cluster)
	args = append(args,
		"exec", "-it",
		"-n", details.Row.Namespace,
		"pod/"+details.Row.Name,
		"-c", container,
		"--", "sh",
	)
	return exec.Command("kubectl", args...), fmt.Sprintf("exec pod/%s", details.Row.Name), nil
}

func (e *Executor) EditPod(details state.PodDetails) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	args := e.baseArgs(details.Row.Cluster)
	args = append(args, "edit", "-n", details.Row.Namespace, "pod/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("edit pod/%s", details.Row.Name), nil
}

func (e *Executor) PortForwardPod(details state.PodDetails) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	port, err := firstPodPort(details.Pod)
	if err != nil {
		return nil, "", err
	}
	mapping := strconv.Itoa(port) + ":" + strconv.Itoa(port)
	args := e.baseArgs(details.Row.Cluster)
	args = append(args, "port-forward", "-n", details.Row.Namespace, "pod/"+details.Row.Name, mapping)
	return exec.Command("kubectl", args...), fmt.Sprintf("port-forward pod/%s %s", details.Row.Name, mapping), nil
}

func (e *Executor) ScaleDeployment(details state.DeploymentDetails, replicas int) (*exec.Cmd, string, error) {
	if details.Deployment == nil {
		return nil, "", fmt.Errorf("deployment disappeared")
	}
	if replicas < 0 {
		return nil, "", fmt.Errorf("replicas must be >= 0")
	}
	args := e.baseArgs(details.Row.Cluster)
	args = append(args,
		"scale",
		"-n", details.Row.Namespace,
		"deployment/"+details.Row.Name,
		"--replicas", strconv.Itoa(replicas),
	)
	return exec.Command("kubectl", args...), fmt.Sprintf("scale deployment/%s to %d", details.Row.Name, replicas), nil
}

func (e *Executor) RestartDeployment(details state.DeploymentDetails) (*exec.Cmd, string, error) {
	if details.Deployment == nil {
		return nil, "", fmt.Errorf("deployment disappeared")
	}
	args := e.baseArgs(details.Row.Cluster)
	args = append(args, "rollout", "restart", "-n", details.Row.Namespace, "deployment/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("restart deployment/%s", details.Row.Name), nil
}

func (e *Executor) EditDeployment(details state.DeploymentDetails) (*exec.Cmd, string, error) {
	if details.Deployment == nil {
		return nil, "", fmt.Errorf("deployment disappeared")
	}
	args := e.baseArgs(details.Row.Cluster)
	args = append(args, "edit", "-n", details.Row.Namespace, "deployment/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("edit deployment/%s", details.Row.Name), nil
}

func (e *Executor) PortForwardService(details state.ServiceDetails) (*exec.Cmd, string, error) {
	if details.Service == nil {
		return nil, "", fmt.Errorf("service disappeared")
	}
	port, err := firstServicePort(details.Service)
	if err != nil {
		return nil, "", err
	}
	mapping := strconv.Itoa(port) + ":" + strconv.Itoa(port)
	args := e.baseArgs(details.Row.Cluster)
	args = append(args, "port-forward", "-n", details.Row.Namespace, "service/"+details.Row.Name, mapping)
	return exec.Command("kubectl", args...), fmt.Sprintf("port-forward service/%s %s", details.Row.Name, mapping), nil
}

func (e *Executor) EditService(details state.ServiceDetails) (*exec.Cmd, string, error) {
	if details.Service == nil {
		return nil, "", fmt.Errorf("service disappeared")
	}
	args := e.baseArgs(details.Row.Cluster)
	args = append(args, "edit", "-n", details.Row.Namespace, "service/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("edit service/%s", details.Row.Name), nil
}

func (e *Executor) EditNode(details state.NodeDetails) (*exec.Cmd, string, error) {
	if details.Node == nil {
		return nil, "", fmt.Errorf("node disappeared")
	}
	args := e.baseArgs(details.Row.Cluster)
	args = append(args, "edit", "node/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("edit node/%s", details.Row.Name), nil
}

func (e *Executor) baseArgs(contextName string) []string {
	if contextName == "" {
		panic("actions.Executor.baseArgs: empty contextName")
	}
	args := make([]string, 0, 4)
	if e.kubeconfigPath != "" {
		args = append(args, "--kubeconfig", e.kubeconfigPath)
	}
	args = append(args, "--context", contextName)
	return args
}

func defaultContainerName(pod *corev1.Pod) (string, error) {
	if pod == nil {
		panic("actions.defaultContainerName: nil pod")
	}
	if len(pod.Spec.Containers) == 0 {
		return "", fmt.Errorf("pod has no containers")
	}
	if preferred := pod.Annotations[defaultContainerAnnotation]; preferred != "" {
		for _, container := range pod.Spec.Containers {
			if container.Name == preferred {
				return preferred, nil
			}
		}
	}
	return pod.Spec.Containers[0].Name, nil
}

func firstPodPort(pod *corev1.Pod) (int, error) {
	if pod == nil {
		panic("actions.firstPodPort: nil pod")
	}
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.ContainerPort > 0 {
				return int(port.ContainerPort), nil
			}
		}
	}
	return 0, fmt.Errorf("pod has no declared container ports")
}

func firstServicePort(service *corev1.Service) (int, error) {
	if service == nil {
		panic("actions.firstServicePort: nil service")
	}
	for _, port := range service.Spec.Ports {
		if port.Port > 0 {
			return int(port.Port), nil
		}
	}
	return 0, fmt.Errorf("service has no declared ports")
}

func DesiredReplicas(deployment *appsv1.Deployment) int {
	if deployment == nil {
		panic("actions.DesiredReplicas: nil deployment")
	}
	if deployment.Spec.Replicas == nil {
		return 1
	}
	return int(*deployment.Spec.Replicas)
}
