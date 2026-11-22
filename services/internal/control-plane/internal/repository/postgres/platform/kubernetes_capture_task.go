package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureTaskRead resolves an independently issued task attempt, including old ACK
// readback. Tenant and source come from the exact task, never an organization in request payload.
// queryKubernetesCaptureTaskRead разрешает independently issued task attempt, включая old ACK
// readback. Tenant/source берутся из exact task, не organization из request payload.
//
//go:embed sql/kubernetes_capture__task_read.sql
var queryKubernetesCaptureTaskRead string

// queryKubernetesCaptureTaskRenew renews only a current attempt within its hard task deadline.
// queryKubernetesCaptureTaskRenew продлевает только current attempt до hard task deadline.
//
//go:embed sql/kubernetes_capture__task_renew.sql
var queryKubernetesCaptureTaskRenew string

// kubernetesCaptureTaskRecord separates immutable source metadata from current worker eligibility.
// It has no raw task or invocation fence, object locator or external output bytes.
// kubernetesCaptureTaskRecord отделяет immutable source metadata от current worker eligibility.
// Не содержит raw task/invocation fence, object locator либо external output bytes.
type kubernetesCaptureTaskRecord struct {
	id, organizationID, streamID, attemptID string
	source                                  kubernetesCaptureSource
	receipt                                 entity.ManagedKubernetesCaptureReceipt
	expires, hardDeadline, now              time.Time
	attemptState                            string
}

// kubernetesCaptureTaskArgs builds only exact task-issued lookup coordinates after syntax validation.
// kubernetesCaptureTaskArgs строит только exact task-issued lookup coordinates после syntax validation.
func kubernetesCaptureTaskArgs(binding value.ManagedKubernetesCaptureTaskBinding) (pgx.StrictNamedArgs, error) {
	if binding.Validate() != nil {
		return nil, errs.ErrInvalid
	}
	return pgx.StrictNamedArgs{"capture_ref": binding.CaptureRef, "lease_ref": binding.LeaseRef, "generation": binding.Generation, "workload_instance": binding.WorkloadInstance, "snapshot_digest": binding.SnapshotDigest, "fence_digest": binding.FenceDigest()}, nil
}

// readKubernetesCaptureTask verifies saved source bytes and all task/source incarnation pins before
// use. It returns no source body and requires an independently authorized artifact worker caller.
// readKubernetesCaptureTask проверяет saved source bytes и все task/source incarnation pins до
// использования. Не выдаёт source body и требует independently authorized artifact worker caller.
func readKubernetesCaptureTask(ctx context.Context, runner queryRunner, binding value.ManagedKubernetesCaptureTaskBinding) (kubernetesCaptureTaskRecord, error) {
	var result kubernetesCaptureTaskRecord
	args, err := kubernetesCaptureTaskArgs(binding)
	if err != nil {
		return result, err
	}
	var raw, state, kind string
	var sourceGeneration int64
	err = runner.QueryRow(ctx, queryKubernetesCaptureTaskRead, args).Scan(&result.id, &result.organizationID, &result.streamID, &result.attemptID, &raw, &result.receipt.SnapshotDigest, &result.receipt.CaptureGeneration, &sourceGeneration, &state, &kind, &result.receipt.ReasonCode, &result.attemptState, &result.expires, &result.hardDeadline, &result.now)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	sum := sha256.Sum256([]byte(raw))
	if len(raw) > 16384 || hex.EncodeToString(sum[:]) != binding.SnapshotDigest || result.receipt.SnapshotDigest != binding.SnapshotDigest || internalrpcauth.DecodeStrictJSON([]byte(raw), &result.source) != nil || result.source.Version != 1 || result.source.CaptureGeneration != result.receipt.CaptureGeneration || result.source.Generation != sourceGeneration || !validKubernetesCaptureSource(result.source) {
		return kubernetesCaptureTaskRecord{}, errs.ErrUnavailable
	}
	result.receipt.CaptureRef = binding.CaptureRef
	result.receipt.StreamRef = result.source.StreamRef
	result.receipt.TaskGeneration = binding.Generation
	result.receipt.TaskBindingDigest, _ = binding.Digest()
	result.receipt.State = enum.ManagedKubernetesCaptureState(state)
	result.receipt.Kind = enum.ManagedKubernetesCaptureKind(kind)
	if !knownKubernetesCaptureState(result.receipt.State) || !managedkubernetes.ValidCaptureReason(result.receipt.ReasonCode) {
		return kubernetesCaptureTaskRecord{}, errs.ErrUnavailable
	}
	return result, nil
}

// validKubernetesCaptureSource checks closed historical provenance without treating it as current
// native authority. Unknown identity scope is refused rather than inferred from missing credentials.
// validKubernetesCaptureSource проверяет closed historical provenance, не current native authority.
// Unknown identity scope отклоняется, не выводится из отсутствия credentials.
func validKubernetesCaptureSource(source kubernetesCaptureSource) bool {
	descriptor, known := integrationpackage.KubernetesOperationByKey(source.Operation)
	if !known || descriptor.Execution != integrationpackage.KubernetesStream {
		return false
	}
	for _, id := range []string{source.OrganizationID, source.RootActorID, source.RunID, source.RootRunID, source.NodeID, source.SubjectID} {
		if uuid.Validate(id) != nil {
			return false
		}
	}
	if source.ProjectID != "" && uuid.Validate(source.ProjectID) != nil || (source.ProjectID == "") != (source.ProjectRef == "") {
		return false
	}
	for _, digest := range []string{source.BindingDigest, source.SnapshotDigest, source.ChildInputDigest, source.PlanDigest, source.OriginalFenceDigest} {
		if !managedkubernetes.ValidDigest(digest) {
			return false
		}
	}
	for _, ref := range []string{source.OrganizationRef, source.RootActorRef, source.RunRef, source.RootRunRef, source.NodeRef, source.InvocationRef, source.StreamRef, source.SubjectRef, source.IdentityRef, source.PlanRef, source.TargetRef, source.ConnectionRef, source.EffectRef, source.OriginalLeaseRef} {
		if !managedkubernetes.ValidLocator(ref) || len(ref) > 96 {
			return false
		}
	}
	if source.Resource.Validate() != nil {
		return false
	}
	if source.LossBytes > source.MaximumTotalBytes || source.LossFacts > source.LossBytes {
		return false
	}
	if source.EffectGeneration < 1 || source.NativeTerminalKnown != (source.TerminalReceipt != nil) {
		return false
	}
	if source.TerminalReceipt != nil {
		receipt := source.TerminalReceipt
		digest, err := receipt.Digest()
		if err != nil || digest != source.TerminalDigest || receipt.TransportJoined != source.TransportJoined || receipt.Origin != (managedkubernetes.StreamReceiptOrigin{SnapshotDigest: source.SnapshotDigest, BindingDigest: source.BindingDigest, ChildInputDigest: source.ChildInputDigest, EffectRef: source.EffectRef, EffectGeneration: source.EffectGeneration, StreamRef: source.StreamRef, StreamGeneration: source.Generation, Operation: source.Operation, TargetRef: source.TargetRef, TargetRevision: source.TargetRevision}) {
			return false
		}
	}
	if source.Generation < 1 || source.CaptureGeneration < 1 || source.TargetRevision < 1 || source.IdentityRevision < 1 || source.MaximumTotalBytes == 0 || source.MaximumTotalBytes > uint64(maximumArtifactBytes) || source.OutputWatermark > managedkubernetes.MaximumFrameSequence || source.AcknowledgedWatermark > source.OutputWatermark || source.SealedAt.IsZero() || !enum.ManagedKubernetesStreamState(source.OwnerState).Terminal() || source.NativeTerminalKnown != (source.TerminalDigest != "") || source.TransportJoined && !source.NativeTerminalKnown || source.TerminalDigest != "" && !managedkubernetes.ValidDigest(source.TerminalDigest) {
		return false
	}
	return source.CredentialPolicy == "INITIATOR_REQUIRED" && source.SubjectKind == "USER" && source.SubjectID == source.RootActorID || source.CredentialPolicy == "PROJECT_SHARED" && source.SubjectKind == "SERVICE"
}

// knownKubernetesCaptureState keeps task status independent from native stream/invocation outcomes.
// knownKubernetesCaptureState сохраняет task status отдельно от native stream/invocation outcomes.
func knownKubernetesCaptureState(state enum.ManagedKubernetesCaptureState) bool {
	switch state {
	case enum.KubernetesCaptureReady, enum.KubernetesCaptureClaimed, enum.KubernetesCaptureComplete, enum.KubernetesCaptureUnknown, enum.KubernetesCaptureRejected, enum.KubernetesCaptureWaitingOwner, enum.KubernetesCaptureSuperseded:
		return true
	default:
		return false
	}
}

// kubernetesCaptureCaller verifies the fixed global background workload before resolving a task.
// Its DB-issued fence scopes tenant access; the job cannot choose an actor or organization.
// kubernetesCaptureCaller проверяет fixed global background workload до task resolution.
// DB-issued fence ограничивает tenant access; job не выбирает actor либо organization.
func (repository *Repository) kubernetesCaptureCaller(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding, permission string) error {
	if p.CallerWorkload != "artifact-retention" || p.Permission != permission || p.ProjectRef != "" {
		return errs.ErrForbidden
	}
	if binding.Validate() != nil {
		return errs.ErrInvalid
	}
	_, err := repository.resolveScope(ctx, p)
	return err
}

// GetManagedKubernetesCapture reads metadata for the original task attempt even after its lease
// closed. It issues no new artifact, source body or current attempt credit.
// GetManagedKubernetesCapture читает metadata original task attempt даже после lease closure.
// Не создаёт artifact, не выдаёт source body либо current attempt credit.
func (repository *Repository) GetManagedKubernetesCapture(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding) (entity.ManagedKubernetesCaptureReceipt, error) {
	if err := repository.kubernetesCaptureCaller(ctx, p, binding, "platform.artifacts.kubernetes-capture.get"); err != nil {
		return entity.ManagedKubernetesCaptureReceipt{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	record, err := readKubernetesCaptureTask(ctx, repository.pool, binding)
	if err != nil {
		return entity.ManagedKubernetesCaptureReceipt{}, err
	}
	if err := readKubernetesCaptureArtifacts(ctx, repository.pool, &record); err != nil {
		return entity.ManagedKubernetesCaptureReceipt{}, err
	}
	return record.receipt, nil
}

// RenewManagedKubernetesCapture keeps one artifact attempt alive within its independent hard
// deadline. It never renews an invocation, stream, original source or artifact visibility grant.
// RenewManagedKubernetesCapture продлевает одну artifact attempt до independent hard deadline.
// Не продлевает invocation/stream/original source либо artifact visibility grant.
func (repository *Repository) RenewManagedKubernetesCapture(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding) (time.Time, error) {
	if err := repository.kubernetesCaptureCaller(ctx, p, binding, "platform.artifacts.kubernetes-capture.renew"); err != nil {
		return time.Time{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args, err := kubernetesCaptureTaskArgs(binding)
	if err != nil {
		return time.Time{}, err
	}
	var expires time.Time
	err = repository.pool.QueryRow(ctx, queryKubernetesCaptureTaskRenew, args).Scan(&expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, errs.ErrForbidden
	}
	if err != nil {
		return time.Time{}, errs.ErrUnavailable
	}
	return expires, nil
}
