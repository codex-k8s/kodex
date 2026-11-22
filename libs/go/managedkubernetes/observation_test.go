package managedkubernetes

import (
	"strings"
	"testing"
)

// TestResourceObservationRequiresExplicitPresence keeps create/collection facts distinct from
// item CAS and rejects unknown states rather than fabricating UID/RV for an absent object.
// TestResourceObservationRequiresExplicitPresence разделяет create/collection facts и item CAS,
// отклоняя unknown states вместо выдуманных UID/RV отсутствующего объекта.
func TestResourceObservationRequiresExplicitPresence(t *testing.T) {
	base := ResourceObservation{State: ObservationPresent, Resource: ResourceAddress{Version: "v1", Resource: "configmaps",
		Namespaced: true, Namespace: "selected", Name: "selected", UID: "9a99d4f2-836d-4efb-868e-5de8792ade07", ResourceVersion: "37"},
		ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
	if base.Validate() != nil {
		t.Fatal("exact present object observation rejected")
	}
	absent := base
	absent.State, absent.Resource.UID, absent.Resource.ResourceVersion = ObservationAbsent, "", ""
	collection := absent
	collection.State, collection.Resource.Name = ObservationCollection, ""
	if absent.Validate() != nil || collection.Validate() != nil {
		t.Fatal("explicit absent or scoped collection observation rejected")
	}
	for _, candidate := range []ResourceObservation{base, absent, collection} {
		for _, mutate := range []func(*ResourceObservation){
			func(value *ResourceObservation) { value.State = "" },
			func(value *ResourceObservation) { value.State = "UNKNOWN" },
			func(value *ResourceObservation) { value.Resource.Namespace = "" },
			func(value *ResourceObservation) { value.ObservedResourceDigest = "" },
			func(value *ResourceObservation) { value.RegisteredSchemaDigest = "" },
		} {
			changed := candidate
			mutate(&changed)
			if changed.Validate() == nil {
				t.Fatal("ambiguous discovery observation accepted")
			}
		}
	}
	for _, state := range []ObservationState{ObservationAbsent, ObservationCollection} {
		changed := base
		changed.State = state
		if changed.Validate() == nil {
			t.Fatal("present item was relabeled absent or collection")
		}
	}
	absent.Resource.Subresource = "status"
	if absent.Validate() == nil {
		t.Fatal("absent object granted a subresource locator")
	}
}

// TestResourceObservationNamespaceWitnessNeverInventsCreateAuthority preserves ordinary
// name-scoped CREATE/READ without an extra namespace permission and rejects fabricated scalar
// evidence for cluster/all-namespace scopes. Comparing generations belongs to the owner.
// TestResourceObservationNamespaceWitnessNeverInventsCreateAuthority сохраняет ordinary
// name-scoped CREATE/READ без дополнительного namespace permission и отклоняет выдуманное
// scalar evidence для cluster/all-namespace scope. Поколения сравнивает owner.
func TestResourceObservationNamespaceWitnessNeverInventsCreateAuthority(t *testing.T) {
	observation := ResourceObservation{State: ObservationAbsent,
		Resource:               ResourceAddress{Version: "v1", Resource: "configmaps", Namespaced: true, Namespace: "selected", Name: "new-object"},
		ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
	if observation.Validate() != nil {
		t.Fatal("ordinary name-scoped create required fabricated namespace evidence")
	}
	observation.NamespaceUID = "9a99d4f2-836d-4efb-868e-5de8792ade07"
	if observation.Validate() != nil {
		t.Fatal("observed optional namespace witness was rejected")
	}
	for _, invalid := range []string{"unknown", "00000000-0000-0000-0000-000000000000", "9A99D4F2-836D-4EFB-868E-5DE8792ADE07"} {
		changed := observation
		changed.NamespaceUID = invalid
		if changed.Validate() == nil {
			t.Fatal("fabricated namespace UID became observed evidence")
		}
	}
	cluster := observation
	cluster.Resource.Namespaced, cluster.Resource.Namespace = false, ""
	if cluster.Validate() == nil {
		t.Fatal("cluster scope accepted a namespace witness")
	}
	all := observation
	all.State, all.Resource.Name, all.Resource.Namespace, all.Resource.AllNamespaces = ObservationCollection, "", "", true
	if all.Validate() == nil {
		t.Fatal("all-namespace scope was represented by a single namespace UID")
	}
	all.NamespaceUID = ""
	if all.Validate() != nil {
		t.Fatal("explicit all-namespace scope without fabricated witness was rejected")
	}
}

// TestResourceObservationUnversionedReadDoesNotInventStorageIdentity keeps computed named API
// views separate from current-object CAS. READ/NONE/discovery classification is enforced by
// consumers; this value alone never proves a permission or admits a write/Pod stream.
// TestResourceObservationUnversionedReadDoesNotInventStorageIdentity отделяет computed named API
// views от current-object CAS. READ/NONE/discovery classification проверяют consumers;
// value сама не доказывает permission и не допускает write/Pod stream.
func TestResourceObservationUnversionedReadDoesNotInventStorageIdentity(t *testing.T) {
	observation := ResourceObservation{State: ObservationUnversionedRead,
		Resource:               ResourceAddress{Version: "v1", Resource: "componentstatuses", Name: "scheduler"},
		ObservedResourceDigest: strings.Repeat("a", 64), RegisteredSchemaDigest: strings.Repeat("b", 64)}
	if observation.Validate() != nil {
		t.Fatal("computed named read required fabricated storage identity")
	}
	for _, edit := range []func(*ResourceObservation){
		func(value *ResourceObservation) { value.Resource.Name = "" },
		func(value *ResourceObservation) { value.Resource.UID = "11111111-1111-4111-8111-111111111111" },
		func(value *ResourceObservation) { value.Resource.ResourceVersion = "37" },
		func(value *ResourceObservation) { value.State = ObservationPresent },
		func(value *ResourceObservation) { value.Resource.Resource = "secrets" },
		func(value *ResourceObservation) { value.Resource.Resource = "pods" },
		func(value *ResourceObservation) { value.Resource.Resource = "serviceaccounts" },
		func(value *ResourceObservation) { value.Resource.Subresource = "status" },
		func(value *ResourceObservation) { value.Resource.Group = "authentication.k8s.io" },
		func(value *ResourceObservation) { value.Resource.Group = "authorization.k8s.io" },
		func(value *ResourceObservation) { value.Resource.Group = "rbac.authorization.k8s.io" },
		func(value *ResourceObservation) { value.Resource.Group = "certificates.k8s.io" },
	} {
		changed := observation
		edit(&changed)
		if changed.Validate() == nil {
			t.Fatal("unversioned view was relabeled as storage/CAS evidence")
		}
	}
}
