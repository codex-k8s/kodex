package kubernetes

import (
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stesting "k8s.io/client-go/testing"
)

func TestAuthorizationCleanupSnapshotChangeRequiresFreshMetadataBeforeEffect(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"resource version", "UID", "late object", "vanished object", "foreign account"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			store, client := newAuthorizationCleanupStore(t)
			attempt, query := authorizationCleanupFixture()
			stored, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt)
			if err != nil {
				t.Fatal(err)
			}
			target := query
			target.Kind, target.UID, target.ResourceVersion = ProviderCleanupAuthorization, stored.SecretUID, stored.ResourceVersion
			name, _ := providerAttemptName(target.MaterializerAttemptRef)
			switch scenario {
			case "resource version":
				target.ResourceVersion = "stale-version"
			case "UID":
				target.UID = "61000000-0000-4000-8000-000000000999"
			case "late object":
				target.Kind, target.UID, target.ResourceVersion = ProviderCleanupAbsence, "", ""
			case "vanished object":
				if err := client.CoreV1().Secrets(testNamespace).Delete(t.Context(), name, metav1.DeleteOptions{}); err != nil {
					t.Fatal(err)
				}
			case "foreign account":
				secret, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), name, metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				secret.Annotations[providerAccountRefAnnotation] = "pacc_foreign123456"
				if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), secret, testNamespace); err != nil {
					t.Fatal(err)
				}
			}
			client.ClearActions()
			err = store.FenceProviderAuthorization(t.Context(), target)
			if !errors.Is(err, ErrProviderCredentialCleanupConflict) ||
				errors.Is(err, ErrProviderAuthorizationCleanupSnapshotChanged) != (scenario != "foreign account") {
				t.Fatalf("unexpected cleanup conflict classification: %v", err)
			}
			for _, action := range client.Actions() {
				if action.GetVerb() != "get" {
					t.Fatal("stale or foreign target performed a mutation")
				}
			}
			if scenario == "foreign account" {
				return
			}
			observed, err := store.ObserveProviderAuthorizationCleanup(t.Context(), query)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.FenceProviderAuthorization(t.Context(), observed.Target); err != nil {
				t.Fatalf("fresh owner metadata could not fence exact target: %v", err)
			}
		})
	}
}

func TestAuthorizationCleanupCASFailureDiffersFromLostWriteAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"update conflict", "invalid object", "lost write acknowledgement"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			store, client := newAuthorizationCleanupStore(t)
			attempt, target := authorizationCleanupFixture()
			stored, _, err := store.CreateProviderAuthorizationAttempt(t.Context(), attempt)
			if err != nil {
				t.Fatal(err)
			}
			target.Kind, target.UID, target.ResourceVersion = ProviderCleanupAuthorization, stored.SecretUID, stored.ResourceVersion
			client.PrependReactor("update", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
				wanted := action.(k8stesting.UpdateAction).GetObject().(*corev1.Secret).DeepCopy()
				switch scenario {
				case "update conflict":
					return true, nil, apierrors.NewConflict(corev1.Resource("secrets"), wanted.Name, errors.New("synthetic CAS rejection"))
				case "invalid object":
					return true, nil, apierrors.NewInvalid(schema.GroupKind{Kind: "Secret"}, wanted.Name, nil)
				default:
					wanted.ResourceVersion = "999"
					if err := client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("secrets"), wanted, testNamespace); err != nil {
						t.Fatal(err)
					}
					return true, nil, apierrors.NewServiceUnavailable("synthetic committed update with lost response")
				}
			})
			err = store.FenceProviderAuthorization(t.Context(), target)
			if err == nil || errors.Is(err, ErrProviderAuthorizationCleanupSnapshotChanged) != (scenario == "update conflict") {
				t.Fatalf("ambiguous write was classified as no effect: %v", err)
			}
			name, _ := providerAttemptName(target.MaterializerAttemptRef)
			current, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if (current.Labels[providerCleanupFenceLabel] != "") != (scenario == "lost write acknowledgement") {
				t.Fatal("fixture did not preserve the actual mutation outcome")
			}
		})
	}
}
