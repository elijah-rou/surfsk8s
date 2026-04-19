package cluster

// Manager holds concurrent connections to multiple k8s clusters.
// Each cluster gets its own clientset and informer factory.
type Manager struct{}
