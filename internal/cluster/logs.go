package cluster

import (
	"context"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/elijahrou/surfsk8s/internal/state"
)

// NodeLogEntry is a selectable file or directory from kubelet's /logs proxy.
type NodeLogEntry struct {
	Path      string
	Name      string
	Directory bool
}

type PodLogsOptions struct {
	Container  string
	Timestamps bool
	TailLines  *int64
	SinceTime  *time.Time
	LimitBytes *int64
}

type NodeLogOptions struct {
	Path      string
	TailBytes int64
}

func (m *Manager) PodLogs(ctx context.Context, details state.PodDetails, container string, tailLines int64, timestamps bool) (string, error) {
	return m.PodLogsWithOptions(ctx, details, PodLogsOptions{Container: container, Timestamps: timestamps, TailLines: &tailLines})
}

func (m *Manager) PodLogsWithOptions(ctx context.Context, details state.PodDetails, options PodLogsOptions) (string, error) {
	if ctx == nil {
		panic("cluster.Manager.PodLogsWithOptions: nil context")
	}
	if details.Pod == nil {
		return "", fmt.Errorf("pod disappeared")
	}
	if details.Row.Cluster == "" {
		return "", fmt.Errorf("missing cluster context")
	}
	if options.Container == "" {
		return "", fmt.Errorf("missing container")
	}
	if options.TailLines != nil && *options.TailLines <= 0 {
		return "", fmt.Errorf("tail lines must be > 0")
	}
	if options.LimitBytes != nil && *options.LimitBytes <= 0 {
		return "", fmt.Errorf("limit bytes must be > 0")
	}

	m.mu.RLock()
	conn, ok := m.conns[details.Row.Cluster]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("cluster %q not connected", details.Row.Cluster)
	}
	if conn.Clientset == nil {
		return "", fmt.Errorf("clientset unavailable")
	}

	logOptions := &corev1.PodLogOptions{
		Container:  options.Container,
		Timestamps: options.Timestamps,
		TailLines:  options.TailLines,
		LimitBytes: options.LimitBytes,
	}
	if options.SinceTime != nil {
		t := metav1.NewTime(options.SinceTime.UTC())
		logOptions.SinceTime = &t
	}

	stream, err := conn.Clientset.CoreV1().Pods(details.Row.Namespace).GetLogs(details.Row.Name, logOptions).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	return readLogStream(stream, options.LimitBytes)
}

// readLogStream consumes a log body with an optional hard client-side byte cap.
// When limitBytes is set, the reader is capped at limit+1 and oversized bodies are rejected
// even if the server ignored LimitBytes.
func readLogStream(stream io.Reader, limitBytes *int64) (string, error) {
	if stream == nil {
		panic("cluster.readLogStream: nil stream")
	}
	reader := stream
	limit := int64(-1)
	if limitBytes != nil {
		if *limitBytes <= 0 {
			panic("cluster.readLogStream: non-positive limitBytes")
		}
		limit = *limitBytes
		reader = io.LimitReader(stream, limit+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if limit >= 0 && int64(len(data)) > limit {
		return "", fmt.Errorf("pod log exceeded %d byte limit", limit)
	}
	return string(data), nil
}

func (m *Manager) DeploymentPodDetails(details state.DeploymentDetails, now time.Time) ([]state.PodDetails, error) {
	if details.Deployment == nil {
		return nil, fmt.Errorf("deployment disappeared")
	}
	selector, err := metav1.LabelSelectorAsSelector(details.Deployment.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("deployment selector: %w", err)
	}
	if selector.Empty() {
		return nil, fmt.Errorf("deployment selector empty")
	}

	pods := make([]state.PodDetails, 0, 8)
	m.store.ForEachPod(func(row state.PodRow) bool {
		if row.Cluster != details.Row.Cluster || row.Namespace != details.Row.Namespace {
			return true
		}
		pod, ok := m.store.PodObjectByKey(row.Key)
		if !ok || pod == nil {
			return true
		}
		if !selector.Matches(labels.Set(pod.Labels)) {
			return true
		}
		podDetails, ok := m.store.PodDetailsByKey(row.Key, now)
		if !ok {
			return true
		}
		pods = append(pods, podDetails)
		return true
	})
	if len(pods) == 0 {
		return nil, fmt.Errorf("deployment has no matching pods")
	}
	sort.Slice(pods, func(i int, j int) bool {
		if pods[i].Row.Name != pods[j].Row.Name {
			return pods[i].Row.Name < pods[j].Row.Name
		}
		return pods[i].Row.Cluster < pods[j].Row.Cluster
	})
	return pods, nil
}

func (m *Manager) DeploymentLogs(ctx context.Context, details state.DeploymentDetails, container string, tailLines int64, timestamps bool, now time.Time) (string, error) {
	return m.DeploymentLogsWithOptions(ctx, details, PodLogsOptions{Container: container, Timestamps: timestamps, TailLines: &tailLines}, now)
}

func (m *Manager) DeploymentLogsWithOptions(ctx context.Context, details state.DeploymentDetails, options PodLogsOptions, now time.Time) (string, error) {
	if ctx == nil {
		panic("cluster.Manager.DeploymentLogsWithOptions: nil context")
	}
	if options.Container == "" {
		return "", fmt.Errorf("missing container")
	}
	pods, err := m.DeploymentPodDetails(details, now)
	if err != nil {
		return "", err
	}

	var body strings.Builder
	errors := make([]string, 0, len(pods))
	for _, pod := range pods {
		content, err := m.PodLogsWithOptions(ctx, pod, options)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", pod.Row.Name, err))
			continue
		}
		trimmed := strings.TrimRight(content, "\n")
		if body.Len() != 0 {
			body.WriteString("\n\n")
		}
		if len(pods) > 1 {
			body.WriteString("== pod/")
			body.WriteString(pod.Row.Name)
			body.WriteString(" ==\n")
		}
		body.WriteString(trimmed)
	}

	if body.Len() == 0 && len(errors) != 0 {
		return "", fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	if len(errors) != 0 {
		body.WriteString("\n\nErrors:\n")
		for _, item := range errors {
			body.WriteString("- ")
			body.WriteString(item)
			body.WriteByte('\n')
		}
	}
	return body.String(), nil
}

func (m *Manager) NodeLogEntries(ctx context.Context, details state.NodeDetails, dir string) ([]NodeLogEntry, error) {
	if ctx == nil {
		panic("cluster.Manager.NodeLogEntries: nil context")
	}
	if details.Row.Cluster == "" {
		return nil, fmt.Errorf("missing cluster context")
	}
	if details.Node == nil {
		return nil, fmt.Errorf("node disappeared")
	}
	cleanDir, err := cleanNodeLogDir(dir)
	if err != nil {
		return nil, err
	}

	conn, err := m.logConn(details.Row.Cluster)
	if err != nil {
		return nil, err
	}
	data, err := conn.REST.Get().AbsPath(buildNodeLogProxyPath(details.Row.Name, cleanDir)).DoRaw(ctx)
	if err != nil {
		return nil, err
	}
	return parseNodeLogEntries(cleanDir, data)
}

func (m *Manager) NodeLog(ctx context.Context, details state.NodeDetails, logPath string, tailBytes int64) (string, error) {
	return m.NodeLogWithOptions(ctx, details, NodeLogOptions{Path: logPath, TailBytes: tailBytes})
}

func (m *Manager) NodeLogWithOptions(ctx context.Context, details state.NodeDetails, options NodeLogOptions) (string, error) {
	if ctx == nil {
		panic("cluster.Manager.NodeLogWithOptions: nil context")
	}
	if details.Row.Cluster == "" {
		return "", fmt.Errorf("missing cluster context")
	}
	if details.Node == nil {
		return "", fmt.Errorf("node disappeared")
	}
	cleanPath, err := cleanNodeLogPath(options.Path)
	if err != nil {
		return "", err
	}
	if options.TailBytes <= 0 {
		return "", fmt.Errorf("tail bytes must be > 0")
	}

	conn, err := m.logConn(details.Row.Cluster)
	if err != nil {
		return "", err
	}
	request := conn.REST.Get().AbsPath(buildNodeLogProxyPath(details.Row.Name, cleanPath))
	request = request.SetHeader("Range", fmt.Sprintf("bytes=-%d", options.TailBytes))
	stream, err := request.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	limited := io.LimitReader(stream, options.TailBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > options.TailBytes {
		return "", fmt.Errorf("node log exceeded %d byte limit", options.TailBytes)
	}
	return string(data), nil
}

func (m *Manager) logConn(clusterName string) (*ClusterConn, error) {
	m.mu.RLock()
	conn, ok := m.conns[clusterName]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cluster %q not connected", clusterName)
	}
	if conn.REST == nil {
		return nil, fmt.Errorf("rest client unavailable")
	}
	return conn, nil
}

func buildNodeLogProxyPath(nodeName string, logPath string) string {
	if nodeName == "" {
		panic("cluster.buildNodeLogProxyPath: empty nodeName")
	}
	base := "/api/v1/nodes/" + nodeName + "/proxy/logs"
	if logPath == "" {
		return base + "/"
	}
	if strings.HasSuffix(logPath, "/") {
		return base + "/" + logPath
	}
	return base + "/" + logPath
}

func cleanNodeLogDir(dir string) (string, error) {
	dir = strings.TrimSpace(strings.TrimPrefix(dir, "/"))
	if dir == "" {
		return "", nil
	}
	clean := path.Clean(dir)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid node log path %q", dir)
	}
	return clean + "/", nil
}

func cleanNodeLogPath(logPath string) (string, error) {
	logPath = strings.TrimSpace(strings.TrimPrefix(logPath, "/"))
	clean := path.Clean(logPath)
	if clean == "." || clean == "" || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("invalid node log path %q", logPath)
	}
	return clean, nil
}

var nodeLogLinkRE = regexp.MustCompile(`<a href="([^"]+)">([^<]+)</a>`)

func parseNodeLogEntries(dir string, data []byte) ([]NodeLogEntry, error) {
	matches := nodeLogLinkRE.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("node log directory empty")
	}
	entries := make([]NodeLogEntry, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		href := strings.TrimSpace(match[1])
		name := strings.TrimSpace(match[2])
		if href == "" || href == "../" {
			continue
		}
		directory := strings.HasSuffix(href, "/")
		segment := strings.TrimSuffix(href, "/")
		if segment == "" {
			continue
		}
		fullPath := path.Join(strings.TrimSuffix(dir, "/"), segment)
		if directory {
			fullPath += "/"
			name = strings.TrimSuffix(name, "/") + "/"
		}
		entries = append(entries, NodeLogEntry{Path: fullPath, Name: name, Directory: directory})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("node log directory empty")
	}
	sort.Slice(entries, func(i int, j int) bool {
		left := nodeLogPriority(entries[i])
		right := nodeLogPriority(entries[j])
		if left != right {
			return left < right
		}
		if entries[i].Directory != entries[j].Directory {
			return !entries[i].Directory
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func nodeLogPriority(entry NodeLogEntry) int {
	switch entry.Path {
	case "cloud-init.log":
		return 0
	case "cloud-init-output.log":
		return 1
	case "containers/":
		return 2
	case "journal/":
		return 3
	case "README":
		return 50
	default:
		return 20
	}
}
