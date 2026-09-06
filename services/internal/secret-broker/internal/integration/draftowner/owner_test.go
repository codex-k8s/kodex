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

func (s *ownerStub) CheckRuntimeSecretDraftWorkReadiness(_ context.Context, r *cp.CheckRuntimeSecretDraftWorkReadinessRequest, _ ...grpc.CallOption) (*cp.CheckRuntimeSecretDraftWorkReadinessResponse, error) {
	s.request = r
	return &cp.CheckRuntimeSecretDraftWorkReadinessResponse{Ready: s.ready}, s.failure
}
func (s *ownerStub) ConsumeRuntimeSecretDraftOperation(_ context.Context, r *cp.ConsumeRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.ConsumeRuntimeSecretDraftOperationResponse, error) {
	s.request = r
	return &cp.ConsumeRuntimeSecretDraftOperationResponse{Work: s.work}, s.failure
}
func (s *ownerStub) CompleteRuntimeSecretDraftOperation(_ context.Context, r *cp.CompleteRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.CompleteRuntimeSecretDraftOperationResponse, error) {
	s.request = r
	return &cp.CompleteRuntimeSecretDraftOperationResponse{Draft: s.draft, Secret: s.secret}, s.failure
}
func (s *ownerStub) FailRuntimeSecretDraftOperation(_ context.Context, r *cp.FailRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.FailRuntimeSecretDraftOperationResponse, error) {
	s.request = r
	return &cp.FailRuntimeSecretDraftOperationResponse{Draft: s.draft, State: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED}, s.failure
}
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
func (s *ownerStub) RecoverRuntimeSecretDraftMaterialization(_ context.Context, r *cp.RecoverRuntimeSecretDraftMaterializationRequest, _ ...grpc.CallOption) (*cp.RecoverRuntimeSecretDraftMaterializationResponse, error) {
	s.request = r
	return &cp.RecoverRuntimeSecretDraftMaterializationResponse{Draft: s.draft, OperationState: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_FAILED, EncryptedAction: s.action, MaterializationAction: s.action}, s.failure
}
func (s *ownerStub) CompleteRuntimeSecretDraftCleanup(_ context.Context, r *cp.CompleteRuntimeSecretDraftCleanupRequest, _ ...grpc.CallOption) (*cp.CompleteRuntimeSecretDraftCleanupResponse, error) {
	s.request = r
	return &cp.CompleteRuntimeSecretDraftCleanupResponse{Completed: s.completed}, s.failure
}

func workFixture() *cp.RuntimeSecretDraftWork {
	tm := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	ref := "draft_fixture"
	sum := sha256.Sum256([]byte(ref))
	return &cp.RuntimeSecretDraftWork{OperationRef: "operation_fixture", Kind: cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_SAVE, Draft: &cp.RuntimeSecretDraft{Ref: ref, Version: 1, Generation: 1, SecretVersion: 1, ProjectRef: "project_fixture", SecretRef: "sec_fixture01", Name: "SYNTHETIC_KEY", ValueType: cp.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, State: cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PREPARING, CreatedAt: timestamppb.New(tm), UpdatedAt: timestamppb.New(tm), ExpiresAt: timestamppb.New(tm.Add(time.Hour))}, ExpectedContentSha256: strings.Repeat("a", 64), Namespace: "kodex-runtime", StagedNamespace: "kodex-system", StagedSecretName: "runtime-secret-draft-" + hex.EncodeToString(sum[:16]), StagedSecretKey: "ciphertext", ClaimantId: "pod_fixture", ClaimGeneration: 3, LeaseDeadline: timestamppb.New(tm.Add(time.Minute)), ExpiresAt: timestamppb.New(tm.Add(time.Minute))}
}
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
func encryptedFixture(w value.DraftWork) value.DraftEncryptedDescriptor {
	return value.DraftEncryptedDescriptor{Namespace: w.StagedNamespace, Name: w.StagedName, DataKey: w.StagedKey, UID: "encrypted-uid", ResourceVersion: "123", CiphertextSHA256: strings.Repeat("b", 64), EncryptionKey: value.DraftEncryptionKey{ID: strings.Repeat("c", 64), Generation: 1}}
}
func draftResult(s *ownerStub, state cp.RuntimeSecretDraftState) *cp.RuntimeSecretDraft {
	d := proto.Clone(s.work.Draft).(*cp.RuntimeSecretDraft)
	d.Version++
	d.State = state
	return d
}

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
	s.secret = &cp.RuntimeSecret{Ref: w.Draft.SecretRef, ProjectRef: w.Draft.ProjectRef, Name: w.Draft.Name, ValueType: s.work.Draft.ValueType, State: "ACTIVE", Version: 2, CurrentRevision: 4, CreatedAt: s.work.Draft.CreatedAt, UpdatedAt: s.work.Draft.UpdatedAt, DisplayHint: &cp.RuntimeSecretDisplayHint{Prefix: "not-returned"}}
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
