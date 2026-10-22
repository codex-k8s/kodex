package draftowner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/secretdrafts"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ownerStub embeds unused RPC methods and supplies programmable metadata/errors while recording the last
// request; it performs no real grant validation or owner persistence.
// ownerStub встраивает неиспользуемые RPC methods и предоставляет настраиваемые metadata/errors, записывая
// last request; реальной grant validation и owner persistence нет.
type ownerStub struct {
	cp.RuntimeSecretDraftWorkServiceClient
	work             *cp.RuntimeSecretDraftWork
	draft            *cp.RuntimeSecretDraft
	secret           *cp.RuntimeSecret
	failure          error
	request          proto.Message
	pages            []*cp.ListRuntimeSecretDraftRecoveryWorkResponse
	page             int
	ready, completed bool
	action           cp.RuntimeSecretRecoveryAction
}

// CheckRuntimeSecretDraftWorkReadiness records the request and returns configured Ready/failure without
// checking context.
// CheckRuntimeSecretDraftWorkReadiness записывает request и возвращает настроенные Ready/failure без
// проверки context.
func (s *ownerStub) CheckRuntimeSecretDraftWorkReadiness(_ context.Context, r *cp.CheckRuntimeSecretDraftWorkReadinessRequest, _ ...grpc.CallOption) (*cp.CheckRuntimeSecretDraftWorkReadinessResponse, error) {
	s.request = r
	return &cp.CheckRuntimeSecretDraftWorkReadinessResponse{Ready: s.ready}, s.failure
}

// ConsumeRuntimeSecretDraftOperation records the grant request and returns configured work/failure without
// consuming authority.
// ConsumeRuntimeSecretDraftOperation записывает grant request и возвращает настроенные work/failure без
// потребления authority.
func (s *ownerStub) ConsumeRuntimeSecretDraftOperation(_ context.Context, r *cp.ConsumeRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.ConsumeRuntimeSecretDraftOperationResponse, error) {
	s.request = r
	return &cp.ConsumeRuntimeSecretDraftOperationResponse{Work: s.work}, s.failure
}

// CompleteRuntimeSecretDraftOperation records the supplied exact descriptors and returns configured
// draft/Secret/failure without committing a transition.
// CompleteRuntimeSecretDraftOperation записывает supplied exact descriptors и возвращает настроенные
// draft/Secret/failure без фиксации transition.
func (s *ownerStub) CompleteRuntimeSecretDraftOperation(_ context.Context, r *cp.CompleteRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.CompleteRuntimeSecretDraftOperationResponse, error) {
	s.request = r
	return &cp.CompleteRuntimeSecretDraftOperationResponse{Draft: s.draft, Secret: s.secret}, s.failure
}

// FailRuntimeSecretDraftOperation records the request and returns configured draft with FAILED operation
// state, without validating the claim.
// FailRuntimeSecretDraftOperation записывает request и возвращает настроенный draft с FAILED operation
// state без проверки claim.
func (s *ownerStub) FailRuntimeSecretDraftOperation(_ context.Context, r *cp.FailRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.FailRuntimeSecretDraftOperationResponse, error) {
	s.request = r
	return &cp.FailRuntimeSecretDraftOperationResponse{Draft: s.draft, State: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED}, s.failure
}

// ListRuntimeSecretDraftRecoveryWork records each request and serves configured pages, repeating the last
// when exhausted; configured errors or absent pages return no valid page.
// ListRuntimeSecretDraftRecoveryWork записывает каждый request и выдаёт настроенные pages, повторяя
// последнюю после исчерпания; настроенная ошибка либо отсутствие pages не дают valid page.
func (s *ownerStub) ListRuntimeSecretDraftRecoveryWork(_ context.Context, r *cp.ListRuntimeSecretDraftRecoveryWorkRequest, _ ...grpc.CallOption) (*cp.ListRuntimeSecretDraftRecoveryWorkResponse, error) {
	s.request = r
	if s.failure != nil {
		return nil, s.failure
	}
	if len(s.pages) == 0 {
		return nil, nil
	}
	p := s.pages[min(s.page, len(s.pages)-1)]
	s.page++
	return p, nil
}

// RecoverRuntimeSecretDraftMaterialization records the request and returns configured draft with FAILED
// state and the same configured action for both effects.
// RecoverRuntimeSecretDraftMaterialization записывает request и возвращает настроенный draft с FAILED
// state и одним настроенным action для обоих effects.
func (s *ownerStub) RecoverRuntimeSecretDraftMaterialization(_ context.Context, r *cp.RecoverRuntimeSecretDraftMaterializationRequest, _ ...grpc.CallOption) (*cp.RecoverRuntimeSecretDraftMaterializationResponse, error) {
	s.request = r
	return &cp.RecoverRuntimeSecretDraftMaterializationResponse{Draft: s.draft, OperationState: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED, EncryptedAction: s.action, MaterializationAction: s.action}, s.failure
}

// CompleteRuntimeSecretDraftCleanup records acknowledgement input and returns configured Completed/failure
// without deleting objects.
// CompleteRuntimeSecretDraftCleanup записывает acknowledgement input и возвращает настроенные
// Completed/failure без удаления объектов.
func (s *ownerStub) CompleteRuntimeSecretDraftCleanup(_ context.Context, r *cp.CompleteRuntimeSecretDraftCleanupRequest, _ ...grpc.CallOption) (*cp.CompleteRuntimeSecretDraftCleanupResponse, error) {
	s.request = r
	return &cp.CompleteRuntimeSecretDraftCleanupResponse{Completed: s.completed}, s.failure
}

// workFixture builds fixed historical scoped SAVE metadata and canonical staging name; timestamps are
// structurally valid, not a promise of an active current lease.
// workFixture создаёт fixed historical scoped SAVE metadata и canonical staging name; timestamps корректны
// по форме, не гарантируют active current lease.
func workFixture() *cp.RuntimeSecretDraftWork {
	tm := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	ref := "draft_fixture"
	sum := sha256.Sum256([]byte(ref))
	return &cp.RuntimeSecretDraftWork{OperationRef: "operation_fixture", Kind: cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_SAVE, Draft: &cp.RuntimeSecretDraft{Ref: ref, Version: 1, Generation: 1, SecretVersion: 1, ScopeKind: cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, OrganizationRef: "org_fixture", ProjectRef: "project_fixture", SecretRef: "sec_fixture01", Name: "SYNTHETIC_KEY", ValueType: cp.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, State: cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PREPARING, CreatedAt: timestamppb.New(tm), UpdatedAt: timestamppb.New(tm), ExpiresAt: timestamppb.New(tm.Add(time.Hour))}, ExpectedContentSha256: strings.Repeat("a", 64), Namespace: "kodex-runtime", StagedNamespace: "kodex-system", StagedSecretName: "runtime-secret-draft-" + hex.EncodeToString(sum[:16]), StagedSecretKey: "ciphertext", ClaimantId: "pod_fixture", ClaimGeneration: 3, LeaseDeadline: timestamppb.New(tm.Add(time.Minute)), ExpiresAt: timestamppb.New(tm.Add(time.Minute))}
}

// nativeFixture consumes synthetic work through the real adapter, then clears the last-request recorder
// for assertions without connecting to a server.
// nativeFixture потребляет synthetic work через реальный adapter и сбрасывает last-request recorder для
// assertions без подключения к серверу.
func nativeFixture(t *testing.T) (value.DraftWork, *ownerStub, *Owner) {
	t.Helper()
	stub := &ownerStub{work: workFixture(), ready: true, completed: true, action: cp.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_KEEP}
	owner, err := New(stub, "pod_fixture")
	if err != nil {
		t.Fatal(err)
	}
	work, err := owner.Consume(context.Background(), "synthetic-grant")
	if err != nil {
		t.Fatal(err)
	}
	stub.request = nil
	return work, stub, owner
}

// encryptedFixture returns matching staging identity/digest/key-shape pins without storing or encrypting
// bytes.
// encryptedFixture возвращает matching staging identity/digest/key-shape pins без сохранения и шифрования
// байтов.
func encryptedFixture(w value.DraftWork) value.DraftEncryptedDescriptor {
	return value.DraftEncryptedDescriptor{Namespace: w.StagedNamespace, Name: w.StagedName, DataKey: w.StagedKey, UID: "encrypted-uid", ResourceVersion: "123", CiphertextSHA256: strings.Repeat("b", 64), EncryptionKey: value.DraftEncryptionKey{ID: strings.Repeat("c", 64), Generation: 1}}
}

// draftResult clones the fixture draft, increments its version and selects a response state without
// mutating the source work.
// draftResult копирует fixture draft, увеличивает version и задаёт response state без изменения source
// work.
func draftResult(s *ownerStub, state cp.RuntimeSecretDraftState) *cp.RuntimeSecretDraft {
	d := proto.Clone(s.work.Draft).(*cp.RuntimeSecretDraft)
	d.Version++
	d.State = state
	return d
}

// TestOwnerConsumesExactClaimAndCompletesWithoutPrivateFields checks readiness, configured claimant/grant
// forwarding, native metadata, SAVE completion descriptors and fixed failure-code/fence forwarding.
// TestOwnerConsumesExactClaimAndCompletesWithoutPrivateFields проверяет readiness, configured
// claimant/grant forwarding, native metadata, SAVE completion descriptors и передачу fixed
// failure-code/fence.
func TestOwnerConsumesExactClaimAndCompletesWithoutPrivateFields(t *testing.T) {
	w, s, o := nativeFixture(t)
	if err := o.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	w, err := o.Consume(context.Background(), "synthetic-grant")
	if err != nil {
		t.Fatal(err)
	}
	q := s.request.(*cp.ConsumeRuntimeSecretDraftOperationRequest)
	if q.GetClaimantId() != "pod_fixture" || q.GetOperationGrant() != "synthetic-grant" || w.Binding.ContentSHA256 != s.work.ExpectedContentSha256 || w.StagedNamespace != "kodex-system" || w.RuntimeNamespace != "kodex-runtime" {
		t.Fatal("consume lost owner context")
	}
	e := encryptedFixture(w)
	s.draft = draftResult(s, cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT)
	result, err := o.Complete(context.Background(), w, &e, nil)
	if err != nil || result.Draft.State != "DRAFT" || result.Secret != nil {
		t.Fatal("save completion rejected")
	}
	r := s.request.(*cp.CompleteRuntimeSecretDraftOperationRequest)
	if r.GetOperationRef() != w.OperationRef || r.GetClaimantId() != w.ClaimantID || r.GetClaimGeneration() != 3 || r.GetEncrypted().GetSecretUid() != e.UID || r.GetEncrypted().GetEncryptionKeyGeneration() != 1 || r.GetMaterialization() != nil {
		t.Fatal("complete lost exact descriptor")
	}
	s.draft = draftResult(s, cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_FAILED)
	if err := o.Fail(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if r := s.request.(*cp.FailRuntimeSecretDraftOperationRequest); r.GetFailureCode() != cp.RuntimeSecretFailureCode_RUNTIME_SECRET_FAILURE_CODE_RECONCILIATION_FAILED || r.GetClaimGeneration() != 3 {
		t.Fatal("failure fence changed")
	}
}

// TestOwnerPublishReadbackAndFencedMaterialization validates published metadata/exact descriptors without
// a display hint and rejects mismatched secret version or stale revision using stub replies, not storage
// readback.
// TestOwnerPublishReadbackAndFencedMaterialization проверяет published metadata/exact descriptors без
// display hint и отклоняет mismatched secret version либо stale revision по stub replies, не storage
// readback.
func TestOwnerPublishReadbackAndFencedMaterialization(t *testing.T) {
	w, s, o := nativeFixture(t)
	w.Kind = value.DraftPublish
	w.TargetRevision = 4
	e := encryptedFixture(w)
	name, _ := runtimesecret.VersionedKubernetesName(w.Draft.SecretRef, 4)
	m := value.DraftMaterialization{Namespace: w.RuntimeNamespace, Name: name, DataKey: "value", UID: "runtime-uid", ResourceVersion: "456", Revision: 4, ContentSHA256: w.Binding.ContentSHA256}
	s.draft = draftResult(s, cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED)
	s.draft.PublishedRevision = 4
	s.draft.SecretVersion = 2
	s.secret = &cp.RuntimeSecret{Ref: w.Draft.SecretRef, ScopeKind: s.work.Draft.ScopeKind, OrganizationRef: w.Draft.OrganizationRef, ProjectRef: w.Draft.ProjectRef, Name: w.Draft.Name, ValueType: s.work.Draft.ValueType, State: "ACTIVE", Version: 2, CurrentRevision: 4, CreatedAt: s.work.Draft.CreatedAt, UpdatedAt: s.work.Draft.UpdatedAt, DisplayHint: &cp.RuntimeSecretDisplayHint{Prefix: "not-returned"}}
	result, err := o.Complete(context.Background(), w, &e, &m)
	if err != nil || result.Secret == nil || result.Secret.Revision != 4 {
		t.Fatalf("publish: %v", err)
	}
	r := s.request.(*cp.CompleteRuntimeSecretDraftOperationRequest)
	if r.GetMaterialization().GetSecretUid() != m.UID || r.GetMaterialization().GetDisplayHint() != nil {
		t.Fatal("runtime descriptor/hint changed")
	}
	s.secret.Version = 1
	if _, err := o.Complete(context.Background(), w, &e, &m); !errors.Is(err, secretdrafts.ErrConflict) {
		t.Fatal("mismatched published secret version accepted")
	}
	s.secret.Version = 2
	s.secret.CurrentRevision = 3
	if _, err := o.Complete(context.Background(), w, &e, &m); !errors.Is(err, secretdrafts.ErrConflict) {
		t.Fatal("stale published revision accepted")
	}
}

// TestOwnerRecoveryZeroTupleAndLostCleanupACK accepts prepared zero-claim recovery, rejects negative ACK,
// preserves exact recovery descriptors and refuses a runtime effect attached to unclaimed work.
// TestOwnerRecoveryZeroTupleAndLostCleanupACK допускает prepared zero-claim recovery, отклоняет negative
// ACK, сохраняет exact recovery descriptors и запрещает runtime effect в unclaimed work.
func TestOwnerRecoveryZeroTupleAndLostCleanupACK(t *testing.T) {
	w, s, o := nativeFixture(t)
	s.work.ClaimantId = ""
	s.work.ClaimGeneration = 0
	s.work.LeaseDeadline = nil
	s.pages = []*cp.ListRuntimeSecretDraftRecoveryWorkResponse{{Operations: []*cp.RuntimeSecretDraftWork{s.work}}}
	items, err := o.ListRecovery(context.Background())
	if err != nil || len(items) != 1 || items[0].ClaimGeneration != 0 || items[0].ClaimantID != "" || !items[0].LeaseDeadline.IsZero() {
		t.Fatal("prepared recovery fence changed")
	}
	w = items[0]
	s.draft = draftResult(s, cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_FAILED)
	s.action = cp.RuntimeSecretRecoveryAction_RUNTIME_SECRET_RECOVERY_ACTION_DELETE
	if decision, err := o.Recover(context.Background(), w, nil, nil); err != nil || decision.EncryptedAction != value.DraftRecoveryDelete {
		t.Fatal("prepared recovery rejected")
	}
	if r := s.request.(*cp.RecoverRuntimeSecretDraftMaterializationRequest); r.GetClaimantId() != "" || r.GetClaimGeneration() != 0 {
		t.Fatal("recovery promoted zero fence")
	}
	if err := o.CompleteCleanup(context.Background(), w, nil, nil); err != nil {
		t.Fatal(err)
	}
	s.completed = false
	if o.CompleteCleanup(context.Background(), w, nil, nil) == nil {
		t.Fatal("negative cleanup ACK accepted")
	}
	s.work.Kind = cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_PUBLISH
	s.work.TargetRevision = 4
	s.work.ClaimantId = "old_pod"
	s.work.ClaimGeneration = 7
	s.work.LeaseDeadline = timestamppb.New(time.Date(2026, 9, 5, 1, 0, 20, 0, time.UTC))
	w.Kind = value.DraftPublish
	w.TargetRevision = 4
	e := encryptedFixture(w)
	s.work.RecoveryEncrypted = &cp.RuntimeSecretDraftEncryptedDescriptor{Namespace: e.Namespace, SecretName: e.Name, SecretKey: e.DataKey, SecretUid: e.UID, SecretResourceVersion: e.ResourceVersion, CiphertextSha256: e.CiphertextSHA256, EncryptionKeyId: e.EncryptionKey.ID, EncryptionKeyGeneration: 1}
	name, _ := runtimesecret.VersionedKubernetesName(w.Draft.SecretRef, 4)
	s.work.RecoveryMaterialization = &cp.RuntimeSecretMaterialization{Namespace: w.RuntimeNamespace, SecretName: name, SecretKey: "value", SecretUid: "runtime-uid", SecretResourceVersion: "777", ContentSha256: w.Binding.ContentSHA256}
	s.page = 0
	items, err = o.ListRecovery(context.Background())
	if err != nil || items[0].RecoveryEncrypted == nil || items[0].RecoveryMaterialization == nil || items[0].RecoveryMaterialization.Revision != 4 || items[0].RecoveryMaterialization.ResourceVersion != "777" {
		t.Fatalf("lost cleanup descriptors: %v", err)
	}
	s.work.ClaimantId = ""
	s.work.ClaimGeneration = 0
	s.work.LeaseDeadline = nil
	s.page = 0
	if _, err := o.ListRecovery(context.Background()); !errors.Is(err, secretdrafts.ErrConflict) {
		t.Fatal("prepared work contained runtime effect")
	}
}

// TestOwnerRejectsMalformedOwnerResponses mutates work enums/identity/claims/metadata, invalidates
// outgoing descriptor pins and checks foreign completion/false readiness refusal.
// TestOwnerRejectsMalformedOwnerResponses меняет work enums/identity/claims/metadata, повреждает outgoing
// descriptor pins и проверяет отказ foreign completion/false readiness.
func TestOwnerRejectsMalformedOwnerResponses(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeSecretDraftWork){"nil draft": func(w *cp.RuntimeSecretDraftWork) { w.Draft = nil }, "unknown kind": func(w *cp.RuntimeSecretDraftWork) { w.Kind = 99 }, "unknown state": func(w *cp.RuntimeSecretDraftWork) { w.Draft.State = 99 }, "unknown type": func(w *cp.RuntimeSecretDraftWork) { w.Draft.ValueType = 99 }, "wrong claimant": func(w *cp.RuntimeSecretDraftWork) { w.ClaimantId = "foreign" }, "zero claim": func(w *cp.RuntimeSecretDraftWork) { w.ClaimGeneration = 0 }, "no lease": func(w *cp.RuntimeSecretDraftWork) { w.LeaseDeadline = nil }, "digest": func(w *cp.RuntimeSecretDraftWork) { w.ExpectedContentSha256 = "private" }, "stage name": func(w *cp.RuntimeSecretDraftWork) { w.StagedSecretName = "foreign" }, "stage key": func(w *cp.RuntimeSecretDraftWork) { w.StagedSecretKey = "value" }, "stage namespace": func(w *cp.RuntimeSecretDraftWork) { w.StagedNamespace = "../foreign" }, "published mismatch": func(w *cp.RuntimeSecretDraftWork) {
		w.Draft.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED
	}, "generation": func(w *cp.RuntimeSecretDraftWork) { w.Draft.Generation = 0 }} {
		t.Run(name, func(t *testing.T) {
			s := &ownerStub{work: workFixture()}
			mutate(s.work)
			o, _ := New(s, "pod_fixture")
			if _, err := o.Consume(context.Background(), "synthetic-grant"); !errors.Is(err, secretdrafts.ErrConflict) {
				t.Fatal("invalid owner work accepted")
			}
		})
	}
	w, s, o := nativeFixture(t)
	e := encryptedFixture(w)
	e.UID = ""
	if _, err := o.Complete(context.Background(), w, &e, nil); !errors.Is(err, secretdrafts.ErrInvalid) || s.request != nil {
		t.Fatal("descriptor without UID reached owner")
	}
	e.UID = "uid"
	e.ResourceVersion = "\n123"
	if _, err := o.Complete(context.Background(), w, &e, nil); !errors.Is(err, secretdrafts.ErrInvalid) || s.request != nil {
		t.Fatal("malformed resource version reached owner")
	}
	s.draft = draftResult(s, cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT)
	s.draft.SecretRef = "foreign"
	if _, err := o.Complete(context.Background(), w, nil, nil); !errors.Is(err, secretdrafts.ErrConflict) {
		t.Fatal("foreign completion accepted")
	}
	s.ready = false
	if o.Check(context.Background()) == nil {
		t.Fatal("false readiness accepted")
	}
}

// TestOwnerPaginationAndErrors checks two-page traversal, repeated cursor/work refusal and fixed sentinel
// mappings that omit arbitrary upstream error text.
// TestOwnerPaginationAndErrors проверяет two-page traversal, отказ repeated cursor/work и fixed sentinel
// mappings без arbitrary upstream error text.
func TestOwnerPaginationAndErrors(t *testing.T) {
	_, s, o := nativeFixture(t)
	second := proto.Clone(s.work).(*cp.RuntimeSecretDraftWork)
	second.OperationRef = "operation_second"
	s.pages = []*cp.ListRuntimeSecretDraftRecoveryWorkResponse{{Operations: []*cp.RuntimeSecretDraftWork{s.work}, Page: &cp.PageInfo{NextPageToken: "next"}}, {Operations: []*cp.RuntimeSecretDraftWork{second}}}
	items, err := o.ListRecovery(context.Background())
	if err != nil || len(items) != 2 || s.request.(*cp.ListRuntimeSecretDraftRecoveryWorkRequest).GetPage().GetPageToken() != "next" {
		t.Fatal("pagination lost")
	}
	s.page = 0
	s.pages = s.pages[:1]
	if _, err := o.ListRecovery(context.Background()); !errors.Is(err, secretdrafts.ErrConflict) {
		t.Fatal("duplicate cursor/operation accepted")
	}
	for code, want := range map[codes.Code]error{codes.NotFound: secretdrafts.ErrNotFound, codes.InvalidArgument: secretdrafts.ErrInvalid, codes.Aborted: secretdrafts.ErrConflict, codes.PermissionDenied: secretdrafts.ErrConflict, codes.Unavailable: secretdrafts.ErrUnavailable} {
		s.failure = status.Error(code, "private upstream detail")
		if _, err := o.Consume(context.Background(), "synthetic-grant"); !errors.Is(err, want) || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe RPC error")
		}
	}
}

// TestOwnerCancellationAndRecoveryBudget requires every cancelled operation to avoid RPC calls and a
// ten-page batch to preserve its continuation hint.
// TestOwnerCancellationAndRecoveryBudget требует отсутствия RPC calls для каждой cancelled operation и
// сохранения continuation hint после ten-page batch.
func TestOwnerCancellationAndRecoveryBudget(t *testing.T) {
	w, s, o := nativeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := o.Check(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("check ignored cancellation")
	}
	if _, err := o.Consume(ctx, "synthetic-grant"); !errors.Is(err, context.Canceled) {
		t.Fatal("consume ignored cancellation")
	}
	if _, err := o.Complete(ctx, w, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("complete ignored cancellation")
	}
	if err := o.Fail(ctx, w); !errors.Is(err, context.Canceled) {
		t.Fatal("fail ignored cancellation")
	}
	if _, err := o.ListRecovery(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("list ignored cancellation")
	}
	if _, err := o.Recover(ctx, w, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("recover ignored cancellation")
	}
	if err := o.CompleteCleanup(ctx, w, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cleanup ignored cancellation")
	}
	if s.request != nil {
		t.Fatal("cancelled operation reached owner")
	}
	for index := 0; index < 10; index++ {
		item := proto.Clone(s.work).(*cp.RuntimeSecretDraftWork)
		item.OperationRef = "operation_" + string(rune('a'+index))
		s.pages = append(s.pages, &cp.ListRuntimeSecretDraftRecoveryWorkResponse{Operations: []*cp.RuntimeSecretDraftWork{item}, Page: &cp.PageInfo{NextPageToken: "page_" + string(rune('a'+index))}})
	}
	if items, err := o.ListRecovery(context.Background()); err != nil || len(items) != 10 || s.page != 10 || o.recoveryCursor != "page_j" {
		t.Fatal("recovery did not return bounded batch and continuation")
	}
}
