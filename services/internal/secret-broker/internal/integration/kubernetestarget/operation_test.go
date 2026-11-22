package kubernetestarget

import "testing"

// TestAllNamespacesRequiresDedicatedReadOperation excludes ordinary list and every mutation.
// Its RBAC tuple remains explicitly namespaced with an empty namespace for the global review.
// TestAllNamespacesRequiresDedicatedReadOperation исключает ordinary list и все mutations.
// RBAC tuple остаётся явно namespaced с пустым namespace для global review.
func TestAllNamespacesRequiresDedicatedReadOperation(t *testing.T) {
	address := ResourceAddress{Group: "apps", Version: "v1", Resource: "deployments", Namespaced: true, AllNamespaces: true}
	checks, err := OperationAccessRequirements("kubernetes.list.all-namespaces", address)
	if err != nil || len(checks) != 1 || checks[0].Verb != "list" || !checks[0].Namespaced || !checks[0].AllNamespaces || checks[0].Namespace != "" {
		t.Fatal("explicit global collection lost its separate access requirement")
	}
	for _, operation := range []string{"kubernetes.list", "kubernetes.get", "kubernetes.create", "kubernetes.delete", "kubernetes.patch"} {
		if _, err := OperationAccessRequirements(operation, address); err == nil {
			t.Fatal("all-namespace address expanded an ordinary or mutable operation")
		}
	}
	for _, verb := range []string{"get", "create", "update", "patch", "delete"} {
		requirement := checks[0]
		requirement.Verb = verb
		if requirement.Validate() == nil {
			t.Fatal("global collection acquired a non-READ/list/watch permission")
		}
	}
}

// TestOperationAccessRequirementsRequiresRegisteredSemantics prevents command and subresource
// substitution and requires optimistic concurrency pins before a resource mutation is checked.
// TestOperationAccessRequirementsRequiresRegisteredSemantics запрещает command/subresource
// substitution и требует optimistic concurrency pins до проверки resource mutation.
func TestOperationAccessRequirementsRequiresRegisteredSemantics(t *testing.T) {
	address := ResourceAddress{Version: "v1", Resource: "pods", Namespaced: true,
		Namespace: "selected", Name: "selected", UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37"}
	checks, err := OperationAccessRequirements("kubernetes.delete", address)
	if err != nil || len(checks) != 1 || checks[0].Verb != "delete" || checks[0].Namespace != address.Namespace || checks[0].Name != address.Name {
		t.Fatal("exact destructive access check was not preserved")
	}
	for _, mutate := range []func(*ResourceAddress){
		func(value *ResourceAddress) { value.UID = "" },
		func(value *ResourceAddress) { value.ResourceVersion = "" },
		func(value *ResourceAddress) { value.Name = "" },
		func(value *ResourceAddress) { value.Subresource = "status" },
		func(value *ResourceAddress) { value.Namespace = "" },
	} {
		changed := address
		mutate(&changed)
		if _, err = OperationAccessRequirements("kubernetes.delete", changed); err == nil {
			t.Fatal("changed or unfenced destructive tuple was admitted")
		}
	}
	for _, key := range []string{"kubernetes.exec", "kubernetes.drain", "kubernetes.raw", "kubernetes.config.view"} {
		if _, err = OperationAccessRequirements(key, address); err == nil {
			t.Fatal("compound or unknown operation inherited a single-resource permission")
		}
	}
	address.Name, address.UID, address.ResourceVersion = "", "", ""
	checks, err = OperationAccessRequirements("kubernetes.list", address)
	if err != nil || len(checks) != 1 || checks[0].Verb != "list" || checks[0].Namespace != "selected" {
		t.Fatal("explicit namespace collection was rejected or widened")
	}
}
