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

const runtimeNamespace = "kodex-runtime"

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
}

func (owner *fakeOwner) Check(context.Context) error { return nil }

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

func (owner *fakeOwner) Fail(_ context.Context, _ string, _ int64, code controlplanev1.RuntimeSecretFailureCode) error {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	owner.failedCode = code
	return nil
}

type fakeStore struct {
	createdEffect kubernetesstore.MaterializationEffect
	createdValue  []byte
	materialized  kubernetesstore.Materialization
	revealedValue []byte
	createErr     error
	resolveErr    error
	readErr       error
	deleteErr     error
	deleteCalls   int
	events        *[]string
}

type fakeRecovery struct{ err error }

func (recovery *fakeRecovery) Check(context.Context) error { return recovery.err }

func (store *fakeStore) Namespace() string           { return runtimeNamespace }
func (store *fakeStore) Check(context.Context) error { return nil }
func (store *fakeStore) CreateImmutableForEffect(_ context.Context, effect kubernetesstore.MaterializationEffect, value []byte) (kubernetesstore.Materialization, error) {
	store.createdEffect = effect
	store.createdValue = append([]byte(nil), value...)
	return store.materialized, store.createErr
}
func (store *fakeStore) ResolveExact(context.Context, kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, error) {
	if store.events != nil {
		*store.events = append(*store.events, "resolve")
	}
	return store.materialized, store.resolveErr
}
func (store *fakeStore) ReadExactValue(_ context.Context, descriptor kubernetesstore.ExactDescriptor) (kubernetesstore.Materialization, []byte, error) {
	if descriptor.UID != store.materialized.UID || descriptor.ResourceVersion != store.materialized.ResourceVersion || descriptor.ContentSHA256 != store.materialized.ContentSHA256 {
		return kubernetesstore.Materialization{}, nil, kubernetesstore.ErrMaterializationConflict
	}
	return store.materialized, store.revealedValue, store.readErr
}
func (store *fakeStore) DeleteExact(context.Context, kubernetesstore.Materialization) error {
	store.deleteCalls++
	if store.events != nil {
		*store.events = append(*store.events, "delete")
	}
	return store.deleteErr
}

func TestCreateUsesFencedEffectAndBuildsBoundedHint(t *testing.T) {
	t.Parallel()
	value := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz")
	operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE, "secop_create", 7, value)
	owner := successfulOwner("grant", operation)
	store := &fakeStore{materialized: materialization(operation)}
	server, err := New(owner, store, &fakeRecovery{}, 512<<10)
	if err != nil {
		t.Fatal(err)
	}
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

func TestRotateReturnsDedicatedResponse(t *testing.T) {
	t.Parallel()
	value := []byte("rotated-value")
	operation := mutationOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_ROTATE, "secop_rotate", 8, value)
	owner := successfulOwner("grant", operation)
	store := &fakeStore{materialized: materialization(operation)}
	server, err := New(owner, store, &fakeRecovery{}, 512<<10)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.RotateSecret(context.Background(), &secretbrokerv1.RotateSecretRequest{OperationGrant: "grant", Value: value})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetSecret().GetSecretRef() != operation.GetSecretRef() {
		t.Fatalf("RotateSecretResponse потерял metadata: %#v", response.GetSecret())
	}
}

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

func TestRevokeCompletesBeforeBestEffortDelete(t *testing.T) {
	t.Parallel()
	events := []string{}
	operation := readOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE, "secop_revoke", 2)
	owner := successfulOwner("grant", operation)
	owner.events = &events
	store := &fakeStore{materialized: materialization(operation), deleteErr: errors.New("synthetic delete outage"), events: &events}
	server, _ := New(owner, store, &fakeRecovery{}, 512<<10)
	response, err := server.RevokeSecret(context.Background(), &secretbrokerv1.RevokeSecretRequest{OperationGrant: "grant"})
	if err != nil || response.GetSecret().GetStatus() != secretbrokerv1.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_REVOKED {
		t.Fatalf("authoritative revoke must succeed independently from cleanup: response=%#v err=%v", response, err)
	}
	if len(events) != 3 || events[0] != "complete" || events[1] != "resolve" || events[2] != "delete" {
		t.Fatalf("unexpected revoke effect order: %v", events)
	}
}

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

func TestReadinessIncludesRecoveryState(t *testing.T) {
	t.Parallel()
	owner := successfulOwner("grant", readOperation(controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL, "secop_ready", 1))
	server, _ := New(owner, &fakeStore{}, &fakeRecovery{err: errors.New("synthetic recovery backlog")}, 512<<10)
	response, err := server.CheckReadiness(context.Background(), &secretbrokerv1.CheckReadinessRequest{})
	if status.Code(err) != codes.Unavailable || response.GetReady() {
		t.Fatalf("recovery failure must close gRPC readiness: response=%#v err=%v", response, err)
	}
}

func successfulOwner(grant string, operation *controlplanev1.ConsumeRuntimeSecretOperationResponse) *fakeOwner {
	state := "ACTIVE"
	if operation.GetKind() == controlplanev1.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE {
		state = "REVOKED"
	}
	return &fakeOwner{
		grants: map[string]*controlplanev1.ConsumeRuntimeSecretOperationResponse{grant: operation},
		completion: &controlplanev1.RuntimeSecret{Ref: operation.GetSecretRef(), ProjectRef: operation.GetProjectRef(),
			Name: operation.GetName(), ValueType: operation.GetValueType(), State: state, CurrentRevision: operation.GetTargetRevision()},
	}
}

func mutationOperation(kind controlplanev1.RuntimeSecretOperationKind, ref string, generation int64, value []byte) *controlplanev1.ConsumeRuntimeSecretOperationResponse {
	digest := sha256.Sum256(value)
	encoded := hex.EncodeToString(digest[:])
	return &controlplanev1.ConsumeRuntimeSecretOperationResponse{
		OperationRef: ref, Kind: kind, ProjectRef: "prj_test123456", SecretRef: "sec_test123456",
		Name: "TEST", ValueType: controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING,
		Namespace: runtimeNamespace, TargetRevision: 1, SecretKey: "value", ClaimGeneration: generation,
		LeaseDeadline: timestamppb.Now(), ExpectedContentSha256: encoded,
		RevisionDescriptors: []*controlplanev1.RuntimeSecretRevisionDescriptor{{
			Revision: 1, Namespace: runtimeNamespace, SecretName: "runtime-secret-test-r1", SecretKey: "value", ContentSha256: encoded,
		}},
	}
}

func readOperation(kind controlplanev1.RuntimeSecretOperationKind, ref string, generation int64) *controlplanev1.ConsumeRuntimeSecretOperationResponse {
	digest := sha256.Sum256([]byte("stored-value"))
	return &controlplanev1.ConsumeRuntimeSecretOperationResponse{
		OperationRef: ref, Kind: kind, ProjectRef: "prj_test123456", SecretRef: "sec_test123456",
		Name: "TEST", ValueType: controlplanev1.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING,
		Namespace: runtimeNamespace, TargetRevision: 1, SecretKey: "value", ClaimGeneration: generation,
		LeaseDeadline: timestamppb.Now(),
		RevisionDescriptors: []*controlplanev1.RuntimeSecretRevisionDescriptor{{
			Revision: 1, Namespace: runtimeNamespace, SecretName: "runtime-secret-test-r1", SecretKey: "value",
			SecretUid: "uid-test", SecretResourceVersion: "9", ContentSha256: hex.EncodeToString(digest[:]),
		}},
	}
}

func materialization(operation *controlplanev1.ConsumeRuntimeSecretOperationResponse) kubernetesstore.Materialization {
	descriptor := operation.GetRevisionDescriptors()[0]
	uid, resourceVersion := descriptor.GetSecretUid(), descriptor.GetSecretResourceVersion()
	if uid == "" {
		uid, resourceVersion = "uid-test", "9"
	}
	return kubernetesstore.Materialization{
		Namespace: runtimeNamespace, Name: descriptor.GetSecretName(), OperationRef: operation.GetOperationRef(),
		ClaimGeneration: operation.GetClaimGeneration(), SecretRef: operation.GetSecretRef(), Key: operation.GetSecretKey(),
		Revision: descriptor.GetRevision(), UID: uid, ResourceVersion: resourceVersion, ContentSHA256: descriptor.GetContentSha256(),
	}
}
