package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	broker "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingcrypto"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/integration/stagingguard"
	store "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/observability"
	transport "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// Owner fixture заменяет PostgreSQL/authority CP, сохраняя настоящий generated
// client port. AEAD, keyring, оба Kubernetes adapter и RPC caster здесь реальные.
type composedDraftOwner struct {
	cp.RuntimeSecretDraftWorkServiceClient
	t            *testing.T
	work         *cp.RuntimeSecretDraftWork
	recoveryErr  error
	materialized *cp.RuntimeSecretMaterialization
}

func (owner *composedDraftOwner) CheckRuntimeSecretDraftWorkReadiness(context.Context, *cp.CheckRuntimeSecretDraftWorkReadinessRequest, ...grpc.CallOption) (*cp.CheckRuntimeSecretDraftWorkReadinessResponse, error) {
	return &cp.CheckRuntimeSecretDraftWorkReadinessResponse{Ready: true}, nil
}
func (owner *composedDraftOwner) ListRuntimeSecretDraftRecoveryWork(context.Context, *cp.ListRuntimeSecretDraftRecoveryWorkRequest, ...grpc.CallOption) (*cp.ListRuntimeSecretDraftRecoveryWorkResponse, error) {
	return &cp.ListRuntimeSecretDraftRecoveryWorkResponse{}, owner.recoveryErr
}
func (owner *composedDraftOwner) ConsumeRuntimeSecretDraftOperation(_ context.Context, request *cp.ConsumeRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.ConsumeRuntimeSecretDraftOperationResponse, error) {
	if request.GetClaimantId() != owner.work.ClaimantId || request.GetOperationGrant() != "synthetic-grant" {
		owner.t.Fatal("composition lost operation grant or claimant")
	}
	return &cp.ConsumeRuntimeSecretDraftOperationResponse{Work: proto.Clone(owner.work).(*cp.RuntimeSecretDraftWork)}, nil
}
func (owner *composedDraftOwner) CompleteRuntimeSecretDraftOperation(_ context.Context, request *cp.CompleteRuntimeSecretDraftOperationRequest, _ ...grpc.CallOption) (*cp.CompleteRuntimeSecretDraftOperationResponse, error) {
	w := owner.work
	if request.GetOperationRef() != w.OperationRef || request.GetClaimantId() != w.ClaimantId || request.GetClaimGeneration() != w.ClaimGeneration {
		owner.t.Fatal("composition lost completion fence")
	}
	if request.GetEncrypted() == nil {
		owner.t.Fatal("composition omitted encrypted descriptor")
	}
	w.Encrypted = proto.Clone(request.Encrypted).(*cp.RuntimeSecretDraftEncryptedDescriptor)
	d := proto.Clone(w.Draft).(*cp.RuntimeSecretDraft)
	d.Version++
	result := &cp.CompleteRuntimeSecretDraftOperationResponse{Draft: d}
	switch w.Kind {
	case cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_SAVE:
		d.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT
	case cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_VALIDATE:
		d.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_VALID
	case cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_PUBLISH:
		if request.GetMaterialization() == nil || request.GetMaterialization().GetDisplayHint() != nil {
			owner.t.Fatal("composition omitted materialization or exposed a value hint")
		}
		owner.materialized = proto.Clone(request.Materialization).(*cp.RuntimeSecretMaterialization)
		d.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED
		d.SecretVersion++
		d.PublishedRevision = w.TargetRevision
		result.Secret = &cp.RuntimeSecret{Ref: d.SecretRef, ProjectRef: d.ProjectRef, Name: d.Name, ValueType: d.ValueType,
			Version: d.SecretVersion, CurrentRevision: w.TargetRevision, State: "ACTIVE", CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt}
	case cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_DISCARD:
		d.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DISCARDED
	default:
		owner.t.Fatal("unexpected operation")
	}
	w.Draft = proto.Clone(d).(*cp.RuntimeSecretDraft)
	return result, nil
}

func composedDraftKubernetes(t *testing.T, namespace, guardName string) *fake.Clientset {
	t.Helper()
	client := fake.NewSimpleClientset(&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: guardName, Namespace: namespace,
		UID: "guard-uid", ResourceVersion: "1", Labels: map[string]string{stagingguard.OwnerLabel: stagingguard.OwnerValue,
			stagingguard.PurposeLabel: stagingguard.PurposeValue}}, Data: map[string]string{stagingguard.StateKey: `{"v":1,"manifest":null,"uses":[]}`}})
	var version atomic.Int64
	version.Store(100)
	client.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		object := action.(k8stesting.CreateAction).GetObject().(*corev1.Secret).DeepCopy()
		object.UID = types.UID("uid-" + object.Name)
		object.ResourceVersion = strconv.FormatInt(version.Add(1), 10)
		err := client.Tracker().Create(corev1.SchemeGroupVersion.WithResource("secrets"), object, object.Namespace)
		return true, object, err
	})
	client.PrependReactor("update", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		next := action.(k8stesting.UpdateAction).GetObject().(*corev1.ConfigMap).DeepCopy()
		resource := corev1.SchemeGroupVersion.WithResource("configmaps")
		old, err := client.Tracker().Get(resource, next.Namespace, next.Name)
		if err != nil {
			return true, nil, err
		}
		previous := old.(*corev1.ConfigMap)
		if next.UID != previous.UID || next.ResourceVersion != previous.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, next.Name, errors.New("fixture conflict"))
		}
		next.ResourceVersion = strconv.FormatInt(version.Add(1), 10)
		err = client.Tracker().Update(resource, next, next.Namespace)
		return true, next, err
	})
	return client
}

func TestSecretDraftCompositionEncryptedLifecycleAndReadiness(t *testing.T) {
	ctx := context.Background()
	const stagedNamespace, runtimeNamespace, guardName = "kodex-secret-drafts", "kodex-runtime", "secret-broker-draft-key-guard"
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	keyring := filepath.Join(directory, "keyring.json")
	if err := stagingcrypto.GenerateFile(keyring); err != nil {
		t.Fatal(err)
	}
	client := composedDraftKubernetes(t, stagedNamespace, guardName)
	runtimeStore, err := store.New(client, runtimeNamespace)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	fixture := []byte("synthetic composition value")
	digest := sha256.Sum256(fixture)
	refDigest := sha256.Sum256([]byte("draft_composition"))
	owner := &composedDraftOwner{t: t, work: &cp.RuntimeSecretDraftWork{
		OperationRef: "operation_save", Kind: cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_SAVE,
		Draft: &cp.RuntimeSecretDraft{Ref: "draft_composition", ProjectRef: "project_composition", SecretRef: "sec_fixture01", Name: "SYNTHETIC_KEY",
			Version: 1, Generation: 1, SecretVersion: 1, ValueType: cp.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING,
			State:     cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PREPARING,
			CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))},
		ExpectedContentSha256: hex.EncodeToString(digest[:]), Namespace: runtimeNamespace, StagedNamespace: stagedNamespace,
		StagedSecretName: "runtime-secret-draft-" + hex.EncodeToString(refDigest[:16]), StagedSecretKey: "ciphertext",
		ClaimantId: "pod_composition", ClaimGeneration: 1, LeaseDeadline: timestamppb.New(now.Add(time.Minute)), ExpiresAt: timestamppb.New(now.Add(2 * time.Minute)),
	}}
	service, err := composeSecretDrafts(Config{ClaimantID: "pod_composition", MaximumSecretBytes: 1024, RuntimeNamespace: runtimeNamespace,
		DraftNamespace: stagedNamespace, DraftKeyGuardName: guardName, DraftKeyringFile: keyring}, owner, runtimeStore, observability.NewSecretDrafts(), client)
	if err != nil {
		t.Fatal(err)
	}
	server := &transport.Server{}
	transport.WithDraftCommands(service)(server)
	if _, err := server.CheckSecretDraftReadiness(ctx, nil); status.Code(err) != codes.Unavailable {
		t.Fatal("startup skipped recovery barrier")
	}
	if err := service.ReconcileOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if ready, err := server.CheckSecretDraftReadiness(ctx, nil); err != nil || !ready.GetReady() {
		t.Fatal("composed service did not become ready")
	}
	request := &broker.SaveSecretDraftRequest{OperationGrant: "synthetic-grant", Value: bytes.Clone(fixture)}
	saved, err := server.SaveSecretDraft(ctx, request)
	if err != nil || saved.GetDraft().GetSecretVersion() != 1 {
		t.Fatalf("save failed: %v", err)
	}
	if !bytes.Equal(request.Value, make([]byte, len(request.Value))) {
		t.Fatal("save retained request plaintext")
	}
	staged, err := client.CoreV1().Secrets(stagedNamespace).Get(ctx, owner.work.StagedSecretName, metav1.GetOptions{})
	if err != nil || staged.Immutable == nil || !*staged.Immutable || len(staged.Data) != 1 || len(staged.Data["ciphertext"]) != len(fixture)+28 || bytes.Contains(staged.Data["ciphertext"], fixture) {
		t.Fatal("save did not store only immutable authenticated ciphertext")
	}
	objects, err := client.CoreV1().Secrets(runtimeNamespace).List(ctx, metav1.ListOptions{})
	if err != nil || len(objects.Items) != 0 {
		t.Fatal("save published an active secret")
	}
	owner.work.Kind = cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_VALIDATE
	owner.work.OperationRef = "operation_validate"
	validated, err := server.ValidateSecretDraft(ctx, &broker.ValidateSecretDraftRequest{OperationGrant: "synthetic-grant"})
	if err != nil || validated.GetDraft().GetState() != broker.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_VALID {
		t.Fatalf("validate failed: %v", err)
	}
	owner.work.Kind = cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_PUBLISH
	owner.work.OperationRef, owner.work.TargetRevision = "operation_publish", 1
	owner.work.Draft.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHING
	published, err := server.PublishSecretDraft(ctx, &broker.PublishSecretDraftRequest{OperationGrant: "synthetic-grant"})
	if err != nil || published.GetSecret().GetVersion() != 2 || published.GetDraft().GetSecretVersion() != 2 {
		t.Fatalf("publish failed: %v", err)
	}
	materialized := owner.materialized
	active, err := client.CoreV1().Secrets(runtimeNamespace).Get(ctx, materialized.GetSecretName(), metav1.GetOptions{})
	if err != nil || !bytes.Equal(active.Data["value"], fixture) || string(active.UID) != materialized.GetSecretUid() || active.ResourceVersion != materialized.GetSecretResourceVersion() || active.Immutable == nil || !*active.Immutable {
		t.Fatal("publish did not read back exact immutable runtime bytes")
	}
	// Следующий черновик того же active Secret можно сохранить и отбросить,
	// сохранив опубликованную immutable revision.
	owner.work.Draft.Ref = "draft_next"
	owner.work.Draft.Generation++
	owner.work.Draft.Version, owner.work.Draft.PublishedRevision = 1, 0
	owner.work.Draft.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PREPARING
	nextDigest := sha256.Sum256([]byte(owner.work.Draft.Ref))
	owner.work.StagedSecretName = "runtime-secret-draft-" + hex.EncodeToString(nextDigest[:16])
	owner.work.Encrypted = nil
	owner.work.Kind = cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_SAVE
	owner.work.OperationRef, owner.work.TargetRevision = "operation_save_next", 0
	if _, err := server.SaveSecretDraft(ctx, &broker.SaveSecretDraftRequest{OperationGrant: "synthetic-grant", Value: bytes.Clone(fixture)}); err != nil {
		t.Fatalf("save next draft failed: %v", err)
	}
	owner.work.Kind = cp.RuntimeSecretDraftOperationKind_RUNTIME_SECRET_DRAFT_OPERATION_KIND_DISCARD
	owner.work.OperationRef, owner.work.TargetRevision = "operation_discard", 0
	owner.work.Draft.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DISCARDED
	discarded, err := server.DiscardSecretDraft(ctx, &broker.DiscardSecretDraftRequest{OperationGrant: "synthetic-grant"})
	if err != nil || discarded.GetDraft().GetState() != broker.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DISCARDED {
		t.Fatalf("discard failed: %v", err)
	}
	if _, err := client.CoreV1().Secrets(stagedNamespace).Get(ctx, owner.work.StagedSecretName, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("discard did not delete ciphertext")
	}
	if _, err := client.CoreV1().Secrets(runtimeNamespace).Get(ctx, active.Name, metav1.GetOptions{}); err != nil {
		t.Fatal("discard touched published runtime bytes")
	}
	owner.recoveryErr = status.Error(codes.Unavailable, "synthetic private diagnostic")
	if service.ReconcileOnce(ctx) == nil {
		t.Fatal("failed recovery became successful")
	}
	if _, err := server.CheckSecretDraftReadiness(ctx, nil); status.Code(err) != codes.Unavailable || bytes.Contains([]byte(err.Error()), []byte("private")) {
		t.Fatal("failed recovery remained ready or exposed upstream diagnostics")
	}
	owner.recoveryErr = nil
	if err := service.ReconcileOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if ready, err := server.CheckSecretDraftReadiness(ctx, nil); err != nil || !ready.GetReady() {
		t.Fatal("readiness did not recover")
	}
}
