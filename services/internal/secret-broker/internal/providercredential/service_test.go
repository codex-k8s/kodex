package providercredential

import (
	"context"
	"errors"
	"testing"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

type providerCredentialStoreStub struct {
	attempt           kubernetesstore.ProviderAuthorizationAttempt
	completed         []kubernetesstore.ProviderAuthorizationAttempt
	cleanupTaskRef    string
	cleanupAccountRef string
	cleanupGeneration int64
	cleanupDescriptor kubernetesstore.ProviderCredentialDescriptor
	cleanupReceipt    string
	cleanupErr        error
	cleanupCalls      int
}

func (store *providerCredentialStoreStub) Check(context.Context) error { return nil }

func (store *providerCredentialStoreStub) CreateProviderAuthorizationAttempt(
	context.Context,
	kubernetesstore.ProviderAuthorizationAttempt,
) (kubernetesstore.ProviderAuthorizationAttempt, bool, error) {
	return kubernetesstore.ProviderAuthorizationAttempt{}, false, errors.New("unexpected create")
}

func (store *providerCredentialStoreStub) GetProviderAuthorizationAttempt(
	_ context.Context,
	_ string,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	return store.attempt, nil
}

func (store *providerCredentialStoreStub) CompleteProviderAuthorizationAttempt(
	_ context.Context,
	attempt kubernetesstore.ProviderAuthorizationAttempt,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	store.completed = append(store.completed, attempt)
	store.attempt = attempt
	return attempt, nil
}

func (store *providerCredentialStoreStub) CreateProviderCredential(
	context.Context,
	string,
	string,
	[]byte,
) (kubernetesstore.ProviderCredentialDescriptor, error) {
	return kubernetesstore.ProviderCredentialDescriptor{}, errors.New("unexpected credential create")
}

func (store *providerCredentialStoreStub) DiscardProviderAuthorizationAttempt(
	context.Context,
	string,
	string,
	string,
	string,
	string,
) error {
	return errors.New("unexpected attempt discard")
}

func (store *providerCredentialStoreStub) DiscardProviderCredential(
	context.Context,
	string,
	string,
	kubernetesstore.ProviderCredentialDescriptor,
) error {
	return errors.New("unexpected credential discard")
}

func (store *providerCredentialStoreStub) CleanupProviderCredential(
	_ context.Context,
	taskRef, accountRef string,
	leaseGeneration int64,
	descriptor kubernetesstore.ProviderCredentialDescriptor,
) (string, error) {
	store.cleanupCalls++
	store.cleanupTaskRef = taskRef
	store.cleanupAccountRef = accountRef
	store.cleanupGeneration = leaseGeneration
	store.cleanupDescriptor = descriptor
	return store.cleanupReceipt, store.cleanupErr
}

type providerAppServerStub struct{ starts int }

func (store *providerCredentialStoreStub) ReadProviderCredentialExact(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
	return nil, errors.New("unexpected credential read")
}

func (server *providerAppServerStub) ObserveModelCatalog(context.Context, []byte, string) (ModelCatalog, error) {
	return ModelCatalog{}, errors.New("unexpected model catalog observation")
}

func (server *providerAppServerStub) Check(context.Context) error { return nil }

func (server *providerAppServerStub) StartDeviceAuthorization(
	context.Context,
	string,
	string,
) (DeviceAuthorizationSession, error) {
	server.starts++
	return nil, errors.New("unexpected device authorization start")
}

func TestStartDeviceAuthorizationFailsClosedForExpiredPendingAttemptAfterRestart(t *testing.T) {
	t.Parallel()
	store := &providerCredentialStoreStub{attempt: pendingProviderAttempt(time.Now().UTC().Add(-time.Minute))}
	appServer := &providerAppServerStub{}
	service, err := New(context.Background(), store, appServer, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.StartDeviceAuthorization(context.Background(), store.attempt.AttemptRef, store.attempt.AccountRef)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expired pending start error = %v", err)
	}
	if appServer.starts != 0 || len(store.completed) != 1 {
		t.Fatalf("starts/completions = %d/%d", appServer.starts, len(store.completed))
	}
	completed := store.completed[0]
	if completed.State != "EXPIRED" || completed.SafeFailureCode != "DEVICE_AUTHORIZATION_EXPIRED" ||
		completed.VerificationURI != "" || completed.UserCode != "" || completed.Credential != nil {
		t.Fatalf("unexpected expired terminal state: %#v", completed)
	}
}

func TestCleanupProviderCredentialForwardsExactFencedTarget(t *testing.T) {
	t.Parallel()
	descriptor := kubernetesstore.ProviderCredentialDescriptor{
		SecretName:            "provider-credential-cleanup",
		SecretUID:             "61000000-0000-4000-8000-000000000002",
		SecretResourceVersion: "cleanup-7",
		ContentSHA256:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	store := &providerCredentialStoreStub{cleanupReceipt: "provider-credential-cleanup:sha256:receipt"}
	service, err := New(context.Background(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.CleanupProviderCredential(
		context.Background(), "pcct_cleanup1234", "pacc_cleanup1234", 7, descriptor,
	)
	if err != nil || receipt != store.cleanupReceipt || store.cleanupCalls != 1 ||
		store.cleanupTaskRef != "pcct_cleanup1234" || store.cleanupAccountRef != "pacc_cleanup1234" ||
		store.cleanupGeneration != 7 || store.cleanupDescriptor != descriptor {
		t.Fatalf("cleanup target was not forwarded exactly: receipt=%q store=%#v err=%v", receipt, store, err)
	}
}

func TestCleanupProviderCredentialMapsStoreValidationError(t *testing.T) {
	t.Parallel()
	store := &providerCredentialStoreStub{cleanupErr: kubernetesstore.ErrProviderCredentialCleanupInvalid}
	service, err := New(context.Background(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.CleanupProviderCredential(
		context.Background(), "pcct_short", "pacc_short", 0, kubernetesstore.ProviderCredentialDescriptor{},
	)
	if !errors.Is(err, ErrInvalidInput) || store.cleanupCalls != 1 {
		t.Fatalf("cleanup validation error was not mapped: calls=%d err=%v", store.cleanupCalls, err)
	}
}

func TestObserveDeviceAuthorizationExpiresDurablePendingAttemptAfterRestart(t *testing.T) {
	t.Parallel()
	store := &providerCredentialStoreStub{attempt: pendingProviderAttempt(time.Now().UTC().Add(-time.Minute))}
	service, err := New(context.Background(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	observed, err := service.ObserveDeviceAuthorization(context.Background(), store.attempt.MaterializerAttemptRef)
	if err != nil {
		t.Fatal(err)
	}
	if observed.State != "EXPIRED" || observed.SafeFailureCode != "DEVICE_AUTHORIZATION_EXPIRED" ||
		observed.VerificationURI != "" || observed.UserCode != "" {
		t.Fatalf("unexpected observed expiry: %#v", observed)
	}
}

func TestStartDeviceAuthorizationReusesUnexpiredCrossReplicaAttemptWithoutDuplicateWorker(t *testing.T) {
	t.Parallel()
	store := &providerCredentialStoreStub{attempt: pendingProviderAttempt(time.Now().UTC().Add(time.Minute))}
	appServer := &providerAppServerStub{}
	service, err := New(context.Background(), store, appServer, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.StartDeviceAuthorization(context.Background(), store.attempt.AttemptRef, store.attempt.AccountRef)
	if err != nil {
		t.Fatal(err)
	}
	if result.MaterializerAttemptRef != store.attempt.MaterializerAttemptRef || appServer.starts != 0 || len(store.completed) != 0 {
		t.Fatalf("unexpected cross-replica reuse: %#v starts=%d completions=%d", result, appServer.starts, len(store.completed))
	}
}

func pendingProviderAttempt(expiresAt time.Time) kubernetesstore.ProviderAuthorizationAttempt {
	return kubernetesstore.ProviderAuthorizationAttempt{
		AttemptRef: "pauth_device123", AccountRef: "pacc_device123", MaterializerAttemptRef: "pmat_device123",
		SecretUID: "uid-provider-attempt", ResourceVersion: "201", State: "PENDING",
		VerificationURI: "https://example.invalid/device", UserCode: "ABCD-EFGH", ExpiresAt: expiresAt,
	}
}
