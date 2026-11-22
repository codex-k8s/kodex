package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesStagingCleanupRepairOperation separates storage cleanup from publication and native retry.
// kubernetesStagingCleanupRepairOperation отделяет storage cleanup от publication и native retry.
const kubernetesStagingCleanupRepairOperation = "platform.integrations.kubernetes.capture.cleanup.repair"

// queryKubernetesStagingCleanupStatus reads physical outstanding reservations under the stream lock.
// queryKubernetesStagingCleanupStatus читает physical outstanding reservations под stream lock.
//
//go:embed sql/kubernetes_capture__cleanup_status.sql
var queryKubernetesStagingCleanupStatus string

// queryKubernetesStagingCleanupRepair opens one bounded exhausted-item cycle without object effects.
// queryKubernetesStagingCleanupRepair открывает bounded exhausted-item cycle без object effects.
//
//go:embed sql/kubernetes_capture__cleanup_repair.sql
var queryKubernetesStagingCleanupRepair string

// readKubernetesStagingCleanup never counts terminal purged receipts against physical storage quota.
// The repair-only version does not claim to version independent worker progress.
// readKubernetesStagingCleanup не считает terminal purged receipts в physical storage quota.
// Версия explicit repair не обозначает version независимого worker progress.
func readKubernetesStagingCleanup(ctx context.Context, tx pgx.Tx, organizationID, streamID string) (*entity.KubernetesStagingCleanupStatus, error) {
	v := &entity.KubernetesStagingCleanupStatus{}
	err := tx.QueryRow(ctx, queryKubernetesStagingCleanupStatus, pgx.StrictNamedArgs{"organization_id": organizationID, "stream_id": streamID}).Scan(&v.Version, &v.RepairGeneration, &v.OutstandingCount, &v.OutstandingBytes, &v.ExhaustedCount, &v.EligibleExhaustedCount)
	if err != nil || v.Version < 1 || v.RepairGeneration < 0 || v.OutstandingCount < 0 || v.OutstandingBytes < 0 || v.ExhaustedCount < 0 || v.ExhaustedCount > v.OutstandingCount || v.EligibleExhaustedCount < 0 || v.EligibleExhaustedCount > v.ExhaustedCount {
		return nil, errs.ErrUnavailable
	}
	return v, nil
}

// readKubernetesStagingCleanupReceipt recovers only the original actor/key acknowledgement.
// It contains no object locator, native permission or successful-deletion assertion.
// readKubernetesStagingCleanupReceipt восстанавливает только original actor/key acknowledgement.
// Object locator, native permission либо подтверждение успешного удаления отсутствуют.
func readKubernetesStagingCleanupReceipt(ctx context.Context, tx pgx.Tx, current scope, ref, key string) (*entity.KubernetesStagingCleanupRepairReceipt, error) {
	if key == "" {
		return nil, nil
	}
	var digest string
	var raw []byte
	err := tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, kubernetesStagingCleanupRepairOperation, key).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var receipt entity.KubernetesStagingCleanupRepairReceipt
	if len(raw) > 4096 || !managedkubernetes.ValidDigest(digest) || internalrpcauth.DecodeStrictJSON(raw, &receipt) != nil || receipt.Version < 2 || receipt.RepairGeneration < 1 || receipt.RepairedCount < 1 || receipt.RepairedCount > 100 || receipt.RemainingExhaustedCount < 0 || receipt.AcceptedAt.IsZero() {
		return nil, errs.ErrUnavailable
	}
	if receipt.CaptureRef != ref {
		return nil, errs.ErrNotFound
	}
	return &receipt, nil
}

// RepairKubernetesStagingCleanup creates an audited new cleanup cycle for at most 100 exhausted,
// currently eligible immutable reservations. Total attempts and payload receipts remain unchanged;
// native execution and publication never reopen. A lost ACK uses one original-key metadata read.
// RepairKubernetesStagingCleanup создаёт audited cleanup cycle максимум для 100 exhausted,
// current eligible immutable reservations. Total attempts и payload receipts не меняются;
// native execution и publication не возобновляются. Lost ACK сверяется original-key metadata read.
func (repository *Repository) RepairKubernetesStagingCleanup(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesCaptureRepair) (result entity.KubernetesCaptureStatusReadback, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := kubernetesInputAPICaller(p, kubernetesStagingCleanupRepairOperation); err != nil {
		return result, err
	}
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 || mutation.IdempotencyKey == "" {
		return result, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	record, err := repository.lockPublicKubernetesCapture(ctx, tx, current, p, input.CaptureRef)
	if err != nil {
		return result, err
	}
	until, err := repository.kubernetesCaptureRepairAccessUntil(ctx, tx, current, record.task.source, record.until)
	if err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID, kubernetesStagingCleanupRepairOperation, mutation.IdempotencyKey); err != nil {
		return result, errs.ErrUnavailable
	}
	until, err = repository.kubernetesCaptureRepairAccessUntil(ctx, tx, current, record.task.source, until)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(struct {
		CaptureRef string
		Version    int64
	}{input.CaptureRef, *mutation.ExpectedVersion})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	intent := hex.EncodeToString(sum[:])
	var storedDigest string
	var stored []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, kubernetesStagingCleanupRepairOperation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	var receipt *entity.KubernetesStagingCleanupRepairReceipt
	if err == nil {
		if storedDigest != intent {
			return result, errs.ErrIdempotencyReuse
		}
		receipt, err = readKubernetesStagingCleanupReceipt(ctx, tx, current, input.CaptureRef, mutation.IdempotencyKey)
		if err != nil {
			return result, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	} else {
		if record.view.Cleanup == nil || record.view.Cleanup.Version != *mutation.ExpectedVersion {
			return result, errs.ErrVersionMismatch
		}
		if record.view.Cleanup.EligibleExhaustedCount == 0 {
			return result, errs.ErrConflict
		}
		receipt = &entity.KubernetesStagingCleanupRepairReceipt{CaptureRef: input.CaptureRef}
		err = tx.QueryRow(ctx, queryKubernetesStagingCleanupRepair, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": record.task.streamID, "expected_version": *mutation.ExpectedVersion}).Scan(&receipt.Version, &receipt.RepairGeneration, &receipt.RepairedCount, &receipt.AcceptedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrConflict
		}
		if err != nil {
			return result, errs.ErrUnavailable
		}
		status, err := readKubernetesStagingCleanup(ctx, tx, current.organizationID, record.task.streamID)
		if err != nil {
			return result, err
		}
		receipt.RemainingExhaustedCount = status.ExhaustedCount
		stored, err = json.Marshal(receipt)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		if _, err = tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, kubernetesStagingCleanupRepairOperation, mutation.IdempotencyKey, intent, "KUBERNETES_CAPTURE", stored); err != nil {
			return result, errs.ErrUnavailable
		}
		auditRef, err := newRef("aud")
		if err != nil {
			return result, errs.ErrUnavailable
		}
		var projectID any
		if record.task.source.ProjectID != "" {
			projectID = record.task.source.ProjectID
		}
		if _, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, projectID, current.actorID, kubernetesStagingCleanupRepairOperation, "KUBERNETES_CAPTURE", input.CaptureRef, "i18n:KUBERNETES_STAGING_CLEANUP_REPAIR_QUEUED", current.correlationRef); err != nil {
			return result, errs.ErrUnavailable
		}
		record, err = repository.lockPublicKubernetesCapture(ctx, tx, current, p, input.CaptureRef)
		if err != nil {
			return result, err
		}
	}
	until, err = repository.kubernetesCaptureRepairAccessUntil(ctx, tx, current, record.task.source, until)
	if err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if accessDeadlineExpired(until) {
		return result, errs.ErrNotFound
	}
	return entity.KubernetesCaptureStatusReadback{View: record.view, CleanupReceipt: receipt}, nil
}
