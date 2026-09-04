package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	providerCleanupTaskRef    = "pcct_cleanup1234"
	providerCleanupAccountRef = "pacc_cleanup_Account9Z"
	providerCleanupGeneration = int64(7)
	providerCleanupContent    = `{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-cleanup-value"}`
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

func TestCleanupProviderCredentialDeletesOnlyExactServerOwnedSecret(t *testing.T) {
	t.Parallel()
	for _, manager := range []string{providerSecretBrokerManager, providerRuntimeManager} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			secret, descriptor := providerCleanupFixture(manager)
			store, client := newTestStore(t, secret)
			receipt, err := store.CleanupProviderCredential(
				context.Background(),
				providerCleanupTaskRef,
				providerCleanupAccountRef,
				providerCleanupGeneration,
				descriptor,
			)
			if err != nil {
				t.Fatal(err)
			}
			if expected := providerCredentialCleanupReceipt(
				providerCleanupTaskRef,
				providerCleanupAccountRef,
				providerCleanupGeneration,
				descriptor,
			); receipt != expected {
				t.Fatalf("terminal receipt = %q, want %q", receipt, expected)
			}
			if len(receipt) > 512 || strings.Contains(receipt, "synthetic-cleanup-value") || !visibleASCII(receipt) {
				t.Fatalf("terminal receipt is not bounded safe metadata: %q", receipt)
			}
			assertProviderSecretDeletePreconditions(
				t,
				client,
				descriptor.SecretName,
				descriptor.SecretUID,
				descriptor.SecretResourceVersion,
			)
			if got := providerSecretActionCount(client, "get", descriptor.SecretName); got != 2 {
				t.Fatalf("exact cleanup GET count = %d, want pre-delete and terminal readback", got)
			}
			if _, err := client.CoreV1().Secrets(testNamespace).Get(
				context.Background(), descriptor.SecretName, metav1.GetOptions{},
			); !apierrors.IsNotFound(err) {
				t.Fatalf("provider credential still exists after exact cleanup: %v", err)
			}
		})
	}
}

func TestCleanupProviderCredentialNotFoundIsDeterministicWithoutDelete(t *testing.T) {
	t.Parallel()
	_, descriptor := providerCleanupFixture(providerSecretBrokerManager)
	store, client := newTestStore(t)
	first, err := store.CleanupProviderCredential(
		context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CleanupProviderCredential(
		context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
	)
	if err != nil || first == "" || second != first {
		t.Fatalf("NotFound receipt is not deterministic: first=%q second=%q err=%v", first, second, err)
	}
	if got := providerSecretActionCount(client, "delete", descriptor.SecretName); got != 0 {
		t.Fatalf("NotFound cleanup issued %d delete calls", got)
	}
}

func TestCleanupProviderCredentialValidatesFencedExactInputBeforeKubernetes(t *testing.T) {
	t.Parallel()
	_, valid := providerCleanupFixture(providerSecretBrokerManager)
	tests := []struct {
		name       string
		taskRef    string
		accountRef string
		generation int64
		descriptor ProviderCredentialDescriptor
	}{
		{name: "task prefix", taskRef: "task_cleanup1234", accountRef: providerCleanupAccountRef, generation: 1, descriptor: valid},
		{name: "task suffix", taskRef: "pcct_short", accountRef: providerCleanupAccountRef, generation: 1, descriptor: valid},
		{name: "account prefix", taskRef: providerCleanupTaskRef, accountRef: "account_cleanup1234", generation: 1, descriptor: valid},
		{name: "account suffix", taskRef: providerCleanupTaskRef, accountRef: "pacc_short", generation: 1, descriptor: valid},
		{name: "generation", taskRef: providerCleanupTaskRef, accountRef: providerCleanupAccountRef, descriptor: valid},
		{name: "secret name", taskRef: providerCleanupTaskRef, accountRef: providerCleanupAccountRef, generation: 1, descriptor: changedCleanupDescriptor(valid, func(value *ProviderCredentialDescriptor) {
			value.SecretName = "Invalid-Secret"
		})},
		{name: "secret UID", taskRef: providerCleanupTaskRef, accountRef: providerCleanupAccountRef, generation: 1, descriptor: changedCleanupDescriptor(valid, func(value *ProviderCredentialDescriptor) {
			value.SecretUID = "not-a-uuid"
		})},
		{name: "resource version", taskRef: providerCleanupTaskRef, accountRef: providerCleanupAccountRef, generation: 1, descriptor: changedCleanupDescriptor(valid, func(value *ProviderCredentialDescriptor) {
			value.SecretResourceVersion = " unsafe"
		})},
		{name: "digest", taskRef: providerCleanupTaskRef, accountRef: providerCleanupAccountRef, generation: 1, descriptor: changedCleanupDescriptor(valid, func(value *ProviderCredentialDescriptor) {
			value.ContentSHA256 = strings.Repeat("A", sha256.Size*2)
		})},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store, client := newTestStore(t)
			_, err := store.CleanupProviderCredential(
				context.Background(), test.taskRef, test.accountRef, test.generation, test.descriptor,
			)
			if !errors.Is(err, ErrProviderCredentialCleanupInvalid) {
				t.Fatalf("invalid cleanup input was accepted: %v", err)
			}
			if len(client.Actions()) != 0 {
				t.Fatalf("invalid cleanup input reached Kubernetes: %#v", client.Actions())
			}
		})
	}
}

func TestProviderCredentialCleanupReceiptBindsEveryExactCoordinate(t *testing.T) {
	t.Parallel()
	_, descriptor := providerCleanupFixture(providerSecretBrokerManager)
	receipts := map[string]struct{}{}
	add := func(taskRef, accountRef string, generation int64, value ProviderCredentialDescriptor) {
		receipts[providerCredentialCleanupReceipt(taskRef, accountRef, generation, value)] = struct{}{}
	}
	add(providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor)
	add("pcct_cleanup5678", providerCleanupAccountRef, providerCleanupGeneration, descriptor)
	add(providerCleanupTaskRef, "pacc_cleanup5678", providerCleanupGeneration, descriptor)
	add(providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration+1, descriptor)
	for _, change := range []func(*ProviderCredentialDescriptor){
		func(value *ProviderCredentialDescriptor) { value.SecretName = "provider-credential-other" },
		func(value *ProviderCredentialDescriptor) { value.SecretUID = "62000000-0000-4000-8000-000000000005" },
		func(value *ProviderCredentialDescriptor) { value.SecretResourceVersion = "cleanup-8" },
		func(value *ProviderCredentialDescriptor) { value.ContentSHA256 = strings.Repeat("b", sha256.Size*2) },
	} {
		add(providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, changedCleanupDescriptor(descriptor, change))
	}
	if len(receipts) != 8 {
		t.Fatalf("terminal receipt does not bind every exact coordinate: %d unique receipts", len(receipts))
	}
}

func TestCleanupProviderCredentialRejectsReplacementWithoutDelete(t *testing.T) {
	t.Parallel()
	secret, descriptor := providerCleanupFixture(providerRuntimeManager)
	replacementContent := []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"replacement-value"}`)
	replacementDigest := sha256.Sum256(replacementContent)
	replacementDigestText := hex.EncodeToString(replacementDigest[:])
	secret.UID = types.UID("62000000-0000-4000-8000-000000000002")
	secret.ResourceVersion = "replacement-8"
	secret.Annotations[providerRuntimeContentSHA] = replacementDigestText
	secret.Data[providerAuthJSONKey] = replacementContent
	secret.Data[providerAuthSHA256Key] = []byte(replacementDigestText)
	store, client := newTestStore(t, secret)
	_, err := store.CleanupProviderCredential(
		context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
	)
	if !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatalf("replacement Secret was not rejected: %v", err)
	}
	if got := providerSecretActionCount(client, "delete", descriptor.SecretName); got != 0 {
		t.Fatalf("replacement Secret received %d delete calls", got)
	}
}

func TestCleanupProviderCredentialRejectsBindingMetadataAndContentMismatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*corev1.Secret, *ProviderCredentialDescriptor)
	}{
		{name: "account binding", change: func(secret *corev1.Secret, _ *ProviderCredentialDescriptor) {
			secret.Annotations[providerAccountRefAnnotation] = "pacc_different123"
		}},
		{name: "managed label", change: func(secret *corev1.Secret, _ *ProviderCredentialDescriptor) {
			secret.Labels[providerManagedByLabel] = "foreign-controller"
		}},
		{name: "UID", change: func(_ *corev1.Secret, descriptor *ProviderCredentialDescriptor) {
			descriptor.SecretUID = "62000000-0000-4000-8000-000000000003"
		}},
		{name: "resource version", change: func(_ *corev1.Secret, descriptor *ProviderCredentialDescriptor) {
			descriptor.SecretResourceVersion = "different-version"
		}},
		{name: "descriptor digest", change: func(_ *corev1.Secret, descriptor *ProviderCredentialDescriptor) {
			descriptor.ContentSHA256 = strings.Repeat("b", sha256.Size*2)
		}},
		{name: "actual content", change: func(secret *corev1.Secret, _ *ProviderCredentialDescriptor) {
			secret.Data[providerAuthJSONKey] = []byte(`{"auth_mode":"apikey","OPENAI_API_KEY":"changed-value"}`)
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			secret, descriptor := providerCleanupFixture(providerSecretBrokerManager)
			test.change(secret, &descriptor)
			store, client := newTestStore(t, secret)
			_, err := store.CleanupProviderCredential(
				context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
			)
			if !errors.Is(err, ErrProviderCredentialCleanupConflict) {
				t.Fatalf("mismatch was not rejected: %v", err)
			}
			if got := providerSecretActionCount(client, "delete", descriptor.SecretName); got != 0 {
				t.Fatalf("mismatched Secret received %d delete calls", got)
			}
		})
	}
}

func TestCleanupProviderCredentialRequiresTerminalNotFoundReadback(t *testing.T) {
	t.Parallel()
	secret, descriptor := providerCleanupFixture(providerRuntimeManager)
	store, client := newTestStore(t, secret)
	getCalls := 0
	client.Fake.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		getCalls++
		if getCalls != 2 {
			return false, nil, nil
		}
		replacement := secret.DeepCopy()
		replacement.UID = types.UID("62000000-0000-4000-8000-000000000004")
		replacement.ResourceVersion = "replacement-after-delete"
		return true, replacement, nil
	})
	_, err := store.CleanupProviderCredential(
		context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
	)
	if !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatalf("replacement after delete was accepted as terminal: %v", err)
	}
}

func TestCleanupProviderCredentialPreconditionConflictFailsClosed(t *testing.T) {
	t.Parallel()
	secret, descriptor := providerCleanupFixture(providerRuntimeManager)
	store, client := newTestStore(t, secret)
	client.Fake.PrependReactor("delete", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewConflict(corev1.Resource("secrets"), descriptor.SecretName, errors.New("synthetic replacement race"))
	})
	_, err := store.CleanupProviderCredential(
		context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
	)
	if !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatalf("precondition conflict was not rejected: %v", err)
	}
	if _, err := client.CoreV1().Secrets(testNamespace).Get(
		context.Background(), descriptor.SecretName, metav1.GetOptions{},
	); err != nil {
		t.Fatalf("precondition conflict removed provider credential: %v", err)
	}
}

func TestCleanupProviderCredentialTemporaryAPIErrorsRemainRetryable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(*fake.Clientset)
	}{
		{name: "initial read", setup: func(client *fake.Clientset) {
			client.Fake.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewServiceUnavailable("synthetic outage")
			})
		}},
		{name: "delete", setup: func(client *fake.Clientset) {
			client.Fake.PrependReactor("delete", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
				return true, nil, apierrors.NewServiceUnavailable("synthetic outage")
			})
		}},
		{name: "terminal readback", setup: func(client *fake.Clientset) {
			getCalls := 0
			client.Fake.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
				getCalls++
				if getCalls == 2 {
					return true, nil, apierrors.NewServiceUnavailable("synthetic outage")
				}
				return false, nil, nil
			})
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			secret, descriptor := providerCleanupFixture(providerRuntimeManager)
			store, client := newTestStore(t, secret)
			test.setup(client)
			_, err := store.CleanupProviderCredential(
				context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
			)
			if err == nil || errors.Is(err, ErrProviderCredentialCleanupConflict) ||
				errors.Is(err, ErrProviderCredentialCleanupInvalid) {
				t.Fatalf("temporary Kubernetes API error lost retryable classification: %v", err)
			}
		})
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

func providerCleanupFixture(manager string) (*corev1.Secret, ProviderCredentialDescriptor) {
	content := []byte(providerCleanupContent)
	digest := sha256.Sum256(content)
	digestText := hex.EncodeToString(digest[:])
	immutable := true
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "provider-credential-cleanup",
			Namespace:       testNamespace,
			UID:             types.UID("61000000-0000-4000-8000-000000000002"),
			ResourceVersion: "cleanup-7",
			Labels: map[string]string{
				providerManagedByLabel: manager,
				providerPartOfLabel:    "kodex",
			},
			Annotations: map[string]string{},
		},
		Immutable: &immutable,
		Type:      corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			providerAuthJSONKey:   content,
			providerAuthSHA256Key: []byte(digestText),
		},
	}
	if manager == providerSecretBrokerManager {
		secret.Labels[providerCredentialLabel] = "true"
		secret.Annotations[providerAccountRefAnnotation] = providerCleanupAccountRef
		secret.Annotations[providerContentSHAAnnotation] = digestText
	} else {
		secret.Labels[providerRuntimeManagedLabel] = "true"
		secret.Annotations[providerRuntimeAccountRef] = providerCleanupAccountRef
		secret.Annotations[providerRuntimeContentSHA] = digestText
	}
	return secret, ProviderCredentialDescriptor{
		SecretName: secret.Name, SecretUID: string(secret.UID),
		SecretResourceVersion: secret.ResourceVersion, ContentSHA256: digestText,
	}
}

func providerSecretActionCount(client *fake.Clientset, verb, name string) int {
	count := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == verb && action.GetResource().Resource == "secrets" {
			if named, ok := action.(interface{ GetName() string }); ok && named.GetName() == name {
				count++
			}
		}
	}
	return count
}

func visibleASCII(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func changedCleanupDescriptor(
	value ProviderCredentialDescriptor,
	change func(*ProviderCredentialDescriptor),
) ProviderCredentialDescriptor {
	change(&value)
	return value
}
