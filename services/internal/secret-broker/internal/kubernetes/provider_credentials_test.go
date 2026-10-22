package kubernetes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	// providerCleanupTaskRef is a typed synthetic task identifier shared by exact cleanup fixture tests, not
	// an owner-issued live task.
	// providerCleanupTaskRef задаёт типизированный синтетический task идентификатор для exact cleanup
	// fixtures, а не выданную владельцем живую task.
	providerCleanupTaskRef = "pcct_cleanup1234"
	// providerCleanupAccountRef binds synthetic credential fixtures to one account so changed-account tests
	// can prove exact rejection.
	// providerCleanupAccountRef привязывает синтетические credential fixtures к одному account для проверки
	// точного отказа другому account.
	providerCleanupAccountRef = "pacc_cleanup_Account9Z"
	// providerCleanupGeneration is the positive fixture claim generation included in deterministic terminal
	// cleanup receipts.
	// providerCleanupGeneration задаёт положительное поколение fixture claim, включаемое в детерминированные
	// terminal cleanup квитанции.
	providerCleanupGeneration = int64(7)
	// providerCleanupContent supplies synthetic JSON credential bytes for hashing and negative tests; it
	// contains no real account or provider credential.
	// providerCleanupContent задаёт синтетические JSON credential байты для хеширования и негативных тестов;
	// реальных account или provider credentials нет.
	providerCleanupContent = `{"auth_mode":"apikey","OPENAI_API_KEY":"synthetic-cleanup-value"}`
)

// TestDiscardProviderCredentialRequiresExactDescriptorAndIsIdempotent rejects changed UID,
// resourceVersion and digest without deleting the current credential, then checks exact precondition
// options, a material-free fence and idempotent replay.
// TestDiscardProviderCredentialRequiresExactDescriptorAndIsIdempotent отклоняет другие UID,
// resourceVersion и digest без удаления текущего credential, затем проверяет точные precondition
// options, fence без material и идемпотентный повтор.
func TestDiscardProviderCredentialRequiresExactDescriptorAndIsIdempotent(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	// attemptRef pins the synthetic source attempt for both exact discard and its deterministic name.
	// attemptRef закрепляет синтетическую исходную attempt для точного discard и детерминированного имени.
	const attemptRef = "pauth_test12345"
	// accountRef keeps create and compensation in the same synthetic account boundary.
	// accountRef сохраняет создание и компенсацию в границе одного синтетического account.
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
	fence, err := client.CoreV1().Secrets(testNamespace).Get(context.Background(), descriptor.SecretName, metav1.GetOptions{})
	if err != nil || len(fence.Data[providerAuthJSONKey]) != 0 {
		t.Fatalf("credential material survived discard: %v", err)
	}
	if _, err := store.parseProviderCleanupFence(fence, "CREDENTIAL", accountRef); err != nil {
		t.Fatalf("discard did not fence credential name: %v", err)
	}
	if err := store.DiscardProviderCredential(context.Background(), attemptRef, accountRef, descriptor); err != nil {
		t.Fatalf("idempotent discard failed: %v", err)
	}
}

// TestCleanupProviderCredentialDeletesOnlyExactServerOwnedSecret exercises broker/runtime publishers,
// validates bounded metadata receipts and delete options, and requires fence readback with no credential
// payload in the fake tracker.
// TestCleanupProviderCredentialDeletesOnlyExactServerOwnedSecret проверяет издателей broker/runtime,
// ограниченные metadata квитанции и delete options, затем требует чтения fence без credential payload в
// fake tracker.
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
			if receipt.TerminalReceipt == "" || receipt.ProducedCredential != nil || len(receipt.TerminalReceipt) > 512 ||
				strings.Contains(receipt.TerminalReceipt, "synthetic-cleanup-value") || !visibleASCII(receipt.TerminalReceipt) {
				t.Fatalf("terminal receipt is not bounded safe metadata: %#v", receipt)
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
			fence, err := client.CoreV1().Secrets(testNamespace).Get(
				context.Background(), descriptor.SecretName, metav1.GetOptions{},
			)
			if err != nil || len(fence.Data[providerAuthJSONKey]) != 0 {
				t.Fatalf("credential material survived cleanup: %v", err)
			}
			if _, err := store.parseProviderCleanupFence(fence, "CREDENTIAL", providerCleanupAccountRef); err != nil {
				t.Fatalf("cleanup did not fence credential name: %v", err)
			}
		})
	}
}

// TestCleanupProviderCredentialNotFoundIsDeterministicWithoutDelete starts without the target, verifies
// deterministic terminal replay and proves no Delete call occurred; creating the durable fence and
// receipt is still allowed.
// TestCleanupProviderCredentialNotFoundIsDeterministicWithoutDelete начинает без цели, проверяет
// детерминированный terminal повтор и отсутствие Delete вызова; создание durable fence и квитанции при
// этом допускается.
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
	if err != nil || first.TerminalReceipt == "" || second != first {
		t.Fatalf("NotFound receipt is not deterministic: first=%#v second=%#v err=%v", first, second, err)
	}
	if got := providerSecretActionCount(client, "delete", descriptor.SecretName); got != 0 {
		t.Fatalf("NotFound cleanup issued %d delete calls", got)
	}
}

// TestCleanupProviderCredentialValidatesFencedExactInputBeforeKubernetes covers malformed typed
// references, generation, name, UID, resourceVersion and digest and requires an empty fake action log
// for every rejected input.
// TestCleanupProviderCredentialValidatesFencedExactInputBeforeKubernetes проверяет повреждённые
// типизированные ссылки, generation, имя, UID, resourceVersion и digest и требует пустой fake action log
// для каждого отклонённого входа.
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

// TestProviderCredentialCleanupReceiptBindsEveryExactCoordinate varies task, account, generation and
// each descriptor coordinate to ensure eight fixture inputs produce distinct terminal receipt digests
// without storage calls.
// TestProviderCredentialCleanupReceiptBindsEveryExactCoordinate меняет task, account, generation и
// каждую координату descriptor, проверяя восемь разных terminal receipt digest без обращений к
// хранилищу.
func TestProviderCredentialCleanupReceiptBindsEveryExactCoordinate(t *testing.T) {
	t.Parallel()
	_, descriptor := providerCleanupFixture(providerSecretBrokerManager)
	receipts := map[string]struct{}{}
	add := func(taskRef, accountRef string, generation int64, value ProviderCredentialDescriptor) {
		identity, err := credentialCleanupReceiptIdentity(taskRef, accountRef, generation, value)
		if err != nil {
			t.Fatal(err)
		}
		receipts[providerCleanupResultReceipt(identity, nil)] = struct{}{}
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

// TestCleanupProviderCredentialReportsReplacementWithoutDelete substitutes another valid UID and payload
// at the same name and verifies the replacement is returned separately without any Delete action.
// TestCleanupProviderCredentialReportsReplacementWithoutDelete подставляет другой корректный UID и
// payload по тому же имени и проверяет отдельный возврат замены без Delete действий.
func TestCleanupProviderCredentialReportsReplacementWithoutDelete(t *testing.T) {
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
	result, err := store.CleanupProviderCredential(
		context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
	)
	if err != nil || result.ProducedCredential == nil || result.ProducedCredential.SecretUID != string(secret.UID) ||
		result.ProducedCredential.ContentSHA256 != replacementDigestText {
		t.Fatalf("replacement Secret did not become a separate exact target: %v", err)
	}
	if got := providerSecretActionCount(client, "delete", descriptor.SecretName); got != 0 {
		t.Fatalf("replacement Secret received %d delete calls", got)
	}
}

// TestCleanupProviderCredentialRejectsBindingMetadataAndContentMismatch changes account, publisher,
// version, declared digest or actual bytes and verifies cleanup conflict before any credential deletion.
// TestCleanupProviderCredentialRejectsBindingMetadataAndContentMismatch меняет account, издателя,
// версию, заявленный digest или фактические байты и проверяет cleanup conflict до удаления credential.
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

// TestCleanupProviderCredentialRequiresFencedTerminalReadback makes the post-delete GET return the old
// UID instead of a valid fence and verifies that a completed deletion request alone cannot produce
// terminal success.
// TestCleanupProviderCredentialRequiresFencedTerminalReadback подменяет post-delete GET старым UID
// вместо корректного fence и проверяет, что одного завершённого запроса удаления недостаточно для
// terminal успеха.
func TestCleanupProviderCredentialRequiresFencedTerminalReadback(t *testing.T) {
	t.Parallel()
	secret, descriptor := providerCleanupFixture(providerRuntimeManager)
	store, client := newTestStore(t, secret)
	getCalls := 0
	client.Fake.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.(k8stesting.GetAction).GetName() != descriptor.SecretName {
			return false, nil, nil
		}
		getCalls++
		if getCalls != 2 {
			return false, nil, nil
		}
		replacement := secret.DeepCopy()
		return true, replacement, nil
	})
	_, err := store.CleanupProviderCredential(
		context.Background(), providerCleanupTaskRef, providerCleanupAccountRef, providerCleanupGeneration, descriptor,
	)
	if !errors.Is(err, ErrProviderCredentialCleanupConflict) {
		t.Fatalf("replacement after delete was accepted as terminal: %v", err)
	}
}

// TestCleanupProviderCredentialPreconditionConflictFailsClosed injects an API delete conflict and
// verifies conflict classification while the fake credential remains present; it simulates rejection,
// not real server-side CAS.
// TestCleanupProviderCredentialPreconditionConflictFailsClosed внедряет API delete conflict и проверяет
// классификацию конфликта при сохранении fake credential; моделируется отказ, а не реальный серверный
// CAS.
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

// TestCleanupProviderCredentialTemporaryAPIErrorsRemainRetryable injects service-unavailable errors at
// initial read, delete and a subsequent GET, then checks they are not collapsed to permanent
// invalid/conflict sentinels. It does not execute an automatic retry.
// TestCleanupProviderCredentialTemporaryAPIErrorsRemainRetryable внедряет service-unavailable на
// исходном чтении, delete и следующем GET, проверяя отсутствие преобразования в постоянные
// invalid/conflict ошибки. Автоматический повтор не выполняется.
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

// TestDiscardProviderAuthorizationAttemptRequiresExactLineage rejects wrong API pins, then verifies one
// in-place fence Update instead of Delete and idempotent compensation replay on synthetic attempt state.
// TestDiscardProviderAuthorizationAttemptRequiresExactLineage отклоняет неверные API-привязки, затем
// проверяет один Update fence на месте вместо Delete и идемпотентный compensation повтор на
// синтетическом attempt состоянии.
func TestDiscardProviderAuthorizationAttemptRequiresExactLineage(t *testing.T) {
	t.Parallel()
	store, client := newAuthorizationCleanupStore(t)
	attempt, _ := authorizationCleanupFixture()
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
	if providerSecretActionCount(client, "delete", name) != 0 || providerSecretActionCount(client, "update", name) != 1 {
		t.Fatal("pending discard must fence atomically without reopening its name")
	}
	if err := store.DiscardProviderAuthorizationAttempt(context.Background(), stored.AttemptRef, stored.AccountRef,
		stored.MaterializerAttemptRef, stored.SecretUID, stored.ResourceVersion); err != nil {
		t.Fatalf("idempotent attempt discard failed: %v", err)
	}
}

// assertProviderSecretDeletePreconditions checks recorded delete options for the named fake Secret
// against exact UID/resourceVersion; it does not validate actual API-server enforcement.
// assertProviderSecretDeletePreconditions сверяет записанные delete options именованного fake Secret с
// точными UID/resourceVersion; фактическое применение реальным API server не проверяется.
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

// providerCleanupFixture constructs an immutable synthetic credential and matching descriptor with
// broker or runtime provenance; non-broker manager inputs take the runtime annotation branch for
// negative publisher tests.
// providerCleanupFixture строит immutable синтетический credential и соответствующий descriptor с broker
// либо runtime происхождением; manager не из broker выбирает runtime annotations для негативных тестов
// издателя.
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

// providerSecretActionCount counts recorded Secret actions matching verb and name, handling named
// read/delete actions and object-bearing create/update actions without contacting Kubernetes.
// providerSecretActionCount считает записанные Secret действия по verb и имени, обрабатывая именованные
// read/delete и несущие объект create/update без обращения к Kubernetes.
func providerSecretActionCount(client *fake.Clientset, verb, name string) int {
	count := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == verb && action.GetResource().Resource == "secrets" {
			if named, ok := action.(interface{ GetName() string }); ok && named.GetName() == name {
				count++
			} else if write, ok := action.(interface{ GetObject() runtime.Object }); ok {
				if secret, ok := write.GetObject().(*corev1.Secret); ok && secret.Name == name {
					count++
				}
			}
		}
	}
	return count
}

// visibleASCII accepts only nonempty visible ASCII fixture metadata, rejecting spaces and controls when
// checking terminal receipt safety.
// visibleASCII допускает только непустые видимые ASCII metadata fixture, отклоняя пробелы и управляющие
// символы при проверке безопасности terminal квитанции.
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

// changedCleanupDescriptor mutates a value copy of the supplied descriptor for negative test cases and
// leaves the original descriptor unchanged.
// changedCleanupDescriptor изменяет копию значения переданного descriptor для негативного теста,
// оставляя исходный descriptor неизменным.
func changedCleanupDescriptor(
	value ProviderCredentialDescriptor,
	change func(*ProviderCredentialDescriptor),
) ProviderCredentialDescriptor {
	change(&value)
	return value
}
