package managedkubernetes

import (
	"errors"
	"testing"
)

// TestAllNamespacesIsAnExplicitCollectionScope excludes implicit cluster or named-resource expansion.
// TestAllNamespacesIsAnExplicitCollectionScope исключает неявное расширение cluster либо named-resource scope.
func TestAllNamespacesIsAnExplicitCollectionScope(t *testing.T) {
	input := ResourceAddress{Group: "apps", Version: "v1", Resource: "deployments", Namespaced: true, AllNamespaces: true}
	if input.Validate() != nil {
		t.Fatal("explicit all-namespace collection was rejected")
	}
	for _, change := range []func(*ResourceAddress){
		func(item *ResourceAddress) { item.AllNamespaces = false },
		func(item *ResourceAddress) { item.Namespaced = false },
		func(item *ResourceAddress) { item.Namespace = "selected" },
		func(item *ResourceAddress) { item.Name = "selected" },
		func(item *ResourceAddress) { item.Subresource = "status" },
		func(item *ResourceAddress) { item.ResourceVersion = "42" },
	} {
		changed := input
		change(&changed)
		if changed.Validate() == nil {
			t.Fatal("ambiguous all-namespace scope became an admitted resource address")
		}
	}
}

// TestResourceAddressSharesClosedLocatorRules prevents owner/broker disagreement about raw routes,
// scope ambiguity, encoded traversal and immutable UID/RV precondition shape.
// TestResourceAddressSharesClosedLocatorRules исключает owner/broker disagreement о raw routes,
// scope ambiguity, encoded traversal и форме immutable UID/RV preconditions.
func TestResourceAddressSharesClosedLocatorRules(t *testing.T) {
	base := ResourceAddress{Group: "apps", Version: "v1", Resource: "deployments", Namespaced: true,
		Namespace: "selected", Name: "selected", UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37"}
	if base.Validate() != nil {
		t.Fatal("canonical owner resource locator was rejected")
	}
	for _, mutate := range []func(*ResourceAddress){
		func(value *ResourceAddress) { value.Group = "*" },
		func(value *ResourceAddress) { value.Version = "v1/../../foreign" },
		func(value *ResourceAddress) { value.Resource = "pods/exec" },
		func(value *ResourceAddress) { value.Namespace = "" },
		func(value *ResourceAddress) { value.Namespaced = false },
		func(value *ResourceAddress) { value.Name = ".." },
		func(value *ResourceAddress) { value.Name = "selected%2fproxy" },
		func(value *ResourceAddress) { value.Name = "selected\\foreign" },
		func(value *ResourceAddress) { value.Name = "selected/foreign" },
		func(value *ResourceAddress) { value.Name = "selected\nforeign" },
		func(value *ResourceAddress) { value.Subresource = "proxy" },
		func(value *ResourceAddress) { value.Subresource = "exec" },
		func(value *ResourceAddress) { value.Subresource = "token" },
		func(value *ResourceAddress) { value.UID = "9A99D4F2-836D-4EFB-868E-5DE8792ADE07" },
		func(value *ResourceAddress) { value.Name = "" },
		func(value *ResourceAddress) { value.ResourceVersion = "foreign\nversion" },
		func(value *ResourceAddress) { value.ResourceVersion = string([]byte{0xff}) },
	} {
		changed := base
		mutate(&changed)
		if !errors.Is(changed.Validate(), ErrInvalidResourceAddress) {
			t.Fatal("unsafe resource locator escaped shared validation")
		}
	}
	role := ResourceAddress{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles", Namespace: "selected", Namespaced: true, Name: "system:команда?scope#one"}
	if role.Validate() != nil {
		t.Fatal("safe one-segment Unicode RBAC name was rejected")
	}
}
