package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sshrepo "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/repository/sshtarget"
	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sshOwnerRPCFixture supplies finite typed responses without a second lifecycle or credential selector; unexpected generated methods remain absent and cannot silently succeed.
// sshOwnerRPCFixture задаёт конечные typed responses без второго lifecycle либо credential selector; неожиданные generated methods отсутствуют и не проходят незаметно.
type sshOwnerRPCFixture struct {
	cp.ManagedSSHIdentitySetupServiceClient
	resolve         *cp.ResolveSSHIdentityUploadResponse
	resolveErr      error
	resolvedRequest *cp.ResolveSSHIdentityUploadRequest
	onResolve       func()
	claim           *cp.ClaimSSHIdentityUploadResponse
	complete        *cp.CompleteSSHIdentityUploadRequest
	calls           int
}

// ResolveSSHIdentityUpload returns a cloned owner snapshot so unknown-field mutations cannot alter another call's receipt implicitly.
// ResolveSSHIdentityUpload возвращает clone owner snapshot, чтобы unknown-field mutations не меняли receipt другого call неявно.
func (fixture *sshOwnerRPCFixture) ResolveSSHIdentityUpload(_ context.Context, input *cp.ResolveSSHIdentityUploadRequest, _ ...grpc.CallOption) (*cp.ResolveSSHIdentityUploadResponse, error) {
	fixture.calls++
	fixture.resolvedRequest = proto.Clone(input).(*cp.ResolveSSHIdentityUploadRequest)
	if fixture.onResolve != nil {
		fixture.onResolve()
	}
	if fixture.resolveErr != nil {
		return nil, fixture.resolveErr
	}
	if fixture.resolve == nil {
		return nil, nil
	}
	return proto.Clone(fixture.resolve).(*cp.ResolveSSHIdentityUploadResponse), nil
}

// ClaimSSHIdentityUpload returns the sole fixed admission without manufacturing retry generations or another material identity.
// ClaimSSHIdentityUpload возвращает единственный fixed admission без изготовления retry generations либо другой material identity.
func (fixture *sshOwnerRPCFixture) ClaimSSHIdentityUpload(context.Context, *cp.ClaimSSHIdentityUploadRequest, ...grpc.CallOption) (*cp.ClaimSSHIdentityUploadResponse, error) {
	fixture.calls++
	if fixture.claim == nil {
		return nil, nil
	}
	return proto.Clone(fixture.claim).(*cp.ClaimSSHIdentityUploadResponse), nil
}

// CompleteSSHIdentityUpload records the actual wire message and returns only safe setup metadata; it performs no external storage operation.
// CompleteSSHIdentityUpload фиксирует actual wire message и возвращает лишь safe setup metadata без external storage operation.
func (fixture *sshOwnerRPCFixture) CompleteSSHIdentityUpload(_ context.Context, input *cp.CompleteSSHIdentityUploadRequest, _ ...grpc.CallOption) (*cp.CompleteSSHIdentityUploadResponse, error) {
	fixture.calls++
	fixture.complete = proto.Clone(input).(*cp.CompleteSSHIdentityUploadRequest)
	return &cp.CompleteSSHIdentityUploadResponse{Setup: &cp.ManagedSSHIdentitySetupView{Ref: input.GetSetupRef(), State: cp.ManagedSSHIdentitySetupState_MANAGED_SSH_IDENTITY_SETUP_STATE_COMPLETED, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}, nil
}

// sshOwnerProtocolFixture binds consistent synthetic coordinates and short deadlines across the real broker owner adapter and its protected generated client port.
// sshOwnerProtocolFixture связывает согласованные synthetic coordinates и short deadlines между реальным broker owner adapter и protected generated client port.
func sshOwnerProtocolFixture() (*Owner, *sshOwnerRPCFixture) {
	preparation := &cp.ManagedSSHIdentityPreparation{IdentityRevisionRef: "broker_identity_ssh_fixture", TargetRef: "target_fixture", Revision: 1, Namespace: "kodex-system", ContentSha256: strings.Repeat("a", 64)}
	fixture := &sshOwnerRPCFixture{resolve: &cp.ResolveSSHIdentityUploadResponse{SetupRef: "ssu_fixture_setup", SnapshotDigest: strings.Repeat("c", 64), Preparation: preparation, ContentBytes: 100, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)), Mode: cp.ManagedSSHIdentityUploadMode_MANAGED_SSH_IDENTITY_UPLOAD_MODE_WRITE},
		claim: &cp.ClaimSSHIdentityUploadResponse{SetupRef: "ssu_fixture_setup", SnapshotDigest: strings.Repeat("c", 64), AttemptRef: "ssa_fixture_attempt", WorkloadInstance: "broker_fixture_instance", Generation: 1, Fence: "fence_fixture_value", ExpiresAt: timestamppb.New(time.Now().Add(20 * time.Second)), Mode: cp.ManagedSSHIdentityUploadMode_MANAGED_SSH_IDENTITY_UPLOAD_MODE_WRITE, Preparation: proto.Clone(preparation).(*cp.ManagedSSHIdentityPreparation), AuthenticationSha256: strings.Repeat("b", 64)}}
	return &Owner{client: &controlplaneclient.Client{SSHIdentities: fixture}}, fixture
}

// TestSSHOwnerProtocolBindsActualGrant verifies the actual decoded admission retains every immutable pin and the existing domain consumer refuses switched targets before storage credit.
// TestSSHOwnerProtocolBindsActualGrant проверяет сохранение всех immutable pins в actual decoded admission и отказ existing domain consumer при смене target до storage credit.
func TestSSHOwnerProtocolBindsActualGrant(t *testing.T) {
	owner, fixture := sshOwnerProtocolFixture()
	snapshot, err := owner.ResolveSSHIdentityUpload(t.Context(), "ssu_fixture_setup", strings.Repeat("d", 64))
	if err != nil {
		t.Fatal("owner snapshot rejected")
	}
	input := sshrepo.IdentityUploadClaim{SetupRef: snapshot.SetupRef, SnapshotDigest: snapshot.Digest, PermitSHA256: strings.Repeat("d", 64), WorkloadInstance: "broker_fixture_instance", Key: value.SSHIdentityKeyMetadata{ContentSHA256: strings.Repeat("a", 64), AuthenticationSHA256: strings.Repeat("b", 64)}}
	admission, err := owner.ClaimSSHIdentityUpload(t.Context(), input)
	if err != nil || !admission.ValidFor(snapshot, input.Key, input.WorkloadInstance, time.Now()) {
		t.Fatal("exact owner admission changed")
	}
	fixture.claim.Preparation.TargetRef = "foreign_target"
	changed, err := owner.ClaimSSHIdentityUpload(t.Context(), input)
	if err != nil {
		t.Fatal("typed alternate fixture malformed")
	}
	if changed.ValidFor(snapshot, input.Key, input.WorkloadInstance, time.Now()) {
		t.Fatal("domain consumer admitted switched owner target")
	}
	if fixture.calls != 3 {
		t.Fatal("owner adapter retried a call")
	}
}

// TestSSHOwnerUnknownFieldsAndInvalidGrantCloseRoute rejects nested unknowns, unknown modes, expiry, missing responses and switched instance without retries or reflected private diagnostics.
// TestSSHOwnerUnknownFieldsAndInvalidGrantCloseRoute отклоняет nested unknowns, unknown modes, expiry, missing responses и switched instance без retries либо private diagnostics.
func TestSSHOwnerUnknownFieldsAndInvalidGrantCloseRoute(t *testing.T) {
	for _, mutate := range []func(*sshOwnerRPCFixture){func(f *sshOwnerRPCFixture) { f.resolve = nil }, func(f *sshOwnerRPCFixture) { f.resolve.Mode = 99 },
		func(f *sshOwnerRPCFixture) { f.resolve.Preparation.ProtoReflect().SetUnknown([]byte{0xa0, 6, 1}) }, func(f *sshOwnerRPCFixture) { f.resolve.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second)) }} {
		owner, fixture := sshOwnerProtocolFixture()
		mutate(fixture)
		if _, err := owner.ResolveSSHIdentityUpload(t.Context(), "ssu_fixture_setup", strings.Repeat("d", 64)); !errors.Is(err, sshrepo.ErrIdentityUnavailable) || fixture.calls != 1 {
			t.Fatal("malformed owner snapshot admitted or retried")
		}
	}
	for _, mutate := range []func(*sshOwnerRPCFixture){func(f *sshOwnerRPCFixture) { f.claim.WorkloadInstance = "foreign_instance" }, func(f *sshOwnerRPCFixture) { f.claim.Generation = 9 }, func(f *sshOwnerRPCFixture) { f.claim.ExpiresAt = timestamppb.New(time.Now().Add(time.Minute)) }} {
		owner, fixture := sshOwnerProtocolFixture()
		mutate(fixture)
		if _, err := owner.ClaimSSHIdentityUpload(t.Context(), sshrepo.IdentityUploadClaim{SetupRef: "ssu_fixture_setup", SnapshotDigest: strings.Repeat("c", 64), PermitSHA256: strings.Repeat("d", 64), WorkloadInstance: "broker_fixture_instance", Key: value.SSHIdentityKeyMetadata{ContentSHA256: strings.Repeat("a", 64), AuthenticationSHA256: strings.Repeat("b", 64)}}); !errors.Is(err, sshrepo.ErrIdentityUnavailable) || fixture.calls != 1 {
			t.Fatal("expanded owner grant admitted or retried")
		}
	}
}

// TestSSHOwnerCompletionPreservesRejectedVersusUnknown verifies exact oneof messages on the real owner adapter, retaining a rejected successful ACK witness instead of granting an observation classification.
// TestSSHOwnerCompletionPreservesRejectedVersusUnknown проверяет exact oneof messages реального owner adapter, сохраняя rejected successful ACK witness вместо observation classification.
func TestSSHOwnerCompletionPreservesRejectedVersusUnknown(t *testing.T) {
	owner, fixture := sshOwnerProtocolFixture()
	admission := value.SSHIdentityUploadAdmission{SetupRef: "ssu_fixture_setup", AttemptRef: "ssa_fixture_attempt", WorkloadInstance: "broker_fixture_instance", Fence: "fence_fixture_value", Generation: 1, Mode: value.SSHIdentityUploadWrite}
	unknown := sshrepo.IdentityUploadCompletion{Admission: admission, Unknown: true}
	if err := owner.CompleteSSHIdentityUpload(t.Context(), unknown); err != nil || fixture.complete.GetUnknown() == nil || fixture.complete.GetIdentity() != nil || fixture.complete.GetRejectedAcknowledgement() != nil {
		t.Fatal("UNKNOWN completion wire changed")
	}
	rejected := sshrepo.IdentityUploadCompletion{Admission: admission, Rejected: &sshrepo.IdentityAcknowledgementRejection{ResponseSHA256: strings.Repeat("e", 64), ObservedUID: "original-opaque-uid", Truncated: true}}
	if err := owner.CompleteSSHIdentityUpload(t.Context(), rejected); err != nil || fixture.complete.GetUnknown() != nil || fixture.complete.GetRejectedAcknowledgement().GetObservedUid() != "original-opaque-uid" || !fixture.complete.GetRejectedAcknowledgement().GetTruncated() {
		t.Fatal("rejected acknowledgement lost terminal witness")
	}
	rejected.Unknown = true
	before := fixture.calls
	if err := owner.CompleteSSHIdentityUpload(t.Context(), rejected); !errors.Is(err, sshrepo.ErrIdentityUnavailable) || fixture.calls != before {
		t.Fatal("mixed completion reached owner")
	}
}

// TestSSHOwnerReadinessUsesProtectedLookup accepts only the sentinel NotFound, never a real credential reply, denial, missing method, canceled lookup or infrastructure error.
// TestSSHOwnerReadinessUsesProtectedLookup принимает лишь sentinel NotFound, не real credential reply, denial, missing method, canceled lookup либо infrastructure error.
func TestSSHOwnerReadinessUsesProtectedLookup(t *testing.T) {
	for _, code := range []codes.Code{codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.Unimplemented, codes.Unavailable, codes.InvalidArgument, codes.OK} {
		owner, fixture := sshOwnerProtocolFixture()
		fixture.resolveErr = status.Error(code, "synthetic readiness result")
		err := owner.CheckSSHIdentitySetupReadiness(t.Context())
		if (err == nil) != (code == codes.NotFound) || fixture.calls != 1 || fixture.resolvedRequest.GetSetupRef() != "ssu_00000000000000000000000000000000" || fixture.resolvedRequest.GetPermitSha256() != strings.Repeat("0", 64) {
			t.Fatal("readiness bypassed protected lookup boundary")
		}
	}
	owner, fixture := sshOwnerProtocolFixture()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture.onResolve = cancel
	fixture.resolveErr = status.Error(codes.NotFound, "synthetic missing setup")
	if owner.CheckSSHIdentitySetupReadiness(ctx) == nil {
		t.Fatal("canceled owner lookup acquired readiness")
	}
}
