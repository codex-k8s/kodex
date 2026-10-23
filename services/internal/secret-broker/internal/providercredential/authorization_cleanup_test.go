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

// cleanupDeviceSession provides synthetic verification metadata and configurable Wait failure, counts
// waits and signals Close once; it owns no provider process or real tokens.
// cleanupDeviceSession задаёт synthetic verification metadata и настраиваемый Wait отказ, считает waits
// и однократно сигнализирует Close; provider процессом и реальными tokens не владеет.
type cleanupDeviceSession struct {
	closed  chan struct{}
	once    sync.Once
	waits   int
	waitErr error
}

// VerificationURI returns a reserved invalid-domain device URL for fixture metadata only, never an
// endpoint contacted by the tests.
// VerificationURI возвращает device URL зарезервированного invalid-domain только для fixture metadata;
// тесты к endpoint не обращаются.
func (*cleanupDeviceSession) VerificationURI() string { return "https://example.invalid/device" }

// MaterializerAttemptRef returns a fixed synthetic session reference; durable test attempt bindings are
// constructed separately by the service/store fixture.
// MaterializerAttemptRef возвращает фиксированную synthetic session ссылку; durable test attempt
// привязки отдельно строит service/store fixture.
func (*cleanupDeviceSession) MaterializerAttemptRef() string { return "pmat_synthetic123456" }

// UserCode returns a conspicuously synthetic fixture verification code rather than reading a real
// pending login.
// UserCode возвращает явно synthetic fixture verification код без чтения реального pending login.
func (*cleanupDeviceSession) UserCode() string { return "SYNTHETIC-ONLY" }

// Wait increments the fixture counter and returns either the programmed error or fresh synthetic auth
// bytes with a masked fixture label; it performs no polling and does not implement context waiting.
// Wait увеличивает fixture счётчик и возвращает настроенную ошибку либо свежие synthetic auth байты с
// masked fixture меткой; polling и ожидание context не реализуются.
func (session *cleanupDeviceSession) Wait(context.Context) ([]byte, string, error) {
	session.waits++
	if session.waitErr != nil {
		return nil, "", session.waitErr
	}
	return []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-only"}}`), "synthetic masked account", nil
}

// TestAuthorizationWorkerClassifiesTerminalFailure runs the worker against synthetic device,
// materialization and deadline errors and verifies the corresponding durable terminal failure code.
// TestAuthorizationWorkerClassifiesTerminalFailure запускает worker с synthetic device, materialization
// и deadline ошибками и проверяет соответствующий durable terminal failure code.
func TestAuthorizationWorkerClassifiesTerminalFailure(t *testing.T) {
	t.Parallel()
	for name, fixture := range map[string]struct {
		err  error
		code string
	}{
		"device":          {err: errors.New("synthetic device failure"), code: "DEVICE_AUTHORIZATION_FAILED"},
		"materialization": {err: errors.Join(errCredentialMaterialization, errors.New("synthetic materialization failure")), code: "CREDENTIAL_MATERIALIZATION_FAILED"},
		"expired":         {err: context.DeadlineExceeded, code: "DEVICE_AUTHORIZATION_EXPIRED"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attempt := pendingProviderAttempt(time.Now().Add(time.Minute))
			store := &providerCredentialStoreStub{attempt: attempt}
			session := &cleanupDeviceSession{closed: make(chan struct{}), waitErr: fixture.err}
			service, err := New(t.Context(), store, &providerAppServerStub{}, DefaultDeviceAuthorizationTTL())
			if err != nil {
				t.Fatal(err)
			}
			worker := &deviceWorker{session: session, done: make(chan struct{}), discard: make(chan struct{})}
			service.workers.Add(1)
			service.waitForDeviceAuthorization(attempt, worker)
			if len(store.completed) != 1 || store.completed[0].State == "PENDING" ||
				store.completed[0].SafeFailureCode != fixture.code {
				t.Fatalf("unexpected terminal state: %#v", store.completed)
			}
		})
	}
}

// TestAuthorizationWorkerDoesNotStartAfterPendingFence replaces state or UID before the worker's fresh
// owner read, then requires no session Wait or completion write and a closed local done signal.
// TestAuthorizationWorkerDoesNotStartAfterPendingFence заменяет state либо UID до свежего owner чтения
// worker, затем требует отсутствия session Wait и completion записи с закрытым local done сигналом.
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

// Close closes the fixture's stopped channel once and always returns success, allowing repeated local
// stop/join paths without closing a channel twice.
// Close однократно закрывает fixture stopped channel и всегда возвращает успех, допуская повторные local
// stop/join пути без двойного закрытия channel.
func (session *cleanupDeviceSession) Close() error {
	session.once.Do(func() { close(session.closed) })
	return nil
}

// cleanupAppServer overrides only device-session start to return a supplied synthetic session while
// inheriting fail-closed defaults for unrelated app-server operations.
// cleanupAppServer подменяет только device-session start возвратом переданной synthetic сессии, сохраняя
// закрытые defaults остальных app-server операций.
type cleanupAppServer struct {
	providerAppServerStub
	session *cleanupDeviceSession
}

// StartDeviceAuthorization returns the programmed local fixture session without creating a process,
// private home or external authorization request.
// StartDeviceAuthorization возвращает настроенную local fixture сессию без процесса, private home и
// внешнего authorization запроса.
func (server *cleanupAppServer) StartDeviceAuthorization(context.Context, string, string) (DeviceAuthorizationSession, error) {
	return server.session, nil
}

// pausedMaterializationStore wraps real storage but gates credential publication between entered/release
// signals so cleanup can race a deliberately paused in-flight materializer.
// pausedMaterializationStore оборачивает реальное storage, задерживая credential публикацию между
// entered/release сигналами для гонки cleanup с намеренно остановленным in-flight materializer.
type pausedMaterializationStore struct {
	Store
	entered, release chan struct{}
}

// CreateProviderCredential signals entry, waits for release or context cancellation and only then
// forwards the exact account/attempt/bytes to the wrapped store.
// CreateProviderCredential сигнализирует вход, ждёт release либо context cancellation и лишь затем
// передаёт точные account/attempt/байты обёрнутому store.
func (store *pausedMaterializationStore) CreateProviderCredential(ctx context.Context, attempt, account string, value []byte) (kubernetesstore.ProviderCredentialDescriptor, error) {
	close(store.entered)
	select {
	case <-store.release:
	case <-ctx.Done():
		return kubernetesstore.ProviderCredentialDescriptor{}, ctx.Err()
	}
	return store.Store.CreateProviderCredential(ctx, attempt, account, value)
}

// TestCleanupAuthorizationJoinsStartedWorkerAndRecoversProducedCredential pauses publication, verifies
// cleanup cancellation cannot complete a receipt before worker join, then resumes publication and
// recovers the distinct produced target and original receipt across service restart using fake
// Kubernetes.
// TestCleanupAuthorizationJoinsStartedWorkerAndRecoversProducedCredential останавливает публикацию,
// проверяет запрет завершения cleanup квитанции до worker join при отмене, затем возобновляет публикацию
// и восстанавливает отдельную produced цель с исходной квитанцией после service restart через fake
// Kubernetes.
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
	// attemptRef and accountRef bind the synthetic producer and cleanup target to the same account/attempt.
	// attemptRef и accountRef связывают synthetic producer и cleanup цель с одним account/attempt.
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
