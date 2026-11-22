package kubernetestarget

import (
	"context"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"strings"
	"testing"
)

// TestReadRequestKeepsOwnerResolvedLocationsOnPinnedOrigin checks actual wire locations and privacy.
// It exercises core, group, collection, subresource and colon-containing RBAC names without network calls.
// TestReadRequestKeepsOwnerResolvedLocationsOnPinnedOrigin проверяет wire locations и privacy.
// Покрывает core/group/collection/subresource и RBAC names с colon без сетевых вызовов.
func TestReadRequestKeepsOwnerResolvedLocationsOnPinnedOrigin(t *testing.T) {
	credential, _ := transportFixture(t, false)
	for _, item := range []struct {
		address ResourceAddress
		path    string
	}{
		{ResourceAddress{Version: "v1", Resource: "pods", Namespace: "app", Name: "worker", Namespaced: true}, "/api/v1/namespaces/app/pods/worker"},
		{ResourceAddress{Group: "apps", Version: "v1", Resource: "deployments", Namespace: "app", Name: "worker", Subresource: "scale", Namespaced: true}, "/apis/apps/v1/namespaces/app/deployments/worker/scale"},
		{ResourceAddress{Group: "example.invalid", Version: "v1alpha1", Resource: "widgets", Namespace: "app", Namespaced: true}, "/apis/example.invalid/v1alpha1/namespaces/app/widgets"},
		{ResourceAddress{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles", Name: "system:discovery"}, "/apis/rbac.authorization.k8s.io/v1/clusterroles/system:discovery"},
		{ResourceAddress{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles", Namespace: "app", Name: "команда?scope#team", Namespaced: true}, "/apis/rbac.authorization.k8s.io/v1/namespaces/app/roles/команда?scope#team"},
	} {
		request, err := credential.ReadRequest(context.Background(), item.address)
		if err != nil || request.URL.Path != item.path || request.URL.Scheme != "https" || request.URL.Host != credential.target.ServerURL[len("https://"):] ||
			request.Header.Get("Authorization") != "" || request.URL.RawQuery != "" || request.URL.Fragment != "" || request.Method != "GET" {
			t.Fatal("resource address escaped its registered exact-origin read path")
		}
	}
}

// TestResourceAddressRejectsRawAndAmbiguousLocations ensures actor input cannot select unsafe routes.
// Validation only checks location shape; successful validation is never a permission decision.
// TestResourceAddressRejectsRawAndAmbiguousLocations запрещает unsafe routes из actor input.
// Проверка касается только location shape; успешная проверка не выдаёт полномочий.
func TestResourceAddressRejectsRawAndAmbiguousLocations(t *testing.T) {
	base := ResourceAddress{Version: "v1", Resource: "pods", Namespace: "app", Name: "worker", Namespaced: true}
	changes := []func(*ResourceAddress){
		func(value *ResourceAddress) { value.Group = "../foreign" },
		func(value *ResourceAddress) { value.Version = "v1/../../secrets" },
		func(value *ResourceAddress) { value.Resource = "pods/exec" },
		func(value *ResourceAddress) { value.Namespace = "" },
		func(value *ResourceAddress) { value.Namespace = "../foreign" },
		func(value *ResourceAddress) { value.Namespaced = false },
		func(value *ResourceAddress) { value.Name = ".." },
		func(value *ResourceAddress) { value.Name = "worker%2fexec" },
		func(value *ResourceAddress) { value.Name = "worker/exec" },
		func(value *ResourceAddress) { value.Name = "worker\\foreign" },
		func(value *ResourceAddress) { value.Name = "worker\x00" },
		func(value *ResourceAddress) { value.Subresource = "proxy" },
		func(value *ResourceAddress) { value.Subresource = "exec" },
		func(value *ResourceAddress) { value.Subresource = "attach" },
		func(value *ResourceAddress) { value.Subresource = "portforward" },
		func(value *ResourceAddress) { value.Subresource = "token" },
		func(value *ResourceAddress) { value.UID = "not-a-resource-uid" },
		func(value *ResourceAddress) { value.ResourceVersion = "unsafe\nversion" },
	}
	for _, change := range changes {
		value := base
		change(&value)
		if value.Validate() == nil {
			t.Fatal("unsafe or ambiguous resource location was accepted")
		}
	}
}

// TestDiscoveryRequestKeepsPinnedOrigin verifies real HTTP request construction for a typed
// non-resource schema read without executing a server-returned URL or adding caller credentials.
// TestDiscoveryRequestKeepsPinnedOrigin проверяет HTTP request construction для typed
// non-resource schema read без исполнения server-returned URL либо caller credentials.
func TestDiscoveryRequestKeepsPinnedOrigin(t *testing.T) {
	credential, _ := transportFixture(t, false)
	address := managedkubernetes.DiscoveryAddress{Kind: managedkubernetes.DiscoveryOpenAPISchema, Group: "apps", Version: "v1", ImmutableSchemaHash: strings.Repeat("a", 128)}
	request, err := credential.DiscoveryRequest(context.Background(), address)
	if err != nil || request.URL.Path != "/openapi/v3/apis/apps/v1" || request.URL.Query().Get("hash") != address.ImmutableSchemaHash || len(request.URL.Query()) != 1 || request.URL.Host != credential.target.ServerURL[len("https://"):] || request.URL.Scheme != "https" || request.Header.Get("Authorization") != "" || request.Method != "GET" || request.Body != nil {
		t.Fatal("typed discovery request escaped pinned read-only location")
	}
	address.Group = "apps/../../secrets"
	if _, err := credential.DiscoveryRequest(context.Background(), address); err == nil {
		t.Fatal("arbitrary discovery path accepted")
	}
}
