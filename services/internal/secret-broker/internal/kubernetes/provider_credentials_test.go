package kubernetes

import (
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestDiscardProviderCredentialRequiresExactDescriptorAndIsIdempotent(t *testing.T) {
	t.Parallel()
	store, client := newTestStore(t)
	const attemptRef = "pauth_test12345"
	const accountRef = "pacc_test12345"
	descriptor, err := store.CreateProviderCredential(context.Background(), attemptRef, accountRef,
		[]byte(`{"OPENAI_API_KEY":"synthetic-test-value","auth_mode":"apikey"}`))
	if err != nil {
		t.Fatal(err)
	}
	wrong := descriptor
	wrong.SecretResourceVersion = "different-version"
	if err := store.DiscardProviderCredential(context.Background(), attemptRef, accountRef, wrong); !errors.Is(err, ErrProviderCredentialConflict) {
		t.Fatalf("wrong resource version must fail closed, got %v", err)
	}
	wrong = descriptor
	wrong.SecretUID = "different-uid"
	if err := store.DiscardProviderCredential(context.Background(), attemptRef, accountRef, wrong); !errors.Is(err, ErrProviderCredentialConflict) {
		t.Fatalf("wrong UID must fail closed, got %v", err)
	}
	wrong = descriptor
	wrong.ContentSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := store.DiscardProviderCredential(context.Background(), attemptRef, accountRef, wrong); !errors.Is(err, ErrProviderCredentialConflict) {
		t.Fatalf("wrong digest must fail closed, got %v", err)
	}
	if _, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), descriptor.SecretName, metav1.GetOptions{}); err != nil {
		t.Fatalf("failed exact check deleted credential: %v", err)
	}
	if err := store.DiscardProviderCredential(context.Background(), attemptRef, accountRef, descriptor); err != nil {
		t.Fatal(err)
	}
	assertProviderSecretDeletePreconditions(t, client, descriptor.SecretName, descriptor.SecretUID, descriptor.SecretResourceVersion)
	if _, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), descriptor.SecretName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("credential was not deleted: %v", err)
	}
	if err := store.DiscardProviderCredential(context.Background(), attemptRef, accountRef, descriptor); err != nil {
		t.Fatalf("idempotent discard failed: %v", err)
	}
}

func TestDiscardProviderAuthorizationAttemptRequiresExactLineage(t *testing.T) {
	t.Parallel()
	store, client := newTestStore(t)
	attempt := ProviderAuthorizationAttempt{
		AttemptRef: "pauth_device123", AccountRef: "pacc_device123", MaterializerAttemptRef: "pmat_device123",
		State: "PENDING", VerificationURI: "https://example.invalid/device", UserCode: "ABCD-EFGH",
		ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}
	stored, created, err := store.CreateProviderAuthorizationAttempt(context.Background(), attempt)
	if err != nil || !created {
		t.Fatalf("create attempt: created=%v err=%v", created, err)
	}
	if err := store.DiscardProviderAuthorizationAttempt(context.Background(), stored.AttemptRef, stored.AccountRef,
		stored.MaterializerAttemptRef, "different-uid", stored.ResourceVersion); !errors.Is(err, ErrProviderAttemptConflict) {
		t.Fatalf("wrong UID must fail closed, got %v", err)
	}
	if err := store.DiscardProviderAuthorizationAttempt(context.Background(), stored.AttemptRef, stored.AccountRef,
		stored.MaterializerAttemptRef, stored.SecretUID, "different-version"); !errors.Is(err, ErrProviderAttemptConflict) {
		t.Fatalf("wrong resource version must fail closed, got %v", err)
	}
	name, _ := providerAttemptName(stored.MaterializerAttemptRef)
	if _, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Fatalf("failed exact check deleted attempt: %v", err)
	}
	if err := store.DiscardProviderAuthorizationAttempt(context.Background(), stored.AttemptRef, stored.AccountRef,
		stored.MaterializerAttemptRef, stored.SecretUID, stored.ResourceVersion); err != nil {
		t.Fatal(err)
	}
	assertProviderSecretDeletePreconditions(t, client, name, stored.SecretUID, stored.ResourceVersion)
	if err := store.DiscardProviderAuthorizationAttempt(context.Background(), stored.AttemptRef, stored.AccountRef,
		stored.MaterializerAttemptRef, stored.SecretUID, stored.ResourceVersion); err != nil {
		t.Fatalf("idempotent attempt discard failed: %v", err)
	}
}

func assertProviderSecretDeletePreconditions(
	t *testing.T,
	client *fake.Clientset,
	name, uid, resourceVersion string,
) {
	t.Helper()
	for _, action := range client.Actions() {
		deleteAction, ok := action.(k8stesting.DeleteAction)
		if !ok || action.GetResource().Resource != "secrets" || deleteAction.GetName() != name {
			continue
		}
		preconditions := deleteAction.GetDeleteOptions().Preconditions
		if preconditions == nil || preconditions.UID == nil || string(*preconditions.UID) != uid ||
			preconditions.ResourceVersion == nil || *preconditions.ResourceVersion != resourceVersion {
			t.Fatalf("unexpected provider Secret delete preconditions: %#v", preconditions)
		}
		return
	}
	t.Fatal("provider Secret delete action was not recorded")
}
