package providercredential

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type cleanupDeviceSession struct {
	closed chan struct{}
	once   sync.Once
	waits  int
}

func (*cleanupDeviceSession) VerificationURI() string        { return "https://example.invalid/device" }
func (*cleanupDeviceSession) MaterializerAttemptRef() string { return "pmat_synthetic123456" }
func (*cleanupDeviceSession) UserCode() string               { return "SYNTHETIC-ONLY" }
func (session *cleanupDeviceSession) Wait(context.Context) ([]byte, string, error) {
	session.waits++
	return []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-only"}}`), "synthetic masked account", nil
}

func TestAuthorizationWorkerDoesNotStartAfterPendingFence(t *testing.T) {
	t.Parallel()
	attempt := pendingProviderAttempt(time.Now().Add(time.Minute))
	attempt.SecretUID = "61000000-0000-4000-8000-000000000001"
	for _, change := range []func(*kubernetesstore.ProviderAuthorizationAttempt){
		func(v *kubernetesstore.ProviderAuthorizationAttempt) { v.State = "FAILED" },
		func(v *kubernetesstore.ProviderAuthorizationAttempt) {
			v.SecretUID = "61000000-0000-4000-8000-000000000002"
		},
	} {
		current := attempt
		change(&current)
		store := &providerCredentialStoreStub{attempt: current}
		session := &cleanupDeviceSession{closed: make(chan struct{})}
		service, err := New(t.Context(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
		if err != nil {
			t.Fatal(err)
		}
		worker := &deviceWorker{session: session, done: make(chan struct{}), discard: make(chan struct{})}
		service.workers.Add(1)
		service.waitForDeviceAuthorization(attempt, worker)
		if session.waits != 0 || len(store.completed) != 0 {
			t.Fatal("fenced/replaced pending attempt reached provider polling or completion")
		}
		select {
		case <-worker.done:
		default:
			t.Fatal("rejected worker did not join")
		}
	}
}
func (session *cleanupDeviceSession) Close() error {
	session.once.Do(func() { close(session.closed) })
	return nil
}

type cleanupAppServer struct {
	providerAppServerStub
	session *cleanupDeviceSession
}

func (server *cleanupAppServer) StartDeviceAuthorization(context.Context, string, string) (DeviceAuthorizationSession, error) {
	return server.session, nil
}

type pausedMaterializationStore struct {
	Store
	entered, release chan struct{}
}

func (store *pausedMaterializationStore) CreateProviderCredential(ctx context.Context, attempt, account string, value []byte) (kubernetesstore.ProviderCredentialDescriptor, error) {
	close(store.entered)
	select {
	case <-store.release:
	case <-ctx.Done():
		return kubernetesstore.ProviderCredentialDescriptor{}, ctx.Err()
	}
	return store.Store.CreateProviderCredential(ctx, attempt, account, value)
}

func TestCleanupAuthorizationJoinsStartedWorkerAndRecoversProducedCredential(t *testing.T) {
	t.Parallel()
	client := fake.NewSimpleClientset()
	var revision int64 = 100
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		secret := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		revision++
		secret.UID, secret.ResourceVersion = types.UID(fmt.Sprintf("61000000-0000-4000-8000-%012d", revision)), strconv.FormatInt(revision, 10)
		err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), secret, secret.Namespace)
		return true, secret, err
	})
	realStore, err := kubernetesstore.New(client, "kodex-runtime")
	if err != nil {
		t.Fatal(err)
	}
	store := &pausedMaterializationStore{Store: realStore, entered: make(chan struct{}), release: make(chan struct{})}
	session := &cleanupDeviceSession{closed: make(chan struct{})}
	service, err := New(t.Context(), store, &cleanupAppServer{session: session}, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(store.release) }); _ = service.Close() })
	const attemptRef, accountRef = "pauth_cleanup123456", "pacc_cleanup123456"
	started, err := service.StartDeviceAuthorization(t.Context(), attemptRef, accountRef)
	if err != nil {
		t.Fatal(err)
	}
	<-store.entered
	service.mu.Lock()
	worker := service.sessions[started.MaterializerAttemptRef]
	service.mu.Unlock()
	target := kubernetesstore.ProviderAuthorizationCleanupTarget{TaskRef: "pcct_cleanup123456", AccountRef: accountRef,
		AuthorizationAttemptRef: attemptRef, MaterializerAttemptRef: started.MaterializerAttemptRef, Generation: 1,
		Kind: kubernetesstore.ProviderCleanupAuthorization, UID: started.MaterializerAttemptUID, ResourceVersion: started.MaterializerAttemptVersion}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := service.CleanupAuthorization(ctx, target)
		done <- err
	}()
	<-session.closed
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup completed before local worker joined: %v", err)
	}
	identity, _ := kubernetesstore.AuthorizationCleanupReceiptIdentity(target)
	if _, found, err := realStore.ReadProviderCleanupReceipt(t.Context(), identity); err != nil || found {
		t.Fatal("unfinished local join acquired terminal receipt")
	}
	release.Do(func() { close(store.release) })
	<-worker.done
	// Повтор после cancellation использует durable pending fence и обнаруживает
	// credential, созданный уже начатым materializer, отдельным exact target.
	result, err := service.CleanupAuthorization(t.Context(), target)
	if err != nil || result.TerminalReceipt == "" || result.ProducedCredential == nil {
		t.Fatalf("cleanup lost credential produced during local cancellation: %v", err)
	}
	cleaned, err := service.CleanupProviderCredential(t.Context(), "pcct_produced123456", accountRef, 1, *result.ProducedCredential)
	if err != nil || cleaned.ProducedCredential != nil || cleaned.TerminalReceipt == "" {
		t.Fatalf("produced credential cleanup failed: %v", err)
	}
	restarted, err := New(t.Context(), realStore, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
	if err != nil {
		t.Fatal(err)
	}
	replay, err := restarted.CleanupAuthorization(t.Context(), target)
	if err != nil || replay.TerminalReceipt != result.TerminalReceipt || replay.ProducedCredential == nil || *replay.ProducedCredential != *result.ProducedCredential {
		t.Fatal("restarted service changed original cleanup receipt")
	}
}
