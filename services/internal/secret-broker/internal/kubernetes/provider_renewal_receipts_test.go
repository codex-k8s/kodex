package kubernetes

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8stesting "k8s.io/client-go/testing"
)

// renewalIdentityFixture builds a synthetic control-plane renewal identity with a valid previous
// credential descriptor and one-minute future expiry; it performs no account authorization or network
// call.
// renewalIdentityFixture строит синтетическую control-plane renewal identity с корректным предыдущим
// credential descriptor и будущим сроком на минуту; авторизация аккаунта и сетевые вызовы отсутствуют.
func renewalIdentityFixture() ProviderRenewalIdentity {
	_, previous := providerCleanupFixture(providerSecretBrokerManager)
	return ProviderRenewalIdentity{TaskRef: "pcrt_renewal123456", AccountRef: providerCleanupAccountRef,
		CredentialRef: "pcr_credential123456", RequestDigest: strings.Repeat("a", 64), ClaimantID: "control-plane-worker",
		Fence: "prf_fence123456", AccountVersion: 3, CredentialRevision: 2, Generation: 1,
		ExpiresAt: time.Now().UTC().Add(time.Minute), Previous: previous}
}

// TestProviderRenewalDurableSingleWinnerAndReadRecovery verifies sequential duplicate reservation
// refusal, read-only recovery, completion replay and absence of token bytes in the fake receipt; it is
// not a real multi-replica concurrency test.
// TestProviderRenewalDurableSingleWinnerAndReadRecovery проверяет запрет последовательного повторного
// резервирования, read-only recovery, повтор завершения и отсутствие байтов токена в fake квитанции;
// реальная межрепличная конкуренция не проверяется.
func TestProviderRenewalDurableSingleWinnerAndReadRecovery(t *testing.T) {
	store, client := newAuthorizationCleanupStore(t)
	identity := renewalIdentityFixture()
	first, created, err := store.BeginProviderCredentialRenewal(t.Context(), identity)
	if err != nil || !created || first.State != ProviderRenewalRunning {
		t.Fatal("first effect reservation failed")
	}
	second, created, err := store.BeginProviderCredentialRenewal(t.Context(), identity)
	if err != nil || created || second.UID != first.UID {
		t.Fatal("duplicate reservation authorized a second effect")
	}
	if _, found, err := store.RecoverProviderRenewalCredential(t.Context(), identity); err != nil || found {
		t.Fatal("absent result became success")
	}
	produced, err := store.CreateProviderCredential(t.Context(), identity.TaskRef, identity.AccountRef, []byte(`{"tokens":"synthetic-secret-only"}`))
	if err != nil {
		t.Fatal(err)
	}
	client.ClearActions()
	actual, found, err := store.RecoverProviderRenewalCredential(t.Context(), identity)
	if err != nil || !found || actual != produced {
		t.Fatal("immutable result was not recovered")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() != "get" {
			t.Fatal("READ recovery performed a write")
		}
	}
	completed, err := store.CompleteProviderCredentialRenewal(t.Context(), first, actual)
	if err != nil || completed.State != ProviderRenewalSucceeded || completed.Produced == nil || *completed.Produced != actual {
		t.Fatal("completion failed")
	}
	replayed, err := store.CompleteProviderCredentialRenewal(t.Context(), first, actual)
	if err != nil || replayed.ResourceVersion != completed.ResourceVersion {
		t.Fatal("completion replay changed receipt")
	}
	secret, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), providerRenewalReceiptName(identity), metav1.GetOptions{})
	if err != nil || bytes.Contains(secret.Data[providerCleanupReceiptKey], []byte("synthetic-secret")) {
		t.Fatal("receipt contains credentials")
	}
}

// TestProviderRenewalRejectsChangedIdentityAndStaleCompletion changes version, generation, prior digest,
// expiry and request digest to ensure none reuse a reservation, then rejects stale CAS pins and the
// previous credential as the new result.
// TestProviderRenewalRejectsChangedIdentityAndStaleCompletion меняет version, generation, прежний
// digest, срок и request digest, проверяя запрет повторного использования резервирования, затем
// отклоняет устаревшие CAS-привязки и прежний credential как новый результат.
func TestProviderRenewalRejectsChangedIdentityAndStaleCompletion(t *testing.T) {
	store, _ := newAuthorizationCleanupStore(t)
	identity := renewalIdentityFixture()
	first, _, err := store.BeginProviderCredentialRenewal(t.Context(), identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ProviderRenewalIdentity){
		func(i *ProviderRenewalIdentity) { i.AccountVersion++ }, func(i *ProviderRenewalIdentity) { i.Generation++ },
		func(i *ProviderRenewalIdentity) { i.Previous.ContentSHA256 = strings.Repeat("b", 64) },
		func(i *ProviderRenewalIdentity) { i.ExpiresAt = i.ExpiresAt.Add(time.Second) },
		func(i *ProviderRenewalIdentity) { i.RequestDigest = strings.Repeat("c", 64) },
	} {
		changed := identity
		change(&changed)
		if _, created, err := store.BeginProviderCredentialRenewal(t.Context(), changed); err == nil || created {
			t.Fatal("changed input reused original effect reservation")
		}
	}
	produced, err := store.CreateProviderCredential(t.Context(), identity.TaskRef, identity.AccountRef, []byte(`{"synthetic":"only"}`))
	if err != nil {
		t.Fatal(err)
	}
	stale := first
	stale.ResourceVersion = "1"
	if _, err := store.CompleteProviderCredentialRenewal(t.Context(), stale, produced); !errors.Is(err, ErrProviderRenewalConflict) {
		t.Fatal("stale receipt completed")
	}
	if _, err := store.CompleteProviderCredentialRenewal(t.Context(), first, identity.Previous); err == nil {
		t.Fatal("previous credential became renewal result")
	}
}

// TestProviderRenewalLostCreateACKDoesNotAuthorizeRetry commits a reservation to the fake tracker but
// returns an error, then verifies that readback of the existing RUNNING record never yields
// created=true.
// TestProviderRenewalLostCreateACKDoesNotAuthorizeRetry записывает резервирование в fake tracker, но
// возвращает ошибку, затем проверяет, что чтение существующей RUNNING записи не выдаёт created=true.
func TestProviderRenewalLostCreateACKDoesNotAuthorizeRetry(t *testing.T) {
	store, client := newAuthorizationCleanupStore(t)
	identity := renewalIdentityFixture()
	once := true
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if !once {
			return false, nil, nil
		}
		once = false
		secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		secret.UID, secret.ResourceVersion = types.UID("61000000-0000-4000-8000-000000000001"), "1"
		if err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), secret, secret.Namespace); err != nil {
			t.Fatal(err)
		}
		return true, nil, errors.New("synthetic unknown API outcome")
	})
	if _, created, err := store.BeginProviderCredentialRenewal(t.Context(), identity); err == nil || created {
		t.Fatal("unknown create ACK authorized external effect")
	}
	receipt, created, err := store.BeginProviderCredentialRenewal(t.Context(), identity)
	if err != nil || created || receipt.State != ProviderRenewalRunning {
		t.Fatal("lost ACK repeated effect")
	}
}

// TestProviderRenewalRejectsMalformedDurableReceipt rejects a foreign manager, trailing JSON, a
// duplicate field and an extra data key through direct canonical receipt parsing.
// TestProviderRenewalRejectsMalformedDurableReceipt отклоняет другого manager, хвост JSON, duplicate
// поле и дополнительный ключ Data через прямой канонический разбор квитанции.
func TestProviderRenewalRejectsMalformedDurableReceipt(t *testing.T) {
	for _, mutate := range []func(*corev1.Secret){
		func(s *corev1.Secret) { s.Labels[providerManagedByLabel] = "foreign" },
		func(s *corev1.Secret) {
			s.Data[providerCleanupReceiptKey] = append(s.Data[providerCleanupReceiptKey], []byte(`{}`)...)
		},
		func(s *corev1.Secret) {
			s.Data[providerCleanupReceiptKey] = append([]byte(`{"State":"RUNNING",`), s.Data[providerCleanupReceiptKey][1:]...)
		},
		func(s *corev1.Secret) { s.Data["extra"] = []byte("synthetic") },
	} {
		store, client := newAuthorizationCleanupStore(t)
		identity := renewalIdentityFixture()
		if _, _, err := store.BeginProviderCredentialRenewal(t.Context(), identity); err != nil {
			t.Fatal(err)
		}
		secret, err := client.CoreV1().Secrets(testNamespace).Get(t.Context(), providerRenewalReceiptName(identity), metav1.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		mutate(secret)
		if _, err := store.parseProviderRenewalReceipt(secret, identity); !errors.Is(err, ErrProviderRenewalConflict) {
			t.Fatal("malformed receipt accepted")
		}
	}
}
