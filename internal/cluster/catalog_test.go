package cluster

import "testing"

func TestBuildCatalogKeepsFavouritesTopAndCRDsBottom(t *testing.T) {
	catalog := buildCatalog([]discoveredResource{
		{APIGroup: "acme.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true},
		{APIGroup: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true},
	})

	if got, want := catalog[0].Name, "Favourites"; got != want {
		t.Fatalf("first group = %q, want %q", got, want)
	}
	if got, want := catalog[len(catalog)-1].Name, "CRDs"; got != want {
		t.Fatalf("last group = %q, want %q", got, want)
	}

	crds := catalog[len(catalog)-1]
	if got, want := len(crds.Resources), 1; got != want {
		t.Fatalf("crd count = %d, want %d", got, want)
	}
	if got, want := crds.Resources[0].Resource, "widgets"; got != want {
		t.Fatalf("crd resource = %q, want %q", got, want)
	}
}

func TestBuildCatalogDoesNotDuplicateKnownResourcesIntoCRDs(t *testing.T) {
	catalog := buildCatalog([]discoveredResource{
		{APIGroup: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true},
		{APIGroup: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true},
	})

	for _, group := range catalog {
		if group.Name != "CRDs" {
			continue
		}
		for _, resource := range group.Resources {
			if resource.Resource == "pods" || resource.Resource == "deployments" {
				t.Fatalf("known resource %q leaked into CRDs", resource.Resource)
			}
		}
	}
}
