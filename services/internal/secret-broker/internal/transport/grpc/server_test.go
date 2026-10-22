package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	secretbrokerv1 "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runtimeNamespace is the canonical namespace used by synthetic storage ports, never a live cluster target.
// runtimeNamespace — канонический namespace synthetic storage ports, не цель живого кластера.
const runtimeNamespace = "kodex-runtime"

// fakeOwner models one-use grants and configurable completion replies in memory under a mutex.
// Recorded generations/failures are observations, not real owner transactions or cryptographic authority.
// fakeOwner моделирует одноразовые grants и настраиваемые completion replies в памяти под mutex.
// Записанные generations/failures — наблюдения, не реальные owner transactions или cryptographic authority.
type fakeOwner struct {
	mu               sync.Mutex
	grants           map[string]*controlplanev1.ConsumeRuntimeSecretOperationResponse
	completion       *controlplanev1.RuntimeSecret
	completionErrors []error
	completionCalls  int
	completedGen     int64
	completedValue   *controlplanev1.RuntimeSecretMaterialization
	failedCode       controlplanev1.RuntimeSecretFailureCode
	events           *[]string
	checkErr         error
}

// Check returns the injected owner-readiness failure without performing a remote check.
// Check возвращает подставленную owner-readiness failure без удалённой проверки.
func (owner *fakeOwner) Check(context.Context) error { return owner.checkErr }

// CheckCredentialProjection satisfies the unused projection-readiness port with synthetic success.
// CheckCredentialProjection удовлетворяет неиспользуемый projection-readiness port синтетическим успехом.
func (owner *fakeOwner) CheckCredentialProjection(context.Context) error { return nil }

// ResolveRuntimeCredentialProjection rejects an unexpected path instead of materializing credentials.
// ResolveRuntimeCredentialProjection отвергает неожиданный path вместо материализации credentials.
func (owner *fakeOwner) ResolveRuntimeCredentialProjection(context.Context, *controlplanev1.ResolveRuntimeCredentialProjectionRequest) (*controlplanev1.ResolveRuntimeCredentialProjectionResponse, error) {
	return nil, errors.New("unexpected runtime credential projection")
}

// ValidateRuntimeCredentialProjection rejects an unexpected validation request without owner access.
// ValidateRuntimeCredentialProjection отвергает неожиданный validation request без обращения к owner.
func (owner *fakeOwner) ValidateRuntimeCredentialProjection(context.Context, *controlplanev1.ValidateRuntimeCredentialProjectionRequest) (bool, error) {
	return false, errors.New("unexpected runtime credential projection validation")
}

// ResolveTranscriptionCredentialProjection rejects an unexpected transcription dependency.
// ResolveTranscriptionCredentialProjection отвергает неожиданное обращение к transcription dependency.
func (owner *fakeOwner) ResolveTranscriptionCredentialProjection(context.Context, *controlplanev1.ResolveTranscriptionCredentialProjectionRequest) (*controlplanev1.ResolveTranscriptionCredentialProjectionResponse, error) {
	return nil, errors.New("unexpected transcription credential projection")
}

// Consume removes a matching synthetic grant exactly once while holding the mutex;
// absence returns PermissionDenied, without validating a signature or the returned descriptor.
// Consume единожды удаляет совпавший synthetic grant под mutex;
// отсутствие даёт PermissionDenied без проверки подписи или возвращаемого descriptor.
func (owner *fakeOwner) Consume(_ context.Context, grant string) (*controlplanev1.ConsumeRuntimeSecretOperationResponse, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	operation := owner.grants[grant]
	if operation == nil {
		return nil, status.Error(codes.PermissionDenied, "operation grant is invalid")
	}
	delete(owner.grants, grant)
	return operation, nil
}

// Complete records generation/materialization and event order, then drains injected errors before
// returning configured metadata. It intentionally does not validate the operation ref or persist a receipt.
// Complete фиксирует generation/materialization и порядок событий, затем извлекает подставленные ошибки
// перед возвратом настроенных metadata. Operation ref не проверяется, receipt не сохраняется.
func (owner *fakeOwner) Complete(_ context.Context, _ string, generation int64, materialization *controlplanev1.RuntimeSecretMaterialization) (*controlplanev1.RuntimeSecret, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.completionCalls++
	owner.completedGen, owner.completedValue = generation, materialization
	if owner.events != nil {
		*owner.events = append(*owner.events, "complete")
	}
	if len(owner.completionErrors) > 0 {
		err := owner.completionErrors[0]
		owner.completionErrors = owner.completionErrors[1:]
		return nil, err
	}
	return owner.completion, nil
}

// Fail records only the requested failure code under the mutex; it does not enforce ref/generation fencing.
// Fail записывает лишь запрошенный failure code под mutex, не обеспечивая ref/generation fencing.
func (owner *fakeOwner) Fail(_ context.Context, _ string, _ int64, code controlplanev1.RuntimeSecretFailureCode) error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.failedCode = code
	return nil
}

// fakeStore captures immutable-create input, exact-read results and cleanup ordering with injected errors.
// It has no Kubernetes client or ownership enforcement beyond the explicit checks of each stub method.
// fakeStore сохраняет immutable-create input, exact-read results и cleanup ordering с подставленными ошибками.
// Kubernetes client и ownership enforcement сверх явных проверок отдельных stub methods отсутствуют.
type fakeStore struct {
	createdEffect kubernetesstore.MaterializationEffect
	createdValue  []byte
	materialized  kubernetesstore.Materialization
	revealedValue []byte
	createErr     error
	resolveErr    error
	readErr       error
	deleteErr     error
	checkErr      error
	deleteCalls   int
	events        *[]string
}

// fakeRecovery supplies one configured health error without starting a reconciliation worker.
// fakeRecovery предоставляет одну настроенную health error без запуска reconciliation worker.
type fakeRecovery struct{ err error }

// Check returns the synthetic recovery health result and ignores the context.
// Check возвращает synthetic recovery health result, игнорируя context.
func (recovery *fakeRecovery) Check(context.Context) error { return recovery.err }

// Namespace returns the fixed synthetic canonical namespace expected by New.
// Namespace возвращает фиксированный synthetic canonical namespace, ожидаемый New.
func (store *fakeStore) Namespace() string { return runtimeNamespace }

// Check returns the injected local-store readiness error without I/O.
// Check возвращает подставленную local-store readiness error без I/O.
func (store *fakeStore) Check(context.Context) error { return store.checkErr }

// CreateImmutableForEffect records the effect and a copied input buffer, then returns configured metadata/error.
// CreateImmutableForEffect записывает effect и копию input buffer, затем возвращает настроенные metadata/error.
func (store *fakeStore) CreateImmutableForEffect(_ context.Context, effect kubernetesstore.MaterializationEffect, value []byte) (kubernetesstore.Materialization, error) {
	store.createdEffect = effect
	store.createdValue = append([]byte(nil), value...)
	return store.materialized, store.createErr
}

// ResolveExact appends a resolve event before returning the configured observation or failure.
// ResolveExact добавляет resolve event перед возвратом настроенного observation либо failure.
func (store *fakeStore) ResolveExact(context.Context, kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, error) {
	if store.events != nil {
		*store.events = append(*store.events, "resolve")
	}
	return store.materialized, store.resolveErr
}

// ReadExactValue checks only UID/resource-version/digest against the fixture materialization,
// returning the shared test buffer so caller clearing can be observed; other descriptor checks are not emulated.
// ReadExactValue сверяет лишь UID/resource-version/digest с fixture materialization,
// возвращая общий test buffer для наблюдения обнуления; прочие descriptor checks не имитируются.
func (store *fakeStore) ReadExactValue(_ context.Context, descriptor kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, []byte, error) {
	if descriptor.UID != store.materialized.UID || descriptor.ResourceVersion != store.materialized.ResourceVersion || descriptor.ContentSHA256 != store.materialized.ContentSHA256 {
		return kubernetesstore.Materialization{}, nil, kubernetesstore.ErrMaterializationConflict
	}
	return store.materialized, store.revealedValue, store.readErr
}

// DeleteExact records every attempted delete and its ordering even when the injected deletion fails.
// DeleteExact фиксирует каждую попытку delete и её порядок даже при подставленном отказе удаления.
func (store *fakeStore) DeleteExact(context.Context, kubernetesstore.Materialization) error {
	store.deleteCalls++
	if store.events != nil {
		*store.events = append(*store.events, "delete")
	}
	return store.deleteErr
}

// ReadProviderCredentialExact fails an unexpected provider-credential read in immediate-secret tests.
// ReadProviderCredentialExact отклоняет неожиданный provider-credential read в immediate-secret тестах.
func (store *fakeStore) ReadProviderCredentialExact(context.Context, string, kubernetesstore.ProviderCredentialDescriptor) ([]byte, error) {
	return nil, errors.New("unexpected provider credential read")
}

// MaterializeRuntimeCredentialProjection fails an unexpected projection write without side effects.
// MaterializeRuntimeCredentialProjection отклоняет неожиданный projection write без побочных эффектов.
func (store *fakeStore) MaterializeRuntimeCredentialProjection(context.Context, kubernetesstore.CredentialProjectionManifest) (kubernetesstore.CredentialProjection, error) {
	return kubernetesstore.CredentialProjection{}, errors.New("unexpected runtime credential projection")
}

// ListRuntimeCredentialProjections reports no synthetic projections and performs no inventory request.
// ListRuntimeCredentialProjections сообщает об отсутствии synthetic projections без inventory request.
func (store *fakeStore) ListRuntimeCredentialProjections(context.Context) ([]kubernetesstore.CredentialProjection, error) {
	return nil, nil
}

// DeleteRuntimeCredentialProjection satisfies the unused cleanup port with a no-op.
// DeleteRuntimeCredentialProjection реализует неиспользуемый cleanup port без действий.
func (store *fakeStore) DeleteRuntimeCredentialProjection(context.Context, kubernetesstore.CredentialProjection) error {
	return nil
}

// TestCreateUsesFencedEffectAndBuildsBoundedHint checks CREATE metadata, operation/generation/digest
// forwarding and the explicit rune disclosure budget of a synthetic string hint.
// TestCreateUsesFencedEffectAndBuildsBoundedHint проверяет CREATE metadata, передачу operation/generation/
// digest и явный rune disclosure budget подсказки синтетической строки.
func TestCreateUsesFencedEffectAndBuildsBoundedHint(t *testing.T) {
	t.Parallel()
	value := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz")
	operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE, "secop_create", 7, value)
	server, owner, store := mutationServerFixture(t, operation)
	response, err := server.CreateSecret(context.Background(), &secretbrokerv1.CreateSecretRequest{OperationGrant: "grant", Value: value})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetSecret().GetSecretRef() != operation.GetSecretRef() {
		t.Fatalf("CreateSecretResponse потерял metadata: %#v", response.GetSecret())
	}
	if store.createdEffect.OperationRef != operation.GetOperationRef() || store.createdEffect.ClaimGeneration != 7 ||
		store.createdEffect.ContentSHA256 != operation.GetExpectedContentSha256() || owner.completedGen != 7 {
		t.Fatalf("fenced effect was lost: effect=%#v generation=%d", store.createdEffect, owner.completedGen)
	}
	hint := owner.completedValue.GetDisplayHint()
	visible := len([]rune(hint.GetPrefix())) + len([]rune(hint.GetSuffix()))
	if visible == 0 || visible > 12 || visible > len([]rune(string(value)))*15/100 {
		t.Fatalf("display hint exceeds disclosure budget: %#v", hint)
	}
}

// TestRotateReturnsDedicatedResponse executes the ROTATE wrapper and checks its dedicated response's secret ref.
// TestRotateReturnsDedicatedResponse выполняет ROTATE wrapper и проверяет secret ref его отдельного response.
func TestRotateReturnsDedicatedResponse(t *testing.T) {
	t.Parallel()
	value := []byte("rotated-value")
	operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE, "secop_rotate", 8, value)
	server, _, _ := mutationServerFixture(t, operation)
	response, err := server.RotateSecret(context.Background(), &secretbrokerv1.RotateSecretRequest{OperationGrant: "grant", Value: value})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetSecret().GetSecretRef() != operation.GetSecretRef() {
		t.Fatalf("RotateSecretResponse потерял metadata: %#v", response.GetSecret())
	}
}

// TestDigestMismatchFailsClaimWithoutMaterialization rejects bytes differing from the consumed content hash,
// records the closed failure code and proves no create buffer reached the fake store.
// TestDigestMismatchFailsClaimWithoutMaterialization отвергает байты, отличные от consumed content hash,
// записывает закрытый failure code и проверяет отсутствие create buffer у fake store.
func TestDigestMismatchFailsClaimWithoutMaterialization(t *testing.T) {
	t.Parallel()
	operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE, "secop_digest", 3, []byte("authorized"))
	owner := successfulOwner("grant", operation)
	store := &fakeStore{materialized: materialization(operation)}
	server, _ := New(owner, store, &fakeRecovery{}, 512<<10)
	_, err := server.RotateSecret(context.Background(), &secretbrokerv1.RotateSecretRequest{OperationGrant: "grant", Value: []byte("different")})
	if status.Code(err) != codes.InvalidArgument || owner.failedCode != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID {
		t.Fatalf("digest mismatch was not failed durably: code=%s failure=%s", status.Code(err), owner.failedCode)
	}
	if store.createdValue != nil {
		t.Fatal("digest mismatch reached Kubernetes materialization")
	}
}

// TestCrashAfterCreateLeavesMaterializationForRecovery injects three completion transport errors after
// create and requires the uncertain effect to remain undeleted without a fabricated permanent failure.
// TestCrashAfterCreateLeavesMaterializationForRecovery подставляет три completion transport errors после
// create и требует сохранить неопределённый эффект без удаления и выдуманного permanent failure.
func TestCrashAfterCreateLeavesMaterializationForRecovery(t *testing.T) {
	t.Parallel()
	value := []byte("synthetic-value")
	operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE, "secop_crash", 4, value)
	owner := successfulOwner("grant", operation)
	owner.completionErrors = []error{
		status.Error(codes.Unavailable, "synthetic outage"),
		status.Error(codes.DeadlineExceeded, "synthetic timeout"),
		status.Error(codes.Unavailable, "synthetic outage"),
	}
	store := &fakeStore{materialized: materialization(operation)}
	server, _ := New(owner, store, &fakeRecovery{}, 512<<10)
	_, err := server.CreateSecret(context.Background(), &secretbrokerv1.CreateSecretRequest{OperationGrant: "grant", Value: value})
	if status.Code(err) != codes.Unavailable || owner.completionCalls != 3 || len(store.createdValue) == 0 || store.deleteCalls != 0 || owner.failedCode != 0 {
		t.Fatalf("uncertain completion was not preserved for recovery: err=%v calls=%d deletes=%d failure=%s", err, owner.completionCalls, store.deleteCalls, owner.failedCode)
	}
}

// TestStaleGenerationNeverDeletesRevokeMaterializations makes owner completion reject the fence
// and proves no exact delete follows the authoritative refusal.
// TestStaleGenerationNeverDeletesRevokeMaterializations подставляет owner completion отказ fence
// и проверяет отсутствие exact delete после authoritative refusal.
func TestStaleGenerationNeverDeletesRevokeMaterializations(t *testing.T) {
	t.Parallel()
	operation := readOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE, "secop_stale", 9)
	owner := successfulOwner("grant", operation)
	owner.completionErrors = []error{status.Error(codes.FailedPrecondition, "stale generation")}
	store := &fakeStore{materialized: materialization(operation)}
	server, _ := New(owner, store, &fakeRecovery{}, 512<<10)
	_, err := server.RevokeSecret(context.Background(), &secretbrokerv1.RevokeSecretRequest{OperationGrant: "grant"})
	if status.Code(err) != codes.FailedPrecondition || store.deleteCalls != 0 {
		t.Fatalf("stale generation crossed authoritative completion: err=%v deletes=%d", err, store.deleteCalls)
	}
}

// TestRevokeReportsIncompleteCleanupAfterDurableCompletion checks complete→resolve→delete ordering
// and retains Unavailable when deletion fails after the fake owner has acknowledged revocation.
// TestRevokeReportsIncompleteCleanupAfterDurableCompletion проверяет порядок complete→resolve→delete
// и сохраняет Unavailable при отказе удаления после подтверждения revocation fake owner.
func TestRevokeReportsIncompleteCleanupAfterDurableCompletion(t *testing.T) {
	t.Parallel()
	events := []string{}
	operation := readOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE, "secop_revoke", 2)
	owner := successfulOwner("grant", operation)
	owner.events = &events
	store := &fakeStore{materialized: materialization(operation), deleteErr: errors.New("synthetic delete outage"), events: &events}
	server, _ := New(owner, store, &fakeRecovery{}, 512<<10)
	response, err := server.RevokeSecret(context.Background(), &secretbrokerv1.RevokeSecretRequest{OperationGrant: "grant"})
	if status.Code(err) != codes.Unavailable || response != nil || owner.completionCalls != 1 {
		t.Fatalf("incomplete cleanup must remain visible after authoritative revoke: response=%#v err=%v completions=%d", response, err, owner.completionCalls)
	}
	if len(events) != 3 || events[0] != "complete" || events[1] != "resolve" || events[2] != "delete" {
		t.Fatalf("unexpected revoke effect order: %v", events)
	}
}

// TestRevealUsesExactDescriptorAndZerosAdapterBuffer checks the returned value copy and completion
// generation, then verifies every byte of the shared adapter buffer was cleared.
// TestRevealUsesExactDescriptorAndZerosAdapterBuffer проверяет возвращённую копию значения и completion
// generation, затем обнуление каждого байта общего adapter buffer.
func TestRevealUsesExactDescriptorAndZerosAdapterBuffer(t *testing.T) {
	t.Parallel()
	operation := readOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL, "secop_reveal", 6)
	owner := successfulOwner("grant", operation)
	store := &fakeStore{materialized: materialization(operation), revealedValue: []byte("synthetic-revealed-value")}
	server, _ := New(owner, store, &fakeRecovery{}, 512<<10)
	response, err := server.RevealSecret(context.Background(), &secretbrokerv1.RevealSecretRequest{OperationGrant: "grant"})
	if err != nil || string(response.GetValue()) != "synthetic-revealed-value" || owner.completedGen != 6 {
		t.Fatalf("exact reveal failed: response=%#v generation=%d err=%v", response, owner.completedGen, err)
	}
	for _, value := range store.revealedValue {
		if value != 0 {
			t.Fatal("adapter plaintext buffer was not zeroed")
		}
	}
}

// TestInvalidJSONProducesTerminalFailure uses hash-matching malformed JSON so rejection occurs at
// value validation and reports the original claim's materialization-invalid failure code.
// TestInvalidJSONProducesTerminalFailure использует hash-matching неверный JSON для отказа на
// value validation и записи materialization-invalid failure code исходного claim.
func TestInvalidJSONProducesTerminalFailure(t *testing.T) {
	t.Parallel()
	value := []byte("not-json")
	operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE, "secop_json", 1, value)
	operation.ValueType = controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_JSON
	owner := successfulOwner("grant", operation)
	server, _ := New(owner, &fakeStore{materialized: materialization(operation)}, &fakeRecovery{}, 512<<10)
	_, err := server.CreateSecret(context.Background(), &secretbrokerv1.CreateSecretRequest{OperationGrant: "grant", Value: value})
	if status.Code(err) != codes.InvalidArgument || owner.failedCode != controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID {
		t.Fatalf("invalid JSON did not produce terminal failure: err=%v failure=%s", err, owner.failedCode)
	}
}

// TestReadinessIgnoresAdjacentOwnerAndRecovery permits adjacent owner/recovery failures but
// requires an injected local Kubernetes-store failure to close broker readiness.
// TestReadinessIgnoresAdjacentOwnerAndRecovery допускает соседние owner/recovery failures,
// но требует закрыть broker readiness при подставленной local Kubernetes-store failure.
func TestReadinessIgnoresAdjacentOwnerAndRecovery(t *testing.T) {
	t.Parallel()
	owner := successfulOwner("grant", readOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL, "secop_ready", 1))
	owner.checkErr = errors.New("synthetic owner outage")
	server, _ := New(owner, &fakeStore{}, &fakeRecovery{err: errors.New("synthetic recovery backlog")}, 512<<10)
	response, err := server.CheckReadiness(context.Background(), &secretbrokerv1.CheckReadinessRequest{})
	if err != nil || !response.GetReady() {
		t.Fatalf("adjacent outage closed gRPC readiness: response=%#v err=%v", response, err)
	}
	server.store = &fakeStore{checkErr: errors.New("synthetic Kubernetes outage")}
	response, err = server.CheckReadiness(context.Background(), &secretbrokerv1.CheckReadinessRequest{})
	if status.Code(err) != codes.Unavailable || response.GetReady() {
		t.Fatalf("local infrastructure outage kept gRPC readiness open: response=%#v err=%v", response, err)
	}
}

// mutationServerFixture assembles independent fake ports and the existing server defaults for one
// caller-built operation; the test retains its concrete RPC call and expected result assertions.
// mutationServerFixture собирает отдельные fake ports и прежние server defaults для operation caller;
// конкретный RPC call и проверки ожидаемого результата остаются в тесте.
func mutationServerFixture(t *testing.T, operation *controlplanev1.ConsumeRuntimeSecretOperationResponse) (*Server, *fakeOwner, *fakeStore) {
	t.Helper()
	owner := successfulOwner("grant", operation)
	store := &fakeStore{materialized: materialization(operation)}
	server, err := New(owner, store, &fakeRecovery{}, 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	return server, owner, store
}

// TestConsumeValidatedOperationUsesOneShotGrantAndExpectedKind checks all four concrete operation
// kinds and exact response identity, then proves the fake one-use grant cannot be consumed again.
// TestConsumeValidatedOperationUsesOneShotGrantAndExpectedKind проверяет четыре конкретных operation
// kinds и точную response identity, затем повторный отказ fake одноразового grant.
func TestConsumeValidatedOperationUsesOneShotGrantAndExpectedKind(t *testing.T) {
	for _, kind := range []controlplanev1.RuntimeSecretOperationKind{
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE,
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE,
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL,
		controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE,
	} {
		t.Run(kind.String(), func(t *testing.T) {
			operation := readOperation(kind, "secop_once", 4)
			if kind == controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE || kind == controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE {
				operation = mutationOperation(kind, "secop_once", 4, []byte("synthetic value"))
			}
			server, owner, _ := mutationServerFixture(t, operation)
			got, err := server.consumeValidatedOperation(t.Context(), "grant", kind)
			if err != nil || got != operation || owner.failedCode != 0 {
				t.Fatal("consumed operation changed identity or failed its expected kind")
			}
			if got, err := server.consumeValidatedOperation(t.Context(), "grant", kind); got != nil || status.Code(err) != codes.PermissionDenied || owner.failedCode != 0 {
				t.Fatal("consumed grant was reused or its denial became a materialization failure")
			}
		})
	}
}

// TestConsumeValidatedOperationRetainsInvalidShapeFailure keeps invalid consumed metadata on the
// original fenced failure path, including the no-receipt case when its generation is missing.
// TestConsumeValidatedOperationRetainsInvalidShapeFailure сохраняет прежний fenced failure path
// неверных consumed metadata, включая отсутствие receipt при отсутствующей generation.
func TestConsumeValidatedOperationRetainsInvalidShapeFailure(t *testing.T) {
	for _, scenario := range []string{"kind", "namespace", "lease", "generation"} {
		t.Run(scenario, func(t *testing.T) {
			kind := controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE
			operation := mutationOperation(kind, "secop_invalid", 4, []byte("synthetic value"))
			switch scenario {
			case "kind":
				operation.Kind = controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE
			case "namespace":
				operation.Namespace = "foreign-namespace"
			case "lease":
				operation.LeaseDeadline = nil
			case "generation":
				operation.ClaimGeneration = 0
			}
			server, owner, _ := mutationServerFixture(t, operation)
			got, err := server.consumeValidatedOperation(t.Context(), "grant", kind)
			wantFailure := controlplanev1.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_MATERIALIZATION_INVALID
			if scenario == "generation" {
				wantFailure = 0
			}
			if got != nil || status.Code(err) != codes.FailedPrecondition || owner.failedCode != wantFailure || len(owner.grants) != 0 {
				t.Fatal("invalid consumed operation changed failure classification or claim fencing")
			}
		})
	}
}

// TestCreateSizeRefusalPrecedesGrantConsumption preserves the existing empty/oversized fast failure
// before owner consumption, durable failure reporting or store materialization.
// TestCreateSizeRefusalPrecedesGrantConsumption сохраняет ранний empty/oversized отказ до owner
// consumption, устойчивой failure reporting и store materialization.
func TestCreateSizeRefusalPrecedesGrantConsumption(t *testing.T) {
	for _, size := range []int{0, (512 << 10) + 1} {
		operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE, "secop_size", 4, []byte("authorized value"))
		server, owner, store := mutationServerFixture(t, operation)
		response, err := server.CreateSecret(t.Context(), &secretbrokerv1.CreateSecretRequest{OperationGrant: "grant", Value: make([]byte, size)})
		if response != nil || status.Code(err) != codes.InvalidArgument || len(owner.grants) != 1 || owner.failedCode != 0 || store.createdValue != nil {
			t.Fatal("size refusal consumed the grant or materialized a value")
		}
	}
}

// successfulOwner returns fresh one-use grant state and matching ACTIVE/REVOKED reply metadata;
// it is an in-memory test oracle, not an authoritative principal or persistent operation.
// successfulOwner возвращает отдельное состояние одноразового grant и совпадающие ACTIVE/REVOKED metadata;
// это in-memory test oracle, не authoritative principal или persistent operation.
func successfulOwner(grant string, operation *controlplanev1.ConsumeRuntimeSecretOperationResponse) *fakeOwner {
	state := "ACTIVE"
	if operation.GetKind() == controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE {
		state = "REVOKED"
	}
	return &fakeOwner{
		grants: map[string]*controlplanev1.ConsumeRuntimeSecretOperationResponse{grant: operation},
		completion: &controlplanev1.RuntimeSecret{Ref: operation.GetSecretRef(), ScopeKind: operation.GetScopeKind(), OrganizationRef: operation.GetOrganizationRef(), ProjectRef: operation.GetProjectRef(),
			Name: operation.GetName(), ValueType: operation.GetValueType(), State: state, CurrentRevision: operation.GetTargetRevision()},
	}
}

// mutationOperation builds a synthetic CREATE/ROTATE shape with exact value digest and an unmaterialized
// revision descriptor; its Now deadline is a syntax fixture, not proof of live lease freshness.
// mutationOperation строит synthetic CREATE/ROTATE shape с точным value digest и нематериализованным
// revision descriptor; Now deadline служит syntax fixture, не доказательством актуальности live lease.
func mutationOperation(kind controlplanev1.RuntimeSecretOperationKind, ref string, generation int64, value []byte) *controlplanev1.ConsumeRuntimeSecretOperationResponse {
	digest := sha256.Sum256(value)
	encoded := hex.EncodeToString(digest[:])
	return &controlplanev1.ConsumeRuntimeSecretOperationResponse{
		OperationRef: ref, Kind: kind, ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture", ProjectRef: "prj_test123456", SecretRef: "sec_test123456",
		Name: "TEST", ValueType: controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING,
		Namespace: runtimeNamespace, TargetRevision: 1, SecretKey: "value", ClaimGeneration: generation,
		LeaseDeadline: timestamppb.Now(), ExpectedContentSha256: encoded,
		RevisionDescriptors: []*controlplanev1.RuntimeSecretRevisionDescriptor{{
			Revision: 1, Namespace: runtimeNamespace, SecretName: "runtime-secret-test-r1", SecretKey: "value", ContentSha256: encoded,
		}},
	}
}

// readOperation supplies synthetic REVEAL/REVOKE metadata with fixed exact object pins and no new-value digest.
// readOperation задаёт synthetic REVEAL/REVOKE metadata с фиксированными exact object pins без new-value digest.
func readOperation(kind controlplanev1.RuntimeSecretOperationKind, ref string, generation int64) *controlplanev1.ConsumeRuntimeSecretOperationResponse {
	digest := sha256.Sum256([]byte("stored-value"))
	return &controlplanev1.ConsumeRuntimeSecretOperationResponse{
		OperationRef: ref, Kind: kind, ScopeKind: controlplanev1.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture", ProjectRef: "prj_test123456", SecretRef: "sec_test123456",
		Name: "TEST", ValueType: controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING,
		Namespace: runtimeNamespace, TargetRevision: 1, SecretKey: "value", ClaimGeneration: generation,
		LeaseDeadline: timestamppb.Now(),
		RevisionDescriptors: []*controlplanev1.RuntimeSecretRevisionDescriptor{{
			Revision: 1, Namespace: runtimeNamespace, SecretName: "runtime-secret-test-r1", SecretKey: "value",
			SecretUid: "uid-test", SecretResourceVersion: "9", ContentSha256: hex.EncodeToString(digest[:]),
		}},
	}
}

// materialization projects the fixture's first revision and inserts deterministic UID/version when
// not yet materialized; callers must provide that revision, and no Kubernetes object is created.
// materialization проецирует первую fixture revision и подставляет UID/version до materialization;
// caller обязан передать эту revision, Kubernetes object не создаётся.
func materialization(operation *controlplanev1.ConsumeRuntimeSecretOperationResponse) kubernetesstore.Materialization {
	descriptor := operation.GetRevisionDescriptors()[0]
	uid, resourceVersion := descriptor.GetSecretUid(), descriptor.GetSecretResourceVersion()
	if uid == "" {
		uid, resourceVersion = "uid-test", "9"
	}
	return kubernetesstore.Materialization{
		WorkKind:  kubernetesstore.WorkKindImmediate,
		Namespace: runtimeNamespace, Name: descriptor.GetSecretName(), OperationRef: operation.GetOperationRef(),
		ClaimGeneration: operation.GetClaimGeneration(), SecretRef: operation.GetSecretRef(), Key: operation.GetSecretKey(),
		Revision: descriptor.GetRevision(), UID: uid, ResourceVersion: resourceVersion, ContentSHA256: descriptor.GetContentSha256(),
	}
}
