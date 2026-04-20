package cluster

import (
	"sort"
	"strings"
)

type PrinterColumn struct {
	Name     string
	JSONPath string
	Type     string
	Priority int32
}

type ResourceKind struct {
	ID             string
	GroupName      string
	Display        string
	Kind           string
	APIGroup       string
	Version        string
	Resource       string
	Namespaced     bool
	Favorite       bool
	Custom         bool
	PrinterColumns []PrinterColumn
}

type ResourceGroup struct {
	Name      string
	Resources []ResourceKind
}

type discoveredResource struct {
	APIGroup       string
	Version        string
	Resource       string
	Kind           string
	Namespaced     bool
	PrinterColumns []PrinterColumn
}

type resourceTemplate struct {
	GroupName  string
	Display    string
	Kind       string
	APIGroup   string
	Resource   string
	Namespaced bool
	Favorite   bool
}

var knownResourceTemplates = []resourceTemplate{
	{GroupName: "Favourites", Display: "Pods", Kind: "Pod", APIGroup: "", Resource: "pods", Namespaced: true, Favorite: true},
	{GroupName: "Favourites", Display: "Deployments", Kind: "Deployment", APIGroup: "apps", Resource: "deployments", Namespaced: true, Favorite: true},
	{GroupName: "Favourites", Display: "Services", Kind: "Service", APIGroup: "", Resource: "services", Namespaced: true, Favorite: true},
	{GroupName: "Favourites", Display: "Nodes", Kind: "Node", APIGroup: "", Resource: "nodes", Namespaced: false, Favorite: true},

	{GroupName: "Workloads", Display: "Pods", Kind: "Pod", APIGroup: "", Resource: "pods", Namespaced: true},
	{GroupName: "Workloads", Display: "Deployments", Kind: "Deployment", APIGroup: "apps", Resource: "deployments", Namespaced: true},
	{GroupName: "Workloads", Display: "StatefulSets", Kind: "StatefulSet", APIGroup: "apps", Resource: "statefulsets", Namespaced: true},
	{GroupName: "Workloads", Display: "DaemonSets", Kind: "DaemonSet", APIGroup: "apps", Resource: "daemonsets", Namespaced: true},
	{GroupName: "Workloads", Display: "ReplicaSets", Kind: "ReplicaSet", APIGroup: "apps", Resource: "replicasets", Namespaced: true},
	{GroupName: "Workloads", Display: "Jobs", Kind: "Job", APIGroup: "batch", Resource: "jobs", Namespaced: true},
	{GroupName: "Workloads", Display: "CronJobs", Kind: "CronJob", APIGroup: "batch", Resource: "cronjobs", Namespaced: true},

	{GroupName: "Network", Display: "Services", Kind: "Service", APIGroup: "", Resource: "services", Namespaced: true},
	{GroupName: "Network", Display: "Ingresses", Kind: "Ingress", APIGroup: "networking.k8s.io", Resource: "ingresses", Namespaced: true},
	{GroupName: "Network", Display: "Endpoints", Kind: "Endpoints", APIGroup: "", Resource: "endpoints", Namespaced: true},
	{GroupName: "Network", Display: "NetworkPolicies", Kind: "NetworkPolicy", APIGroup: "networking.k8s.io", Resource: "networkpolicies", Namespaced: true},

	{GroupName: "Config", Display: "ConfigMaps", Kind: "ConfigMap", APIGroup: "", Resource: "configmaps", Namespaced: true},
	{GroupName: "Config", Display: "Secrets", Kind: "Secret", APIGroup: "", Resource: "secrets", Namespaced: true},
	{GroupName: "Config", Display: "ServiceAccounts", Kind: "ServiceAccount", APIGroup: "", Resource: "serviceaccounts", Namespaced: true},

	{GroupName: "Storage", Display: "PersistentVolumeClaims", Kind: "PersistentVolumeClaim", APIGroup: "", Resource: "persistentvolumeclaims", Namespaced: true},
	{GroupName: "Storage", Display: "PersistentVolumes", Kind: "PersistentVolume", APIGroup: "", Resource: "persistentvolumes", Namespaced: false},
	{GroupName: "Storage", Display: "StorageClasses", Kind: "StorageClass", APIGroup: "storage.k8s.io", Resource: "storageclasses", Namespaced: false},

	{GroupName: "Access", Display: "Roles", Kind: "Role", APIGroup: "rbac.authorization.k8s.io", Resource: "roles", Namespaced: true},
	{GroupName: "Access", Display: "RoleBindings", Kind: "RoleBinding", APIGroup: "rbac.authorization.k8s.io", Resource: "rolebindings", Namespaced: true},
	{GroupName: "Access", Display: "ClusterRoles", Kind: "ClusterRole", APIGroup: "rbac.authorization.k8s.io", Resource: "clusterroles", Namespaced: false},
	{GroupName: "Access", Display: "ClusterRoleBindings", Kind: "ClusterRoleBinding", APIGroup: "rbac.authorization.k8s.io", Resource: "clusterrolebindings", Namespaced: false},

	{GroupName: "Policy", Display: "HorizontalPodAutoscalers", Kind: "HorizontalPodAutoscaler", APIGroup: "autoscaling", Resource: "horizontalpodautoscalers", Namespaced: true},
	{GroupName: "Policy", Display: "PodDisruptionBudgets", Kind: "PodDisruptionBudget", APIGroup: "policy", Resource: "poddisruptionbudgets", Namespaced: true},

	{GroupName: "Cluster", Display: "Namespaces", Kind: "Namespace", APIGroup: "", Resource: "namespaces", Namespaced: false},
	{GroupName: "Cluster", Display: "Nodes", Kind: "Node", APIGroup: "", Resource: "nodes", Namespaced: false},
	{GroupName: "Cluster", Display: "Events", Kind: "Event", APIGroup: "", Resource: "events", Namespaced: true},
}

func buildCatalog(resources []discoveredResource) []ResourceGroup {
	groupOrder := []string{"Favourites", "Workloads", "Network", "Config", "Storage", "Access", "Policy", "Cluster", "CRDs"}
	groups := make(map[string][]ResourceKind, len(groupOrder))
	knownByKey := make(map[string]resourceTemplate, len(knownResourceTemplates))
	discoveredByKey := make(map[string]discoveredResource, len(resources))
	seen := make(map[string]struct{}, len(knownResourceTemplates))

	for _, resource := range resources {
		key := resourceKey(resource.APIGroup, resource.Resource)
		if _, ok := discoveredByKey[key]; ok {
			continue
		}
		discoveredByKey[key] = resource
	}

	for _, template := range knownResourceTemplates {
		key := resourceKey(template.APIGroup, template.Resource)
		if _, ok := seen[template.GroupName+"|"+key]; ok {
			continue
		}
		seen[template.GroupName+"|"+key] = struct{}{}
		knownByKey[key] = template
		knownResource := ResourceKind{
			ID:         key,
			GroupName:  template.GroupName,
			Display:    template.Display,
			Kind:       template.Kind,
			APIGroup:   template.APIGroup,
			Resource:   template.Resource,
			Namespaced: template.Namespaced,
			Favorite:   template.Favorite,
		}
		if discovered, ok := discoveredByKey[key]; ok {
			knownResource.Version = discovered.Version
			if discovered.Kind != "" {
				knownResource.Kind = discovered.Kind
			}
			knownResource.PrinterColumns = append([]PrinterColumn(nil), discovered.PrinterColumns...)
		}
		groups[template.GroupName] = append(groups[template.GroupName], knownResource)
	}

	crdResources := make([]ResourceKind, 0, 16)
	crdSeen := make(map[string]struct{}, 32)
	for _, resource := range resources {
		key := resourceKey(resource.APIGroup, resource.Resource)
		if _, ok := knownByKey[key]; ok {
			continue
		}
		if _, ok := crdSeen[key]; ok {
			continue
		}
		crdSeen[key] = struct{}{}
		crdResources = append(crdResources, ResourceKind{
			ID:             key,
			GroupName:      "CRDs",
			Display:        resource.Resource,
			Kind:           resource.Kind,
			APIGroup:       resource.APIGroup,
			Version:        resource.Version,
			Resource:       resource.Resource,
			Namespaced:     resource.Namespaced,
			Custom:         true,
			PrinterColumns: append([]PrinterColumn(nil), resource.PrinterColumns...),
		})
	}

	sort.Slice(crdResources, func(i int, j int) bool {
		left := crdResources[i]
		right := crdResources[j]
		if left.APIGroup != right.APIGroup {
			return left.APIGroup < right.APIGroup
		}
		return left.Resource < right.Resource
	})
	if len(crdResources) != 0 {
		groups["CRDs"] = crdResources
	}

	catalog := make([]ResourceGroup, 0, len(groupOrder))
	for _, groupName := range groupOrder {
		resourcesForGroup := groups[groupName]
		if len(resourcesForGroup) == 0 {
			continue
		}
		sort.Slice(resourcesForGroup, func(i int, j int) bool {
			return strings.ToLower(resourcesForGroup[i].Display) < strings.ToLower(resourcesForGroup[j].Display)
		})
		if groupName == "Favourites" {
			sort.SliceStable(resourcesForGroup, func(i int, j int) bool {
				return favoriteRank(resourcesForGroup[i].Resource) < favoriteRank(resourcesForGroup[j].Resource)
			})
		}
		catalog = append(catalog, ResourceGroup{Name: groupName, Resources: resourcesForGroup})
	}
	return catalog
}

func resourceKey(apiGroup string, resource string) string {
	return apiGroup + "/" + resource
}

func favoriteRank(resource string) int {
	switch resource {
	case "pods":
		return 0
	case "deployments":
		return 1
	case "services":
		return 2
	case "nodes":
		return 3
	default:
		return 100
	}
}
