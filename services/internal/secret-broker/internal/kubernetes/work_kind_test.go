package kubernetes

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMaterializationWorkKindIsRequiredAndNeverInferred(t *testing.T) {
	for _, kind := range []MaterializationWorkKind{"", "UNKNOWN", "CREATE", "PUBLISH"} {
		t.Run(string(kind), func(t *testing.T) {
			store, client := newTestStore(t)
			effect, content := testEffect("sdop_fixture01", 1, "sec_fixture01", 1, "synthetic value")
			effect.WorkKind = kind
			if _, err := store.CreateImmutableForEffect(t.Context(), effect, content); err == nil {
				t.Fatal("missing/unknown source was inferred from operation prefix")
			}
			list, err := client.CoreV1().Secrets(testNamespace).List(t.Context(), metav1.ListOptions{})
			if err != nil || len(list.Items) != 0 {
				t.Fatal("invalid work kind caused a materialization effect")
			}
		})
	}
}

func TestMaterializationOwnerRouteIsExactAcrossLookupReadbackAndDelete(t *testing.T) {
	for _, kind := range []MaterializationWorkKind{WorkKindImmediate, WorkKindDraft} {
		t.Run(string(kind), func(t *testing.T) {
			store, _ := newTestStore(t)
			effect, content := testEffect("synthetic_operation", 1, "sec_fixture01", 1, "synthetic value")
			effect.WorkKind = kind
			materialized, err := store.CreateImmutableForEffect(t.Context(), effect, content)
			if err != nil || materialized.WorkKind != kind {
				t.Fatalf("source pin lost: %+v %v", materialized, err)
			}
			foreignEffect, foreignMaterialization := effect, materialized
			foreignEffect.WorkKind, foreignMaterialization.WorkKind = WorkKindDraft, WorkKindDraft
			if kind == WorkKindDraft {
				foreignEffect.WorkKind, foreignMaterialization.WorkKind = WorkKindImmediate, WorkKindImmediate
			}
			if _, err := store.LookupExpectedEffect(t.Context(), foreignEffect); err == nil {
				t.Fatal("lookup crossed owner route")
			}
			if _, err := store.ReadbackExact(t.Context(), foreignMaterialization); err == nil {
				t.Fatal("readback crossed owner route")
			}
			if err := store.DeleteExact(t.Context(), foreignMaterialization); err == nil {
				t.Fatal("delete crossed owner route")
			}
			if actual, err := store.ReadbackExact(t.Context(), materialized); err != nil || actual != materialized {
				t.Fatal("foreign route changed source materialization")
			}
		})
	}
}

func TestOldOrUnknownMaterializationLabelFailsClosedWithoutUpgradeOrDelete(t *testing.T) {
	for _, kind := range []string{"", "UNKNOWN"} {
		store, client := newTestStore(t)
		effect, content := testEffect("sdop_fixture01", 1, "sec_fixture01", 1, "synthetic value")
		materialized, err := store.CreateImmutableForEffect(t.Context(), effect, content)
		if err != nil {
			t.Fatal(err)
		}
		object, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), materialized.Name, metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		object.Annotations[workKindAnnotation] = kind
		if _, err := client.CoreV1().Secrets(testNamespace).Update(t.Context(), object, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ListManaged(t.Context()); err == nil {
			t.Fatal("legacy label was upgraded during scan")
		}
		if err := store.DeleteExact(t.Context(), materialized); err == nil {
			t.Fatal("legacy label reached delete effect")
		}
		actual, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), materialized.Name, metav1.GetOptions{})
		if err != nil || actual.Annotations[workKindAnnotation] != kind || actual.UID != object.UID {
			t.Fatal("legacy object was upgraded or deleted")
		}
	}
}
