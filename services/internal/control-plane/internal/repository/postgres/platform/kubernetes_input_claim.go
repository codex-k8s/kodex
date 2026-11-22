package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// lockKubernetesInputClaim rechecks the original live claim, child and immutable source tuple.
// It issues neither a frame nor a native write admission.
// lockKubernetesInputClaim проверяет original live claim, child и immutable source tuple.
// Не создаёт frame либо native write admission.
func (repository *Repository) lockKubernetesInputClaim(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, streamRef string) (kubernetesInputRecord, kubernetesInputStage, error) {
	var empty kubernetesInputRecord
	var stage kubernetesInputStage
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		return empty, stage, err
	}
	record, err := readKubernetesInputRecord(ctx, tx, current.organizationID, binding.InvocationRef, streamRef)
	if err != nil {
		return record, stage, err
	}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil || digest != record.bindingDigest {
		return record, stage, errs.ErrForbidden
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil || snapshot != record.snapshotDigest || binding.Generation != record.view.Generation {
		return record, stage, errs.ErrForbidden
	}
	if record.view.State != "ACTIVE" || record.invocationState != "RUNNING" {
		return record, stage, errs.ErrForbidden
	}
	var allowed bool
	if err := tx.QueryRow(ctx, queryKubernetesInputCurrent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": record.id}).Scan(&allowed); err != nil {
		return record, stage, errs.ErrUnavailable
	}
	if !allowed {
		return record, stage, errs.ErrForbidden
	}
	if owner.lease.ExpiresAt.Before(record.ceiling) {
		record.ceiling = owner.lease.ExpiresAt
	}
	if record.view.HardDeadline.Before(record.ceiling) {
		record.ceiling = record.view.HardDeadline
	}
	if record.view.Operation == "kubernetes.cp.upload" {
		if _, _, err := repository.currentKubernetesFileStreamTx(ctx, tx, current, binding, &record, time.Time{}); err != nil {
			return record, stage, err
		}
	}
	stage, err = readKubernetesInputStage(ctx, tx, current.organizationID, record.id, "", "", record.view)
	return record, stage, err
}

// ClaimManagedKubernetesInputFrame commits the only delivery attempt before one version-pinned read.
// A repeated/lost claim returns UNKNOWN without bytes; no stream sequence or native effect is replayed.
// ClaimManagedKubernetesInputFrame фиксирует единственную delivery attempt до одного version-pinned read.
// Повторный/lost claim возвращает UNKNOWN без bytes; sequence либо native effect не повторяются.
func (repository *Repository) ClaimManagedKubernetesInputFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, streamRef string) (result entity.ManagedKubernetesInputFrameAdmission, resultErr error) {
	if err := kubernetesInvocationGatewayCaller(p, binding, "platform.runtime.kubernetes.input.claim"); err != nil {
		return result, err
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	firstCeiling, err := repository.stageKubernetesFileNextInput(ctx, current, binding, streamRef)
	if err != nil {
		return result, err
	}
	if err := kubernetesInputClaimCreditError(ctx, firstCeiling); err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	record, stage, err := repository.lockKubernetesInputClaim(ctx, tx, current, binding, streamRef)
	if err != nil {
		return result, err
	}
	if firstCeiling.Before(record.ceiling) {
		record.ceiling = firstCeiling
	}
	if err := kubernetesInputClaimCreditError(ctx, record.ceiling); err != nil {
		return result, err
	}
	result.Disposition = enum.KubernetesInputFrameNone
	if stage.id == "" || stage.metadata.State == enum.KubernetesInputAcknowledged {
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		if err := kubernetesInputClaimCreditError(ctx, record.ceiling); err != nil {
			return entity.ManagedKubernetesInputFrameAdmission{}, err
		}
		return result, nil
	}
	if stage.metadata.State != enum.KubernetesInputQueued {
		result.Disposition = enum.KubernetesInputFrameUnknown
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		if err := kubernetesInputClaimCreditError(ctx, record.ceiling); err != nil {
			return entity.ManagedKubernetesInputFrameAdmission{}, err
		}
		return result, nil
	}
	var fileRange objectstorage.RangeRead
	if stage.metadata.Channel == "FILE_CONTENT" {
		fileRange, err = kubernetesFileRangeTx(ctx, tx, current.organizationID, record.id, stage, record.fileSource)
		if err != nil {
			return result, err
		}
	}
	if err := validateKubernetesPortForwardStageTx(ctx, tx, current.organizationID, record.id, stage, nil); err != nil {
		return result, err
	}
	until := record.now.UTC().Truncate(time.Microsecond).Add(5 * time.Second)
	if record.ceiling.Before(until) {
		until = record.ceiling.UTC().Truncate(time.Microsecond)
	}
	if !until.After(record.now) {
		return result, errs.ErrForbidden
	}
	tag, err := tx.Exec(ctx, queryKubernetesInputClaim, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stage_id": stage.id, "delivery_expires_at": until})
	if err != nil {
		return result, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if err := kubernetesInputClaimCreditError(ctx, until); err != nil {
		return entity.ManagedKubernetesInputFrameAdmission{}, err
	}
	// One committed attempt precedes the GET; read/close failures cannot reopen that attempt.
	// Одна committed attempt предшествует GET; read/close failure не открывает её повторно.
	readCtx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	payload, readErr := repository.readKubernetesInputPayload(readCtx, stage, fileRange)
	if readErr != nil {
		clear(payload)
		repository.markKubernetesInputUnknown(ctx, current.organizationID, record.id, stage.id)
		return entity.ManagedKubernetesInputFrameAdmission{Disposition: enum.KubernetesInputFrameUnknown}, nil
	}
	// A second owner read checks revocation and the same original source deadline before releasing bytes.
	// Вторая owner read проверяет revoke и тот же original source deadline до выдачи bytes.
	verify, err := repository.pool.BeginTx(readCtx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		clear(payload)
		return result, errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = verify.Rollback(cleanup)
	}()
	fresh, latest, err := repository.lockKubernetesInputClaim(readCtx, verify, current, binding, streamRef)
	originalMetadata, latestMetadata := stage.metadata, latest.metadata
	originalMetadata.State, latestMetadata.State = "", ""
	if err != nil || originalMetadata != latestMetadata || latest.key != stage.key || latest.version != stage.version || latest.etag != stage.etag || latest.submissionKey != stage.submissionKey || latest.intentDigest != stage.intentDigest || latest.id != stage.id || latest.metadata.State != enum.KubernetesInputClaimed || latest.deadline == nil || !latest.deadline.Equal(until) || !until.After(fresh.now) || fresh.ceiling.Before(until) {
		clear(payload)
		if err != nil {
			return result, err
		}
		return result, errs.ErrForbidden
	}
	input := command.KubernetesStreamInputSubmit{Channel: stage.metadata.Channel, Payload: payload, PayloadDigest: stage.metadata.PayloadDigest, EndOfInput: stage.metadata.EndOfInput}
	if input.Channel == "FILE_CONTENT" {
		if fresh.view.Operation != "kubernetes.cp.upload" {
			clear(payload)
			return result, errs.ErrForbidden
		}
		if err := validateKubernetesFileStageTx(readCtx, verify, current.organizationID, fresh.id, latest, fresh.fileSource); err != nil {
			clear(payload)
			return result, err
		}
	} else if err := validateKubernetesInputSource(fresh, input); err != nil {
		clear(payload)
		return result, err
	}

	if err := validateKubernetesPortForwardStageTx(readCtx, verify, current.organizationID, fresh.id, latest, payload); err != nil {
		clear(payload)
		return result, err
	}
	frame := managedkubernetes.InputFrameDelivery{Ref: stage.metadata.Ref, InvocationRef: binding.InvocationRef, StreamRef: streamRef, Generation: binding.Generation, Sequence: stage.metadata.Sequence, Channel: stage.metadata.Channel, PayloadDigest: stage.metadata.PayloadDigest, ByteCount: stage.metadata.ByteCount, EndOfInput: stage.metadata.EndOfInput, Payload: bytes.Clone(payload), DeliveryExpiresAt: until, BindingDigest: record.bindingDigest}
	clear(payload)
	if frame.Validate(binding, streamRef, fresh.now) != nil {
		clear(frame.Payload)
		return result, errs.ErrUnavailable
	}
	if verify.Commit(readCtx) != nil {
		clear(frame.Payload)
		return result, errs.ErrUnavailable
	}
	if readCtx.Err() != nil {
		clear(frame.Payload)
		return result, errs.ErrUnavailable
	}
	if !until.After(time.Now()) {
		clear(frame.Payload)
		return result, errs.ErrForbidden
	}
	result.Disposition, result.Frame = enum.KubernetesInputFrameDeliver, &frame
	return result, nil
}

// kubernetesInputClaimCreditError rejects expired first-transaction horizons for data and metadata recovery alike.
// It checks only the already resolved absolute credit; it never refreshes permission or grants an input attempt.
// kubernetesInputClaimCreditError отклоняет expired first-transaction horizons для data и metadata recovery.
// Проверяет лишь уже разрешённый absolute credit; не обновляет permission и не выдаёт input attempt.
func kubernetesInputClaimCreditError(ctx context.Context, until time.Time) error {
	if ctx.Err() != nil || until.IsZero() {
		return errs.ErrUnavailable
	}
	if !until.After(time.Now()) {
		return errs.ErrForbidden
	}
	return nil
}

// markKubernetesInputUnknown preserves an unsuccessful single delivery attempt without queuing a retry.
// markKubernetesInputUnknown сохраняет unsuccessful single delivery attempt без постановки retry.
func (repository *Repository) markKubernetesInputUnknown(ctx context.Context, org, stream, id string) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, err := repository.pool.Begin(cleanup)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(cleanup) }()
	if _, err := tx.Exec(cleanup, queryKubernetesInputStreamLock, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream}); err != nil {
		return
	}
	if _, err := tx.Exec(cleanup, queryKubernetesInputUnknown, pgx.StrictNamedArgs{"organization_id": org, "stage_id": id}); err != nil {
		return
	}
	_ = tx.Commit(cleanup)
}

// readKubernetesInputPayload reads one committed delivery only; FILE uses its exact source Range and never an object PUT/Get.
// The caller must persist the one-attempt Claim first and recheck current owner authority before releasing returned bytes.
// readKubernetesInputPayload читает committed delivery; FILE использует exact source Range без object PUT/Get.
// Caller сначала фиксирует one-attempt Claim и проверяет current owner authority до выдачи возвращённых bytes.
func (repository *Repository) readKubernetesInputPayload(ctx context.Context, stage kubernetesInputStage, fileRange objectstorage.RangeRead) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	var payload []byte
	var readErr error
	if stage.metadata.Channel == "FILE_CONTENT" {
		if stage.key != "" || stage.version != "" || stage.etag != "" || fileRange.Validate() != nil || fileRange.Length != int64(stage.metadata.ByteCount) || fileRange.Digest != "sha256:"+stage.metadata.PayloadDigest {
			return nil, errs.ErrUnavailable
		}
		reader, ok := repository.objects.(objectstorage.RangeReader)
		if !ok {
			readErr = errs.ErrUnavailable
		} else {
			payload, readErr = reader.ReadRange(ctx, fileRange)
			if readErr == nil {
				sum := sha256.Sum256(payload)
				if len(payload) != int(stage.metadata.ByteCount) || hex.EncodeToString(sum[:]) != stage.metadata.PayloadDigest {
					readErr = errs.ErrUnavailable
				}
			}
		}
	} else {
		object, objectErr := repository.objects.Get(ctx, stage.key, stage.version)
		readErr = objectErr
		if readErr != nil && object.Body != nil {
			_ = object.Body.Close()
		}
		if readErr == nil && object.Body == nil {
			readErr = errs.ErrUnavailable
		}
		if readErr == nil {
			payload, readErr = io.ReadAll(io.LimitReader(object.Body, int64(stage.metadata.ByteCount)+1))
			closeErr := object.Body.Close()
			if readErr == nil {
				readErr = closeErr
			}
			sum := sha256.Sum256(payload)
			if object.Key != stage.key || object.VersionID != stage.version || object.ETag != stage.etag || object.SizeBytes != int64(stage.metadata.ByteCount) || object.Digest != "sha256:"+stage.metadata.PayloadDigest || len(payload) != int(stage.metadata.ByteCount) || hex.EncodeToString(sum[:]) != stage.metadata.PayloadDigest {
				readErr = errs.ErrUnavailable
			}
		}
	}
	if readErr == nil && ctx.Err() != nil {
		readErr = ctx.Err()
	}
	if readErr != nil {
		clear(payload)
		return nil, errs.ErrUnavailable
	}
	return payload, nil
}
