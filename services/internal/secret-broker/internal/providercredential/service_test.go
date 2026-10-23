package providercredential

import (
	"context"
	"errors"
	"testing"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// providerCredentialStoreStub models one durable attempt, completion history and recorded cleanup
// arguments with programmable cleanup failure; unneeded writes fail explicitly instead of touching
// external storage.
// providerCredentialStoreStub моделирует одну durable attempt, историю завершений и записанные cleanup
// аргументы с задаваемой ошибкой; ненужные записи явно отклоняются без внешнего хранилища.
type providerCredentialStoreStub struct {
	Store
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

// Check returns synthetic readiness without inspecting Kubernetes, provider accounts or credential
// files.
// Check возвращает synthetic readiness без проверки Kubernetes, provider accounts и credential файлов.
func (store *providerCredentialStoreStub) Check(context.Context) error { return nil }

// CreateProviderAuthorizationAttempt rejects unexpected pending creation so reuse/restart tests cannot
// silently start a new durable flow.
// CreateProviderAuthorizationAttempt отклоняет неожиданное pending создание, не позволяя reuse/restart
// тестам незаметно начать новый durable flow.
func (store *providerCredentialStoreStub) CreateProviderAuthorizationAttempt(
	context.Context,
	kubernetesstore.ProviderAuthorizationAttempt,
) (kubernetesstore.ProviderAuthorizationAttempt, bool, error) {
	return kubernetesstore.ProviderAuthorizationAttempt{}, false, errors.New("unexpected create")
}

// GetProviderAuthorizationAttempt returns the configured in-memory attempt unchanged; exact argument
// validation is provided by scenario-specific fixtures where needed.
// GetProviderAuthorizationAttempt возвращает настроенную attempt в памяти без изменений; точные
// аргументы при необходимости проверяют scenario-specific fixtures.
func (store *providerCredentialStoreStub) GetProviderAuthorizationAttempt(
	_ context.Context,
	_ string,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	return store.attempt, nil
}

// CompleteProviderAuthorizationAttempt records and replaces the fixture attempt with the supplied
// terminal state without emulating real storage CAS.
// CompleteProviderAuthorizationAttempt записывает и заменяет fixture attempt переданным terminal
// состоянием без эмуляции реального storage CAS.
func (store *providerCredentialStoreStub) CompleteProviderAuthorizationAttempt(
	_ context.Context,
	attempt kubernetesstore.ProviderAuthorizationAttempt,
) (kubernetesstore.ProviderAuthorizationAttempt, error) {
	store.completed = append(store.completed, attempt)
	store.attempt = attempt
	return attempt, nil
}

// CreateProviderCredential rejects unexpected publication to expose accidental credential writes in
// tests designed for observation or failure classification.
// CreateProviderCredential отклоняет неожиданную публикацию, обнаруживая случайные credential записи в
// тестах наблюдения или классификации отказа.
func (store *providerCredentialStoreStub) CreateProviderCredential(
	context.Context,
	string,
	string,
	[]byte,
) (kubernetesstore.ProviderCredentialDescriptor, error) {
	return kubernetesstore.ProviderCredentialDescriptor{}, errors.New("unexpected credential create")
}

// DiscardProviderAuthorizationAttempt returns a fixed unexpected-call error, allowing tests to detect
// when compensation reaches the storage boundary.
// DiscardProviderAuthorizationAttempt возвращает фиксированную unexpected-call ошибку, позволяя тестам
// определить достижение storage границы компенсации.
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

// DiscardProviderCredential rejects an unplanned exact-credential compensation call without removing any
// fixture or live material.
// DiscardProviderCredential отклоняет незапланированный exact-credential compensation вызов без удаления
// fixture и живого material.
func (store *providerCredentialStoreStub) DiscardProviderCredential(
	context.Context,
	string,
	string,
	kubernetesstore.ProviderCredentialDescriptor,
) error {
	return errors.New("unexpected credential discard")
}

// CleanupProviderCredential counts calls and records every exact task/account/generation/descriptor
// argument, then returns the programmed metadata receipt or error.
// CleanupProviderCredential считает вызовы и записывает каждый точный task/account/generation/descriptor
// аргумент, затем возвращает настроенную metadata квитанцию либо ошибку.
func (store *providerCredentialStoreStub) CleanupProviderCredential(
	_ context.Context,
	taskRef, accountRef string,
	leaseGeneration int64,
	descriptor kubernetesstore.ProviderCredentialDescriptor,
) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	store.cleanupCalls++
	store.cleanupTaskRef = taskRef
	store.cleanupAccountRef = accountRef
	store.cleanupGeneration = leaseGeneration
	store.cleanupDescriptor = descriptor
	return kubernetesstore.ProviderCredentialCleanupResult{TerminalReceipt: store.cleanupReceipt}, store.cleanupErr
}

// providerAppServerStub counts unexpected device-session starts and supplies fail-closed catalog/device
// defaults for tests that should not invoke a real provider.
// providerAppServerStub считает неожиданные device-session starts и задаёт закрытые catalog/device
// defaults для тестов без обращения к реальному provider.
type providerAppServerStub struct{ starts int }

// ReadProviderCredentialExact rejects an unconfigured credential read; catalog and renewal fixtures
// override it only with synthetic material.
// ReadProviderCredentialExact отклоняет ненастроенное credential чтение; catalog и renewal fixtures
// подменяют его только синтетическим material.
func (store *providerCredentialStoreStub) ReadProviderCredentialExact(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
	return nil, errors.New("unexpected credential read")
}

// ObserveModelCatalog rejects unexpected catalog access without launching an executable or contacting
// HTTP endpoints.
// ObserveModelCatalog отклоняет неожиданный доступ к каталогу без executable и обращений к HTTP
// endpoints.
func (server *providerAppServerStub) ObserveModelCatalog(context.Context, []byte, string) (ModelCatalog, error) {
	return ModelCatalog{}, errors.New("unexpected model catalog observation")
}

// Check reports fixture readiness without version probes or process creation.
// Check сообщает fixture readiness без version probes и создания процесса.
func (server *providerAppServerStub) Check(context.Context) error { return nil }

// StartDeviceAuthorization increments the start counter and returns a fixed error, proving reuse/expiry
// paths do not launch an unnecessary authorization session.
// StartDeviceAuthorization увеличивает счётчик starts и возвращает фиксированную ошибку, позволяя
// проверить отсутствие лишней authorization сессии на reuse/expiry путях.
func (server *providerAppServerStub) StartDeviceAuthorization(
	context.Context,
	string,
	string,
) (DeviceAuthorizationSession, error) {
	server.starts++
	return nil, errors.New("unexpected device authorization start")
}

// TestStartDeviceAuthorizationFailsClosedForExpiredPendingAttemptAfterRestart reuses a past-expiry
// durable fixture after service recreation and requires EXPIRED completion with cleared verification
// fields and no provider start.
// TestStartDeviceAuthorizationFailsClosedForExpiredPendingAttemptAfterRestart использует durable fixture
// с прошедшим сроком после создания сервиса и требует EXPIRED завершения с очищенными verification
// полями без provider start.
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

// TestCleanupProviderCredentialForwardsExactFencedTarget verifies one storage call with unchanged task,
// account, generation and descriptor and returns the exact programmed receipt.
// TestCleanupProviderCredentialForwardsExactFencedTarget проверяет один storage вызов с неизменными
// task, account, generation и descriptor и возврат точной настроенной квитанции.
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
	if err != nil || receipt.TerminalReceipt != store.cleanupReceipt || store.cleanupCalls != 1 ||
		store.cleanupTaskRef != "pcct_cleanup1234" || store.cleanupAccountRef != "pacc_cleanup1234" ||
		store.cleanupGeneration != 7 || store.cleanupDescriptor != descriptor {
		t.Fatalf("cleanup target was not forwarded exactly: receipt=%#v store=%#v err=%v", receipt, store, err)
	}
}

// TestCleanupProviderCredentialMapsStoreValidationError verifies the storage invalid-cleanup sentinel
// becomes service ErrInvalidInput without bypassing the store's authoritative input validation.
// TestCleanupProviderCredentialMapsStoreValidationError проверяет преобразование storage invalid-cleanup
// sentinel в service ErrInvalidInput без обхода авторитетной storage проверки входа.
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

// TestObserveDeviceAuthorizationExpiresDurablePendingAttemptAfterRestart verifies observation persists
// EXPIRED for an elapsed pending record and clears user-code/verification metadata without a provider
// session.
// TestObserveDeviceAuthorizationExpiresDurablePendingAttemptAfterRestart проверяет сохранение EXPIRED
// при наблюдении истёкшей pending записи и очистку user-code/verification metadata без provider сессии.
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

// TestStartDeviceAuthorizationReusesUnexpiredCrossReplicaAttemptWithoutDuplicateWorker models an
// existing durable record absent from local tracking and verifies metadata reuse with no native start or
// extra terminal write; no real replicas are launched.
// TestStartDeviceAuthorizationReusesUnexpiredCrossReplicaAttemptWithoutDuplicateWorker моделирует
// существующую durable запись без local учёта и проверяет metadata reuse без native start и
// дополнительной terminal записи; реальные реплики не запускаются.
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

// pendingProviderAttempt builds synthetic pending verification metadata with caller-chosen expiry and
// fixed API pins for restart/reuse tests, without creating a Kubernetes object.
// pendingProviderAttempt строит synthetic pending verification metadata с выбранным сроком и
// фиксированными API-привязками для restart/reuse тестов без создания Kubernetes объекта.
func pendingProviderAttempt(expiresAt time.Time) kubernetesstore.ProviderAuthorizationAttempt {
	return kubernetesstore.ProviderAuthorizationAttempt{
		AttemptRef: "pauth_device123", AccountRef: "pacc_device123", MaterializerAttemptRef: "pmat_device123",
		SecretUID: "uid-provider-attempt", ResourceVersion: "201", State: "PENDING",
		VerificationURI: "https://example.invalid/device", UserCode: "ABCD-EFGH", ExpiresAt: expiresAt,
	}
}

// TestStopAndJoinDeviceWorkerAbsentPreservesCallerSemantics permits an absent registration even with
// a canceled context, leaving the following authoritative storage call responsible for that context.
// TestStopAndJoinDeviceWorkerAbsentPreservesCallerSemantics допускает отсутствие регистрации даже
// при отменённом context, сохраняя ответственность следующего авторитетного storage вызова за context.
func TestStopAndJoinDeviceWorkerAbsentPreservesCallerSemantics(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	service := &Service{}
	if err := service.stopAndJoinDeviceWorker(ctx, "pmat_absent_fixture"); err != nil {
		t.Fatal("absent worker changed caller cancellation semantics")
	}
}

// TestStopAndJoinDeviceWorkerWaitsAndReplays checks discard-before-close, waiting for the local done
// signal, idempotent signal replay and preservation of the worker registration using synthetic channels.
// TestStopAndJoinDeviceWorkerWaitsAndReplays проверяет discard до close, ожидание local done,
// идемпотентный повтор сигнала и сохранение worker регистрации через синтетические channels.
func TestStopAndJoinDeviceWorkerWaitsAndReplays(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	session := &cleanupDeviceSession{closed: make(chan struct{})}
	worker := &deviceWorker{session: session, done: make(chan struct{}), discard: make(chan struct{})}
	service := &Service{sessions: map[string]*deviceWorker{"pmat_join_fixture": worker}}
	result := make(chan error, 1)
	go func() { result <- service.stopAndJoinDeviceWorker(ctx, "pmat_join_fixture") }()
	select {
	case <-session.closed:
	case <-ctx.Done():
		t.Fatal("local session did not close")
	}
	select {
	case <-worker.discard:
	default:
		t.Fatal("session closed before discard signal")
	}
	select {
	case <-result:
		t.Fatal("local join finished before worker completion")
	default:
	}
	close(worker.done)
	if err := <-result; err != nil {
		t.Fatal("completed local worker did not join")
	}
	if err := service.stopAndJoinDeviceWorker(ctx, "pmat_join_fixture"); err != nil {
		t.Fatal("completed local join was not replayable")
	}
	if service.sessions["pmat_join_fixture"] != worker {
		t.Fatal("local helper removed the worker registration")
	}
}

// TestStopAndJoinDeviceWorkerCancellationStillSignalsStop requires session close and discard before
// returning caller cancellation, without inventing local completion or deleting the registration.
// TestStopAndJoinDeviceWorkerCancellationStillSignalsStop требует session close и discard до возврата
// caller cancellation без выдуманного local completion и удаления регистрации.
func TestStopAndJoinDeviceWorkerCancellationStillSignalsStop(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	session := &cleanupDeviceSession{closed: make(chan struct{})}
	worker := &deviceWorker{session: session, done: make(chan struct{}), discard: make(chan struct{})}
	service := &Service{sessions: map[string]*deviceWorker{"pmat_cancel_fixture": worker}}
	if err := service.stopAndJoinDeviceWorker(ctx, "pmat_cancel_fixture"); !errors.Is(err, context.Canceled) {
		t.Fatal("unfinished local join lost caller cancellation")
	}
	select {
	case <-session.closed:
	default:
		t.Fatal("canceled join failed to close its session")
	}
	select {
	case <-worker.discard:
	default:
		t.Fatal("canceled join failed to signal discard")
	}
	select {
	case <-worker.done:
		t.Fatal("canceled helper claimed worker completion")
	default:
	}
	if service.sessions["pmat_cancel_fixture"] != worker {
		t.Fatal("canceled helper removed the worker registration")
	}
}

// TestDiscardJoinsDeviceWorkerBeforeCompensation keeps the local worker unfinished after session close
// and verifies that Discard reaches its programmed storage compensation only after the done signal.
// TestDiscardJoinsDeviceWorkerBeforeCompensation сохраняет local worker незавершённым после session
// close и проверяет достижение запрограммированной storage компенсации Discard только после done.
func TestDiscardJoinsDeviceWorkerBeforeCompensation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	session := &cleanupDeviceSession{closed: make(chan struct{})}
	worker := &deviceWorker{session: session, done: make(chan struct{}), discard: make(chan struct{})}
	service := &Service{store: &providerCredentialStoreStub{}, sessions: map[string]*deviceWorker{"pmat_discard_fixture": worker}}
	target := DiscardMaterialization{AttemptRef: "pauth_discard_fixture", AccountRef: "pacc_discard_fixture",
		MaterializerAttemptRef: "pmat_discard_fixture", MaterializerAttemptUID: "fixture-uid", MaterializerAttemptVersion: "1"}
	result := make(chan error, 1)
	go func() { result <- service.Discard(ctx, target) }()
	select {
	case <-session.closed:
	case <-ctx.Done():
		t.Fatal("discard did not stop its local session")
	}
	select {
	case <-result:
		t.Fatal("discard reached compensation before local completion")
	default:
	}
	close(worker.done)
	if err := <-result; err == nil || err.Error() != "unexpected attempt discard" {
		t.Fatal("joined discard did not reach the programmed storage boundary")
	}
}
