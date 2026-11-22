// Package controlplane adapts fixed protected capture RPCs without exposing generated messages.
// Пакет controlplane адаптирует fixed protected capture RPC без выдачи generated messages.
package controlplane

import (
	"context"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/capture"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Capture binds the existing fixed client connection; it never invokes generic artifact upload.
// Capture связывает existing fixed client connection; generic artifact upload не вызывает.
type Capture struct {
	client cp.ManagedKubernetesOwnerServiceClient
}

// NewCapture requires a caller-created protected owner client without starting RPCs or workers.
// NewCapture принимает caller-created protected owner client без запуска RPC либо workers.
func NewCapture(client cp.ManagedKubernetesOwnerServiceClient) *Capture {
	return &Capture{client: client}
}

// captureWireKnown refuses unknown fields before a positive response is interpreted.
// captureWireKnown отклоняет unknown fields до интерпретации positive response.
func captureWireKnown(message proto.Message) bool {
	return message != nil && message.ProtoReflect().IsValid() && len(message.ProtoReflect().GetUnknown()) == 0
}

// captureBindingToProto preserves the exact independently issued task; its fence is never logged.
// captureBindingToProto сохраняет exact independently issued task; fence не записывается в logs.
func captureBindingToProto(binding managedkubernetes.CaptureTaskBinding) *cp.ManagedKubernetesCaptureTaskBinding {
	return &cp.ManagedKubernetesCaptureTaskBinding{CaptureRef: binding.CaptureRef, LeaseRef: binding.LeaseRef, Fence: binding.Fence, Generation: binding.Generation, WorkloadInstance: binding.WorkloadInstance, SnapshotDigest: binding.SnapshotDigest}
}

// captureResponse decodes the closed receipt and uses the shared exact-binding validator.
// captureResponse декодирует closed receipt и использует shared exact-binding validator.
func captureResponse(input *cp.ManagedKubernetesCaptureReceipt, binding managedkubernetes.CaptureTaskBinding) (managedkubernetes.CaptureTaskReceipt, error) {
	var result managedkubernetes.CaptureTaskReceipt
	if !captureWireKnown(input) {
		return result, capture.ErrMalformed
	}
	result = managedkubernetes.CaptureTaskReceipt{CaptureRef: input.GetCaptureRef(), StreamRef: input.GetStreamRef(), SnapshotDigest: input.GetSnapshotDigest(), TaskBindingDigest: input.GetTaskBindingDigest(), TaskGeneration: input.GetTaskGeneration(), CaptureGeneration: input.GetCaptureGeneration(), ReasonCode: input.GetReasonCode()}
	switch input.GetState() {
	case cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_READY:
		result.State = managedkubernetes.CaptureReady
	case cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_CLAIMED:
		result.State = managedkubernetes.CaptureClaimed
	case cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_COMPLETE:
		result.State = managedkubernetes.CaptureComplete
	case cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_UNKNOWN:
		result.State = managedkubernetes.CaptureUnknown
	case cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_REJECTED:
		result.State = managedkubernetes.CaptureRejected
	case cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_WAITING_OWNER:
		result.State = managedkubernetes.CaptureWaitingOwner
	case cp.ManagedKubernetesCaptureState_MANAGED_KUBERNETES_CAPTURE_STATE_SUPERSEDED:
		result.State = managedkubernetes.CaptureSuperseded
	default:
		return result, capture.ErrMalformed
	}
	switch input.GetKind() {
	case cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_UNSPECIFIED:
	case cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_EMPTY:
		result.Kind = managedkubernetes.CaptureEmpty
	case cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_FULL:
		result.Kind = managedkubernetes.CaptureFull
	case cp.ManagedKubernetesCaptureKind_MANAGED_KUBERNETES_CAPTURE_KIND_PARTIAL:
		result.Kind = managedkubernetes.CapturePartial
	default:
		return result, capture.ErrMalformed
	}
	if len(input.GetArtifacts()) > 5 {
		return result, capture.ErrMalformed
	}
	for _, artifact := range input.GetArtifacts() {
		if !captureWireKnown(artifact) {
			return result, capture.ErrMalformed
		}
		result.Artifacts = append(result.Artifacts, managedkubernetes.CapturedArtifactReceipt{Channel: artifact.GetChannel(), ArtifactRef: artifact.GetArtifactRef(), RevisionRef: artifact.GetRevisionRef(), Digest: artifact.GetDigest(), SizeBytes: artifact.GetSizeBytes(), CaptureGeneration: artifact.GetCaptureGeneration(), Partial: artifact.GetPartial()})
	}
	if result.ValidateFor(binding) != nil {
		return managedkubernetes.CaptureTaskReceipt{}, capture.ErrMalformed
	}
	return result, nil
}

// captureRPCError keeps only closed outcome classes, excluding raw upstream status diagnostics.
// captureRPCError сохраняет только closed outcome classes без raw upstream status diagnostics.
func captureRPCError(err error) error {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled, codes.Unknown, codes.Internal, codes.DataLoss:
		return capture.ErrUncertain
	default:
		return capture.ErrLostClaim
	}
}

// Check proves exact contract revision1 before the job claims work or emits its heartbeat.
// Check проверяет exact contract revision1 до job claim либо heartbeat.
func (owner *Capture) Check(ctx context.Context) (uint32, error) {
	if owner == nil || owner.client == nil {
		return 0, capture.ErrLostClaim
	}
	response, err := owner.client.CheckManagedKubernetesCaptureReadiness(ctx, &cp.CheckManagedKubernetesCaptureReadinessRequest{})
	if err != nil {
		return 0, captureRPCError(err)
	}
	if !captureWireKnown(response) || response.GetContractRevision() != 0 && response.GetContractRevision() != managedkubernetes.CaptureContractRevision || response.GetReady() != (response.GetContractRevision() == managedkubernetes.CaptureContractRevision) {
		return 0, capture.ErrMalformed
	}
	return response.GetContractRevision(), nil
}

// Finalize sends only original task metadata once, without any caller-proposed bytes or outcome.
// Finalize отправляет original task metadata один раз, без caller-proposed bytes либо outcome.
func (owner *Capture) Finalize(ctx context.Context, binding managedkubernetes.CaptureTaskBinding) (managedkubernetes.CaptureTaskReceipt, error) {
	if owner == nil || owner.client == nil || binding.Validate() != nil {
		return managedkubernetes.CaptureTaskReceipt{}, capture.ErrMalformed
	}
	response, err := owner.client.FinalizeManagedKubernetesCapture(ctx, &cp.FinalizeManagedKubernetesCaptureRequest{Binding: captureBindingToProto(binding)})
	if err != nil {
		return managedkubernetes.CaptureTaskReceipt{}, captureRPCError(err)
	}
	if !captureWireKnown(response) {
		return managedkubernetes.CaptureTaskReceipt{}, capture.ErrMalformed
	}
	return captureResponse(response.GetReceipt(), binding)
}

// Read recovers the original task's metadata without body replay or a new task grant.
// Read сверяет original task metadata без body replay либо нового task grant.
func (owner *Capture) Read(ctx context.Context, binding managedkubernetes.CaptureTaskBinding) (managedkubernetes.CaptureTaskReceipt, error) {
	if owner == nil || owner.client == nil || binding.Validate() != nil {
		return managedkubernetes.CaptureTaskReceipt{}, capture.ErrMalformed
	}
	response, err := owner.client.GetManagedKubernetesCapture(ctx, &cp.GetManagedKubernetesCaptureRequest{Binding: captureBindingToProto(binding)})
	if err != nil {
		return managedkubernetes.CaptureTaskReceipt{}, captureRPCError(err)
	}
	if !captureWireKnown(response) {
		return managedkubernetes.CaptureTaskReceipt{}, capture.ErrMalformed
	}
	return captureResponse(response.GetReceipt(), binding)
}

// Renew accepts only the same attempt reference/generation and a finite future task deadline.
// Renew принимает только same attempt reference/generation и finite future task deadline.
func (owner *Capture) Renew(ctx context.Context, binding managedkubernetes.CaptureTaskBinding) (time.Time, error) {
	if owner == nil || owner.client == nil || binding.Validate() != nil {
		return time.Time{}, capture.ErrMalformed
	}
	response, err := owner.client.RenewManagedKubernetesCapture(ctx, &cp.RenewManagedKubernetesCaptureRequest{Binding: captureBindingToProto(binding)})
	if err != nil {
		return time.Time{}, captureRPCError(err)
	}
	if !captureWireKnown(response) || response.GetLeaseRef() != binding.LeaseRef || response.GetGeneration() != binding.Generation || !captureWireKnown(response.GetExpiresAt()) || response.GetExpiresAt().CheckValid() != nil {
		return time.Time{}, capture.ErrMalformed
	}
	expires := response.GetExpiresAt().AsTime()
	if !expires.After(time.Now()) || expires.After(time.Now().Add(301*time.Second)) {
		return time.Time{}, capture.ErrLostClaim
	}
	return expires, nil
}
