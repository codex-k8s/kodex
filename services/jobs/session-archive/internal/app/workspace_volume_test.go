package app

import (
	"context"
	"errors"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/controller"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// volumeOwnerFixture records terminal requests while returning a controlled uncertain ACK;
// it provides no live credentials, platform state or proof of a native effect.
// volumeOwnerFixture сохраняет terminal requests и возвращает controlled uncertain ACK;
// fixture не содержит live credentials/platform state и не доказывает native effect.
type volumeOwnerFixture struct {
	cp.WorkspaceVolumeWorkServiceClient
	lease     *cp.WorkLease
	complete  []*cp.CompleteWorkspaceVolumeTaskRequest
	renewals  int
	uncertain bool
}

// RenewWorkspaceVolumeTask returns the fixture's exact lease so the real adapter validates it.
// RenewWorkspaceVolumeTask возвращает exact fixture lease для проверки настоящим adapter.
func (owner *volumeOwnerFixture) RenewWorkspaceVolumeTask(context.Context, *cp.RenewWorkspaceVolumeTaskRequest, ...grpc.CallOption) (*cp.RenewWorkspaceVolumeTaskResponse, error) {
	owner.renewals++
	return &cp.RenewWorkspaceVolumeTaskResponse{Lease: proto.Clone(owner.lease).(*cp.WorkLease)}, nil
}

// CompleteWorkspaceVolumeTask simulates one lost response after a positive owner commit and
// subsequently returns the same recorded outcome; every submitted request is retained for comparison.
// CompleteWorkspaceVolumeTask имитирует один lost response после positive owner commit и затем
// возвращает тот же recorded outcome; все отправленные requests сохраняются для сравнения.
func (owner *volumeOwnerFixture) CompleteWorkspaceVolumeTask(_ context.Context, request *cp.CompleteWorkspaceVolumeTaskRequest, _ ...grpc.CallOption) (*cp.CompleteWorkspaceVolumeTaskResponse, error) {
	owner.complete = append(owner.complete, proto.Clone(request).(*cp.CompleteWorkspaceVolumeTaskRequest))
	if owner.uncertain && len(owner.complete) == 1 {
		return nil, status.Error(codes.Unavailable, "fixture lost response")
	}
	return &cp.CompleteWorkspaceVolumeTaskResponse{TaskRef: request.GetTaskRef(), RecordedTaskState: "SUCCEEDED",
		RecordedVolumeState: "CREATED", MatchedVolumeIncarnation: true}, nil
}

// volumeExecutorFixture counts physical invocation separately from terminal owner RPC retries.
// volumeExecutorFixture считает physical invocation отдельно от retries terminal owner RPC.
type volumeExecutorFixture struct{ creates int }

// CreateWorkspaceVolume requires the real adapter's renewal before returning a fixed positive UID.
// CreateWorkspaceVolume требует renewal настоящего adapter до возврата fixed positive UID.
func (executor *volumeExecutorFixture) CreateWorkspaceVolume(ctx context.Context, _ runtimecontract.WorkspaceVolumeWork, renew func(context.Context) error) (string, error) {
	if err := renew(ctx); err != nil {
		return "", err
	}
	executor.creates++
	return "10000000-0000-4000-8000-000000000001", nil
}

// PrepareWorkspaceVolume rejects the other stage in this first-Create-only fixture.
// PrepareWorkspaceVolume отклоняет другой stage в first-Create-only fixture.
func (*volumeExecutorFixture) PrepareWorkspaceVolume(context.Context, runtimecontract.WorkspaceVolumeWork, func(context.Context) error) (controller.WorkspaceVolumePreparationResult, *controller.WorkspaceVolumeResources, error) {
	return controller.WorkspaceVolumePreparationResult{}, nil, errors.New("unexpected fixture preparation")
}

// volumeClaimFixture constructs an exact immutable first-Create snapshot and a separate fresh
// private worker lease; the digest comes from the real contract, not a hand-written placeholder.
// volumeClaimFixture строит exact immutable first-Create snapshot и отдельный fresh private worker
// lease; digest назначается настоящим контрактом, не рукописным placeholder.
func volumeClaimFixture(t *testing.T) *cp.WorkspaceVolumeTask {
	t.Helper()
	work := runtimecontract.WorkspaceVolumeWork{TaskRef: "svt_create0000", Kind: runtimecontract.WorkspaceVolumeCreate, Attempt: 1,
		Volume: runtimecontract.WorkspaceVolumeInput{Profile: runtimecontract.WorkspaceVolumeLocalPathProfile,
			Scope:        runtimecontract.WorkspaceStorageScope{OrganizationRef: "org_fixture000", ProjectRef: "prj_fixture000", SessionRef: "ses_fixture000", AgentRef: "agt_fixture000"},
			WorkspaceRef: "wks_fixture000", VolumeGeneration: 1, CreateTaskRef: "svt_create0000"}}
	digest, err := work.Digest()
	if err != nil {
		t.Fatal("claim fixture digest rejected")
	}
	return &cp.WorkspaceVolumeTask{TaskRef: work.TaskRef, Kind: cp.WorkspaceVolumeTaskKind_WORKSPACE_VOLUME_TASK_KIND_CREATE,
		Attempt: 1, InputDigest: digest, Volume: &cp.WorkspaceVolumeInput{Profile: work.Volume.Profile, WorkspaceRef: work.Volume.WorkspaceRef,
			VolumeGeneration: 1, CreateTaskRef: work.TaskRef, Scope: &cp.RuntimeWorkspaceScope{OrganizationRef: work.Volume.Scope.OrganizationRef,
				ProjectRef: work.Volume.Scope.ProjectRef, SessionRef: work.Volume.Scope.SessionRef, AgentRef: work.Volume.Scope.AgentRef}},
		Lease: &cp.WorkLease{Ref: "lea_fixture000", Fence: "fnc_fixture000", Generation: 1, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}}
}

// TestVolumeAcknowledgementRetryNeverRepeatsPhysicalCreate verifies one physical action and
// byte-equivalent owner requests across a lost ACK; changed keys or rerunning Create are failures.
// TestVolumeAcknowledgementRetryNeverRepeatsPhysicalCreate проверяет один physical action и
// byte-equivalent owner requests при lost ACK; changed keys либо повтор Create являются ошибкой.
func TestVolumeAcknowledgementRetryNeverRepeatsPhysicalCreate(t *testing.T) {
	claim := volumeClaimFixture(t)
	owner := &volumeOwnerFixture{lease: claim.GetLease(), uncertain: true}
	executor := &volumeExecutorFixture{}
	if err := processWorkspaceVolume(t.Context(), owner, executor, claim, newArchiveMetrics(), time.Second); err != nil {
		t.Fatal("exact terminal acknowledgement did not recover")
	}
	if executor.creates != 1 || owner.renewals != 1 || len(owner.complete) != 2 ||
		!proto.Equal(owner.complete[0], owner.complete[1]) || owner.complete[0].GetMutation().GetIdempotencyKey() == "" {
		t.Fatal("lost owner ACK repeated the physical action or changed its receipt")
	}
	claim.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
	if err := processWorkspaceVolume(t.Context(), owner, executor, claim, newArchiveMetrics(), time.Second); err == nil || executor.creates != 1 || owner.renewals != 1 {
		t.Fatal("expired claim entered another physical effect")
	}
}

// TestVolumeReadinessCannotMaskAnotherFailedPath preserves legacy error reasons and requires
// both working paths when V2 is selected, including recovery in either update order.
// TestVolumeReadinessCannotMaskAnotherFailedPath сохраняет legacy error reasons и требует оба
// working paths при V2, включая recovery в любом порядке updates.
func TestVolumeReadinessCannotMaskAnotherFailedPath(t *testing.T) {
	readiness := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics("session_archive", "fixture", map[string]string{})
	state := newArchiveWorkerReadiness(readiness, metrics, true)
	state.setArchive(true, "ready")
	if ready, reason := readiness.Ready(); ready || reason != "workspace_volume_unchecked" {
		t.Fatal("archive success hid an untested physical path")
	}
	state.setVolume(true, "ready")
	if ready, _ := readiness.Ready(); !ready {
		t.Fatal("both successful working paths remained unavailable")
	}
	state.setArchive(false, "kubernetes_unavailable")
	state.setVolume(true, "ready")
	if ready, reason := readiness.Ready(); ready || reason != "kubernetes_unavailable" {
		t.Fatal("volume success masked archive failure or changed its reason")
	}
	state.setVolume(false, "control_plane_unavailable")
	state.setArchive(true, "ready")
	if ready, reason := readiness.Ready(); ready || reason != "control_plane_unavailable" {
		t.Fatal("archive recovery masked physical authority failure")
	}
}

// TestVolumeProfileRequiresExplicitQualifiedGeometry keeps the flag off by default and rejects
// a foreign/default class, an increased size or an unknown profile before controller startup.
// TestVolumeProfileRequiresExplicitQualifiedGeometry оставляет flag выключенным по умолчанию и
// отклоняет foreign/default class, increased size либо unknown profile до controller startup.
func TestVolumeProfileRequiresExplicitQualifiedGeometry(t *testing.T) {
	config := validTestConfig()
	if config.WorkspaceStorageProfile != "" || config.validate() != nil {
		t.Fatal("legacy fixture changed its default profile")
	}
	config.WorkspaceStorageProfile = runtimecontract.WorkspaceStorageProfile
	if config.validate() == nil {
		t.Fatal("implicit default storage class entered the physical profile")
	}
	config.StorageClass = "local-path"
	if config.validate() != nil {
		t.Fatal("qualified physical geometry rejected")
	}
	config.SessionPVCSize = "21Gi"
	if config.validate() == nil {
		t.Fatal("physical profile expanded the prior storage budget")
	}
}
