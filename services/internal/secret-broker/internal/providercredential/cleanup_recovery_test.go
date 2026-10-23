package providercredential

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// recoveryServiceFixture creates a fake Kubernetes tracker with synthetic API pins, a real cleanup
// service and a synthetic produced credential; the registered cleanup closes the service lifecycle
// without any provider connection.
// recoveryServiceFixture создаёт fake Kubernetes tracker с синтетическими API-привязками, реальный
// cleanup service и синтетический произведённый credential; зарегистрированный cleanup закрывает
// lifecycle сервиса без provider соединения.
func recoveryServiceFixture(t *testing.T) (*Service, *kubernetesstore.Store, *fake.Clientset, kubernetesstore.ProviderAuthorizationCleanupTarget, kubernetesstore.ProviderCredentialDescriptor) {
	t.Helper()
	client := fake.NewSimpleClientset()
	var revision int64 = 100
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		revision++
		secret.UID, secret.ResourceVersion = types.UID(fmt.Sprintf("61000000-0000-4000-8000-%012d", revision)), fmt.Sprint(revision)
		err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), secret, secret.Namespace)
		return true, secret, err
	})
	store, err := kubernetesstore.New(client, "kodex-runtime")
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(t.Context(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	target := kubernetesstore.ProviderAuthorizationCleanupTarget{TaskRef: "pcct_original1234", AccountRef: "pacc_recovery1234", AuthorizationAttemptRef: "pauth_recovery1234", Kind: kubernetesstore.ProviderCleanupAbsence, Generation: 7}
	digest := sha256.Sum256([]byte(target.AuthorizationAttemptRef + "\x00" + target.AccountRef))
	target.MaterializerAttemptRef = "pmat_" + hex.EncodeToString(digest[:16])
	produced, err := store.CreateProviderCredential(t.Context(), target.AuthorizationAttemptRef, target.AccountRef, []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-only"}}`))
	if err != nil {
		t.Fatal(err)
	}
	return service, store, client, target, produced
}

// TestCleanupRecoveryKeepsProducedAcrossSuccessorsAndRestart verifies that canonical-origin and bounded
// historical receipts preserve the original produced descriptor across successor task references and a
// new service instance after the credential itself is cleaned.
// TestCleanupRecoveryKeepsProducedAcrossSuccessorsAndRestart проверяет, что canonical-origin и
// ограниченные исторические квитанции сохраняют исходный produced descriptor при смене successor task и
// экземпляра сервиса после cleanup самого credential.
func TestCleanupRecoveryKeepsProducedAcrossSuccessorsAndRestart(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint("legacy=", legacy), func(t *testing.T) {
			service, store, _, target, produced := recoveryServiceFixture(t)
			recovery := kubernetesstore.ProviderCleanupRecoveryIdentity{TaskRef: target.TaskRef, Generation: 1}
			var result kubernetesstore.ProviderCredentialCleanupResult
			var err error
			if legacy {
				result, err = service.CleanupAuthorization(t.Context(), target)
				recovery.LegacyLastGeneration = target.Generation
			} else {
				result, err = service.CleanupAuthorizationWithRecovery(t.Context(), target, recovery)
			}
			if err != nil || result.ProducedCredential == nil || *result.ProducedCredential != produced {
				t.Fatalf("initial cleanup: %v", err)
			}
			if _, err := service.CleanupProviderCredential(t.Context(), "pcct_produced1234", target.AccountRef, 1, produced); err != nil {
				t.Fatal(err)
			}
			restarted, err := New(t.Context(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			for _, task := range []string{"pcct_successor1234", "pcct_successor5678"} {
				target.TaskRef, target.Generation = task, 3
				result, err = restarted.CleanupAuthorizationWithRecovery(t.Context(), target, recovery)
				if err != nil || result.ProducedCredential == nil || *result.ProducedCredential != produced {
					t.Fatalf("successor lost original produced: %v", err)
				}
				current, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(target)
				stored, found, err := store.ReadProviderCleanupReceipt(t.Context(), current)
				if err != nil || !found || stored.TerminalReceipt != result.TerminalReceipt {
					t.Fatal("current receipt not persisted")
				}
			}
		})
	}
}

// TestCleanupRecoveryRejectsConflictingLegacyProduced seeds historical receipts with different
// replacement outcomes and requires read-only conflict rather than choosing one result or mutating
// cleanup state.
// TestCleanupRecoveryRejectsConflictingLegacyProduced заранее помещает исторические квитанции с разными
// replacement исходами и требует read-only конфликта вместо выбора результата или изменения cleanup
// состояния.
func TestCleanupRecoveryRejectsConflictingLegacyProduced(t *testing.T) {
	service, store, client, target, produced := recoveryServiceFixture(t)
	for generation := int64(2); generation <= 3; generation++ {
		old := target
		old.Generation = generation
		identity, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(old)
		var descriptor *kubernetesstore.ProviderCredentialDescriptor
		if generation == 2 {
			descriptor = &produced
		}
		if _, err := store.CompleteProviderCleanupReceipt(t.Context(), identity, descriptor); err != nil {
			t.Fatal(err)
		}
	}
	client.ClearActions()
	_, err := service.CleanupAuthorizationWithRecovery(t.Context(), target, kubernetesstore.ProviderCleanupRecoveryIdentity{TaskRef: target.TaskRef, Generation: 1, LegacyLastGeneration: 3})
	if err == nil {
		t.Fatal("conflicting legacy outcomes accepted")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("conflicting receipts caused a mutation")
		}
	}
}

// TestCleanupRecoveryRejectsMalformedOriginBeforeStore supplies absent, foreign, future or over-budget
// recovery identities and verifies rejection before any fake storage action.
// TestCleanupRecoveryRejectsMalformedOriginBeforeStore передаёт отсутствующие, чужие, будущие либо
// превышающие бюджет recovery identity и проверяет отказ до любого fake storage действия.
func TestCleanupRecoveryRejectsMalformedOriginBeforeStore(t *testing.T) {
	service, _, client, target, _ := recoveryServiceFixture(t)
	for _, identity := range []kubernetesstore.ProviderCleanupRecoveryIdentity{
		{}, {TaskRef: "foreign", Generation: 1}, {TaskRef: target.TaskRef, Generation: 8},
		{TaskRef: target.TaskRef, Generation: 1, LegacyLastGeneration: 33}, {TaskRef: target.TaskRef, Generation: 2, LegacyLastGeneration: 3},
	} {
		client.ClearActions()
		if _, err := service.CleanupAuthorizationWithRecovery(t.Context(), target, identity); err == nil || len(client.Actions()) != 0 {
			t.Fatal("invalid origin reached store")
		}
	}
}

// TestCleanupRecoveryPersistsOriginBeforeLostAcknowledgement commits the origin receipt but drops its
// response, verifies current was not persisted first, then restarts with a successor and recovers the
// original produced descriptor despite credential cleanup.
// TestCleanupRecoveryPersistsOriginBeforeLostAcknowledgement записывает origin квитанцию, но теряет
// ответ, проверяет отсутствие преждевременной current записи, затем после перезапуска с successor
// восстанавливает исходный produced descriptor несмотря на cleanup credential.
func TestCleanupRecoveryPersistsOriginBeforeLostAcknowledgement(t *testing.T) {
	service, store, client, target, produced := recoveryServiceFixture(t)
	lost := false
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		if lost || secret.Labels["provider-credentials.kodex.dev/cleanup-receipt"] != "true" {
			return false, nil, nil
		}
		lost = true
		secret.UID, secret.ResourceVersion = "61000000-0000-4000-8000-000000000999", "999"
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), secret, secret.Namespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, apierrors.NewServiceUnavailable("synthetic committed receipt with lost response")
	})
	recovery := kubernetesstore.ProviderCleanupRecoveryIdentity{TaskRef: target.TaskRef, Generation: 1}
	if _, err := service.CleanupAuthorizationWithRecovery(t.Context(), target, recovery); err == nil || errors.Is(err, kubernetesstore.ErrProviderAuthorizationCleanupSnapshotChanged) {
		t.Fatal("lost receipt ACK became success or no-effect proof")
	}
	originTarget := target
	originTarget.Generation = 1
	origin, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(originTarget)
	if result, found, err := store.ReadProviderCleanupReceipt(t.Context(), origin); err != nil || !found || result.ProducedCredential == nil || *result.ProducedCredential != produced {
		t.Fatal("origin was not persisted first")
	}
	current, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(target)
	if _, found, err := store.ReadProviderCleanupReceipt(t.Context(), current); err != nil || found {
		t.Fatal("current receipt preceded confirmed origin")
	}
	if _, err := service.CleanupProviderCredential(t.Context(), "pcct_produced1234", target.AccountRef, 1, produced); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(t.Context(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	target.TaskRef, target.Generation = "pcct_replacement1234", 1
	result, err := restarted.CleanupAuthorizationWithRecovery(t.Context(), target, recovery)
	if err != nil || result.ProducedCredential == nil || *result.ProducedCredential != produced {
		t.Fatalf("lost-ACK successor forgot produced: %v", err)
	}
}

// TestCleanupRecoveryRejectsCurrentOriginDisagreement preloads a current absence result and an origin
// replacement result, then verifies disagreement is rejected using only GET actions.
// TestCleanupRecoveryRejectsCurrentOriginDisagreement заранее сохраняет current результат отсутствия и
// origin результат с заменой, затем проверяет отказ противоречию с выполнением только GET.
func TestCleanupRecoveryRejectsCurrentOriginDisagreement(t *testing.T) {
	service, store, client, target, produced := recoveryServiceFixture(t)
	current, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(target)
	originTarget := target
	originTarget.Generation = 1
	origin, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(originTarget)
	if _, err := store.CompleteProviderCleanupReceipt(t.Context(), current, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteProviderCleanupReceipt(t.Context(), origin, &produced); err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	if _, err := service.CleanupAuthorizationWithRecovery(t.Context(), target, kubernetesstore.ProviderCleanupRecoveryIdentity{TaskRef: target.TaskRef, Generation: 1}); err == nil {
		t.Fatal("current/origin conflict accepted")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("conflicting current/origin caused mutation")
		}
	}
}

// TestCleanupRecoveryLegacyRequiresExactTargetDigest changes target kind and API pins after seeding a
// historical receipt to ensure task/generation similarity cannot reuse a result for another immutable
// target digest.
// TestCleanupRecoveryLegacyRequiresExactTargetDigest меняет target kind и API-привязки после сохранения
// исторической квитанции, проверяя запрет использования результата другого immutable target digest по
// сходным task/generation.
func TestCleanupRecoveryLegacyRequiresExactTargetDigest(t *testing.T) {
	service, store, _, target, produced := recoveryServiceFixture(t)
	old, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(target)
	if _, err := store.CompleteProviderCleanupReceipt(t.Context(), old, &produced); err != nil {
		t.Fatal(err)
	}
	target.Kind, target.UID, target.ResourceVersion = kubernetesstore.ProviderCleanupAuthorization, "61000000-0000-4000-8000-000000000777", "777"
	result, err := service.CleanupAuthorizationWithRecovery(t.Context(), target, kubernetesstore.ProviderCleanupRecoveryIdentity{TaskRef: target.TaskRef, Generation: 1, LegacyLastGeneration: 7})
	if err == nil || result.ProducedCredential != nil {
		t.Fatal("legacy receipt crossed immutable target digest")
	}
}
