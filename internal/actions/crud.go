package actions

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	"github.com/elijahrou/surfsk8s/internal/cluster"
	"github.com/elijahrou/surfsk8s/internal/state"
)

const defaultContainerAnnotation = "kubectl.kubernetes.io/default-container"
const defaultNodeDebugImage = "ubuntu:24.04"

const manifestEditScript = `set -eu
file="$1"
shift
trap 'rm -f "$file"' EXIT
editor="${VISUAL:-${EDITOR:-vi}}"
# shellcheck disable=SC2086
$editor "$file"
kubectl "$@" apply -f "$file"
`

// Executor builds real kubectl operations against specific cluster contexts.
// Interactive actions like exec, edit, and port-forward are intended to run
// via tea.ExecProcess so Bubble Tea can suspend and restore the terminal.
type Executor struct {
	kubeconfigPath string
}

type PortChoice struct {
	Port  int
	Label string
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
	return e.ExecPodShellInContainer(details, container)
}

func (e *Executor) ExecPodShellInContainer(details state.PodDetails, container string) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	if !podHasContainer(details.Pod, container) {
		return nil, "", fmt.Errorf("pod container %q not found", container)
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	args = append(args,
		"exec", "-it",
		"-n", details.Row.Namespace,
		"pod/"+details.Row.Name,
		"-c", container,
		"--", "sh",
	)
	return exec.Command("kubectl", args...), fmt.Sprintf("exec pod/%s", details.Row.Name), nil
}

func (e *Executor) ExecNodeShell(details state.NodeDetails) (*exec.Cmd, string, error) {
	if details.Node == nil {
		return nil, "", fmt.Errorf("node disappeared")
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	args = append(args,
		"debug", "node/"+details.Row.Name,
		"-it",
		"--profile=sysadmin",
		"--image="+defaultNodeDebugImage,
		"--",
		"sh", "-lc", "chroot /host /bin/bash || chroot /host /bin/sh || exec bash || exec sh",
	)
	return exec.Command("kubectl", args...), fmt.Sprintf("debug node/%s", details.Row.Name), nil
}

func (e *Executor) EditPod(details state.PodDetails) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	object, err := typedManifestObject(details.Pod, "v1", "Pod")
	if err != nil {
		return nil, "", err
	}
	return e.editManifest(details.Row.Cluster, fmt.Sprintf("edit pod/%s manifest", details.Row.Name), object)
}

func (e *Executor) PortForwardPod(details state.PodDetails) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	port, err := firstPodPort(details.Pod)
	if err != nil {
		return nil, "", err
	}
	return e.PortForwardPodWithPorts(details, port, port)
}

func (e *Executor) PortForwardPodWithPorts(details state.PodDetails, localPort int, remotePort int) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	if err := validatePort(localPort, "local port"); err != nil {
		return nil, "", err
	}
	if err := validatePort(remotePort, "remote port"); err != nil {
		return nil, "", err
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	mapping := portMapping(localPort, remotePort)
	args = append(args, "port-forward", "-n", details.Row.Namespace, "pod/"+details.Row.Name, mapping)
	return exec.Command("kubectl", args...), fmt.Sprintf("port-forward pod/%s %s", details.Row.Name, mapping), nil
}

func (e *Executor) DeletePod(details state.PodDetails) (*exec.Cmd, string, error) {
	if details.Pod == nil {
		return nil, "", fmt.Errorf("pod disappeared")
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	args = append(args, "delete", "-n", details.Row.Namespace, "pod/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("delete pod/%s", details.Row.Name), nil
}

func (e *Executor) ScaleDeployment(details state.DeploymentDetails, replicas int) (*exec.Cmd, string, error) {
	if details.Deployment == nil {
		return nil, "", fmt.Errorf("deployment disappeared")
	}
	if replicas < 0 {
		return nil, "", fmt.Errorf("replicas must be >= 0")
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
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
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	args = append(args, "rollout", "restart", "-n", details.Row.Namespace, "deployment/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("restart deployment/%s", details.Row.Name), nil
}

func (e *Executor) DeleteDeployment(details state.DeploymentDetails) (*exec.Cmd, string, error) {
	if details.Deployment == nil {
		return nil, "", fmt.Errorf("deployment disappeared")
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	args = append(args, "delete", "-n", details.Row.Namespace, "deployment/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("delete deployment/%s", details.Row.Name), nil
}

func (e *Executor) EditDeployment(details state.DeploymentDetails) (*exec.Cmd, string, error) {
	if details.Deployment == nil {
		return nil, "", fmt.Errorf("deployment disappeared")
	}
	object, err := typedManifestObject(details.Deployment, "apps/v1", "Deployment")
	if err != nil {
		return nil, "", err
	}
	return e.editManifest(details.Row.Cluster, fmt.Sprintf("edit deployment/%s manifest", details.Row.Name), object)
}

func (e *Executor) PortForwardService(details state.ServiceDetails) (*exec.Cmd, string, error) {
	if details.Service == nil {
		return nil, "", fmt.Errorf("service disappeared")
	}
	port, err := firstServicePort(details.Service)
	if err != nil {
		return nil, "", err
	}
	return e.PortForwardServiceWithPorts(details, port, port)
}

func (e *Executor) PortForwardServiceWithPorts(details state.ServiceDetails, localPort int, remotePort int) (*exec.Cmd, string, error) {
	if details.Service == nil {
		return nil, "", fmt.Errorf("service disappeared")
	}
	if err := validatePort(localPort, "local port"); err != nil {
		return nil, "", err
	}
	if err := validatePort(remotePort, "remote port"); err != nil {
		return nil, "", err
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	mapping := portMapping(localPort, remotePort)
	args = append(args, "port-forward", "-n", details.Row.Namespace, "service/"+details.Row.Name, mapping)
	return exec.Command("kubectl", args...), fmt.Sprintf("port-forward service/%s %s", details.Row.Name, mapping), nil
}

func (e *Executor) EditService(details state.ServiceDetails) (*exec.Cmd, string, error) {
	if details.Service == nil {
		return nil, "", fmt.Errorf("service disappeared")
	}
	object, err := typedManifestObject(details.Service, "v1", "Service")
	if err != nil {
		return nil, "", err
	}
	return e.editManifest(details.Row.Cluster, fmt.Sprintf("edit service/%s manifest", details.Row.Name), object)
}

func (e *Executor) DeleteService(details state.ServiceDetails) (*exec.Cmd, string, error) {
	if details.Service == nil {
		return nil, "", fmt.Errorf("service disappeared")
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	args = append(args, "delete", "-n", details.Row.Namespace, "service/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("delete service/%s", details.Row.Name), nil
}

func (e *Executor) EditNode(details state.NodeDetails) (*exec.Cmd, string, error) {
	if details.Node == nil {
		return nil, "", fmt.Errorf("node disappeared")
	}
	object, err := typedManifestObject(details.Node, "v1", "Node")
	if err != nil {
		return nil, "", err
	}
	return e.editManifest(details.Row.Cluster, fmt.Sprintf("edit node/%s manifest", details.Row.Name), object)
}

func (e *Executor) DeleteNode(details state.NodeDetails) (*exec.Cmd, string, error) {
	if details.Node == nil {
		return nil, "", fmt.Errorf("node disappeared")
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	args = append(args, "delete", "node/"+details.Row.Name)
	return exec.Command("kubectl", args...), fmt.Sprintf("delete node/%s", details.Row.Name), nil
}

func (e *Executor) EditGenericResource(resource cluster.ResourceKind, details cluster.GenericResourceDetails) (*exec.Cmd, string, error) {
	if resource.Resource == "" {
		return nil, "", fmt.Errorf("resource kind disappeared")
	}
	if details.Row.Name == "" {
		return nil, "", fmt.Errorf("resource row disappeared")
	}
	if details.Object == nil {
		return nil, "", fmt.Errorf("resource disappeared")
	}
	object := details.Object.DeepCopy()
	if object.GetAPIVersion() == "" {
		if resource.APIGroup == "" {
			object.SetAPIVersion(resource.Version)
		} else {
			object.SetAPIVersion(resource.APIGroup + "/" + resource.Version)
		}
	}
	if object.GetKind() == "" && resource.Kind != "" {
		object.SetKind(resource.Kind)
	}
	return e.editManifest(details.Row.Cluster, fmt.Sprintf("edit %s/%s manifest", resource.Resource, details.Row.Name), object)
}

func (e *Executor) DeleteGenericResource(resource cluster.ResourceKind, details cluster.GenericResourceDetails) (*exec.Cmd, string, error) {
	if resource.Resource == "" {
		return nil, "", fmt.Errorf("resource kind disappeared")
	}
	if details.Row.Name == "" {
		return nil, "", fmt.Errorf("resource row disappeared")
	}
	if details.Object == nil {
		return nil, "", fmt.Errorf("resource disappeared")
	}
	args, err := e.baseArgs(details.Row.Cluster)
	if err != nil {
		return nil, "", err
	}
	qualifiedResource := qualifiedResourceName(resource)
	if resource.Namespaced {
		args = append(args, "delete", "-n", details.Row.Namespace, qualifiedResource+"/"+details.Row.Name)
	} else {
		args = append(args, "delete", qualifiedResource+"/"+details.Row.Name)
	}
	return exec.Command("kubectl", args...), fmt.Sprintf("delete %s/%s", resource.Resource, details.Row.Name), nil
}

func (e *Executor) editManifest(contextName string, description string, object *unstructured.Unstructured) (*exec.Cmd, string, error) {
	if object == nil {
		return nil, "", fmt.Errorf("resource disappeared")
	}
	args, err := e.baseArgs(contextName)
	if err != nil {
		return nil, "", err
	}
	manifest := scrubManifestObject(object)
	content, err := yaml.Marshal(manifest.Object)
	if err != nil {
		return nil, "", fmt.Errorf("marshal manifest: %w", err)
	}
	file, err := os.CreateTemp("", "surfsk8s-edit-*.yaml")
	if err != nil {
		return nil, "", fmt.Errorf("create temp manifest: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, "", fmt.Errorf("write temp manifest: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(file.Name())
		return nil, "", fmt.Errorf("close temp manifest: %w", err)
	}
	argv := append([]string{"-c", manifestEditScript, "sh", file.Name()}, args...)
	return exec.Command("sh", argv...), description, nil
}

func (e *Executor) baseArgs(contextName string) ([]string, error) {
	contextName = strings.TrimSpace(contextName)
	if contextName == "" {
		return nil, fmt.Errorf("missing cluster context")
	}
	args := make([]string, 0, 4)
	if e.kubeconfigPath != "" {
		args = append(args, "--kubeconfig", e.kubeconfigPath)
	}
	args = append(args, "--context", contextName)
	return args, nil
}

func typedManifestObject(object interface{}, apiVersion string, kind string) (*unstructured.Unstructured, error) {
	mapped, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
	if err != nil {
		return nil, fmt.Errorf("convert manifest: %w", err)
	}
	manifest := &unstructured.Unstructured{Object: mapped}
	manifest.SetAPIVersion(apiVersion)
	manifest.SetKind(kind)
	return manifest, nil
}

func scrubManifestObject(object *unstructured.Unstructured) *unstructured.Unstructured {
	if object == nil {
		panic("actions.scrubManifestObject: nil object")
	}
	manifest := object.DeepCopy()
	delete(manifest.Object, "status")
	metadata, ok := manifest.Object["metadata"].(map[string]interface{})
	if !ok {
		return manifest
	}
	for _, key := range []string{"creationTimestamp", "deletionGracePeriodSeconds", "deletionTimestamp", "generation", "managedFields", "resourceVersion", "selfLink", "uid"} {
		delete(metadata, key)
	}
	annotations, ok := metadata["annotations"].(map[string]interface{})
	if ok {
		delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
		if len(annotations) == 0 {
			delete(metadata, "annotations")
		}
	}
	return manifest
}

func PodContainerNames(pod *corev1.Pod) ([]string, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod disappeared")
	}
	if len(pod.Spec.Containers) == 0 {
		return nil, fmt.Errorf("pod has no containers")
	}
	names := make([]string, 0, len(pod.Spec.Containers))
	for _, container := range pod.Spec.Containers {
		names = append(names, container.Name)
	}
	return names, nil
}

func PodPortChoices(pod *corev1.Pod) ([]PortChoice, error) {
	if pod == nil {
		return nil, fmt.Errorf("pod disappeared")
	}
	choices := make([]PortChoice, 0, 8)
	for _, container := range pod.Spec.Containers {
		for _, port := range container.Ports {
			if port.ContainerPort <= 0 {
				continue
			}
			choices = append(choices, PortChoice{
				Port:  int(port.ContainerPort),
				Label: fmt.Sprintf("%d/%s  (%s)", port.ContainerPort, defaultProtocol(port.Protocol), container.Name),
			})
		}
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("pod has no declared container ports")
	}
	return choices, nil
}

func ServicePortChoices(service *corev1.Service) ([]PortChoice, error) {
	if service == nil {
		return nil, fmt.Errorf("service disappeared")
	}
	choices := make([]PortChoice, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		if port.Port <= 0 {
			continue
		}
		label := fmt.Sprintf("%d/%s", port.Port, defaultProtocol(port.Protocol))
		if port.Name != "" {
			label = label + "  (" + port.Name + ")"
		}
		choices = append(choices, PortChoice{Port: int(port.Port), Label: label})
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("service has no declared ports")
	}
	return choices, nil
}

func defaultContainerName(pod *corev1.Pod) (string, error) {
	names, err := PodContainerNames(pod)
	if err != nil {
		return "", err
	}
	if preferred := pod.Annotations[defaultContainerAnnotation]; preferred != "" {
		for _, name := range names {
			if name == preferred {
				return preferred, nil
			}
		}
	}
	return names[0], nil
}

func firstPodPort(pod *corev1.Pod) (int, error) {
	choices, err := PodPortChoices(pod)
	if err != nil {
		return 0, err
	}
	return choices[0].Port, nil
}

func firstServicePort(service *corev1.Service) (int, error) {
	choices, err := ServicePortChoices(service)
	if err != nil {
		return 0, err
	}
	return choices[0].Port, nil
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

func podHasContainer(pod *corev1.Pod, containerName string) bool {
	if pod == nil {
		return false
	}
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName {
			return true
		}
	}
	return false
}

func validatePort(port int, field string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", field)
	}
	return nil
}

func portMapping(localPort int, remotePort int) string {
	return strconv.Itoa(localPort) + ":" + strconv.Itoa(remotePort)
}

func qualifiedResourceName(resource cluster.ResourceKind) string {
	if resource.Resource == "" {
		panic("actions.qualifiedResourceName: empty resource")
	}
	if resource.APIGroup == "" {
		return resource.Resource
	}
	return resource.Resource + "." + resource.APIGroup
}

func defaultProtocol(protocol corev1.Protocol) string {
	if protocol == "" {
		return string(corev1.ProtocolTCP)
	}
	return string(protocol)
}
