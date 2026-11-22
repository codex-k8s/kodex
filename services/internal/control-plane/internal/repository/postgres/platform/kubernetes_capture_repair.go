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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesCaptureRepairOperation scopes the immutable public receipt to a repair, never native execution.
// kubernetesCaptureRepairOperation ограничивает immutable public receipt восстановлением, не native execution.
const kubernetesCaptureRepairOperation = "platform.integrations.kubernetes.capture.repair"

// queryKubernetesCapturePublicOrigin rejoins original actor and project through the existing execution graph.
// queryKubernetesCapturePublicOrigin разрешает original actor и project через existing execution graph.
//
//go:embed sql/kubernetes_capture__public_origin.sql
var queryKubernetesCapturePublicOrigin string

// queryKubernetesCapturePublicRead locks current capture metadata after the root and stream locks.
// queryKubernetesCapturePublicRead блокирует current capture metadata после root и stream locks.
//
//go:embed sql/kubernetes_capture__public_read.sql
var queryKubernetesCapturePublicRead string

// queryKubernetesCaptureRepair opens one new bounded retry cycle while preserving total attempt history.
// queryKubernetesCaptureRepair открывает один bounded retry cycle с сохранением полной истории attempts.
//
//go:embed sql/kubernetes_capture__repair.sql
var queryKubernetesCaptureRepair string

// kubernetesCapturePublicRecord keeps owner-only source pins separate from the public status.
// kubernetesCapturePublicRecord отделяет owner-only source pins от public status.
type kubernetesCapturePublicRecord struct {
	task  kubernetesCaptureTaskRecord
	view  entity.KubernetesCaptureStatus
	now   time.Time
	until time.Time
}

// lockPublicKubernetesCapture resolves immutable provenance before current visibility and any OCC or
// idempotency checks. Integration/native credentials are unnecessary for retained historical output.
// lockPublicKubernetesCapture разрешает immutable provenance до current visibility и любых OCC либо
// idempotency checks. Для сохранённой истории не нужны integration/native credentials.
func (repository *Repository) lockPublicKubernetesCapture(ctx context.Context, tx pgx.Tx, current scope, p value.Principal, ref string) (kubernetesCapturePublicRecord, error) {
	var result kubernetesCapturePublicRecord
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "project_ref": p.ProjectRef, "capture_ref": ref}
	var id, streamID, rootID, rootRef, projectRef string
	readOrigin := func() error {
		err := tx.QueryRow(ctx, queryKubernetesCapturePublicOrigin, args).Scan(&id, &streamID, &rootID, &rootRef, &projectRef)
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		}
		if err != nil {
			return errs.ErrUnavailable
		}
		return nil
	}
	if err := readOrigin(); err != nil {
		return result, err
	}
	if _, err := lockOwnerGateRootTx(ctx, tx, rootID); err != nil {
		return result, err
	}
	var locked string
	if err := tx.QueryRow(ctx, queryKubernetesCaptureStreamLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": streamID}).Scan(&locked); err != nil || locked != streamID {
		return result, errs.ErrUnavailable
	}
	if err := readOrigin(); err != nil {
		return result, err
	}
	until, err := repository.requireAccessUntil(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: rootRef, ProjectRef: projectRef})
	if err != nil {
		return result, concealAccessDenial(err, errs.ErrNotFound)
	}
	result.until = until
	var raw, digest, state, kind string
	v := &result.view
	if err := tx.QueryRow(ctx, queryKubernetesCapturePublicRead, pgx.StrictNamedArgs{"organization_id": current.organizationID, "capture_id": id}).Scan(&raw, &digest, &v.CaptureGeneration, &v.Version, &state, &kind, &v.ReasonCode, &v.AttemptCount, &v.RepairGeneration, &v.RetainUntil, &result.now); err != nil {
		return result, errs.ErrUnavailable
	}
	sum := sha256.Sum256([]byte(raw))
	source := &result.task.source
	if len(raw) > 16384 || hex.EncodeToString(sum[:]) != digest || internalrpcauth.DecodeStrictJSON([]byte(raw), source) != nil || !validKubernetesCaptureSource(*source) || source.RootActorID != current.actorID || source.OrganizationID != current.organizationID || source.RootRunID != rootID || source.RootRunRef != rootRef || source.ProjectRef != projectRef || source.CaptureGeneration != v.CaptureGeneration {
		return kubernetesCapturePublicRecord{}, errs.ErrUnavailable
	}
	v.CaptureRef = ref
	v.StreamRef = source.StreamRef
	v.InvocationRef = source.InvocationRef
	v.RunRef = source.RunRef
	v.NodeRef = source.NodeRef
	v.Operation = source.Operation
	v.SealedAt = source.SealedAt
	v.State = enum.ManagedKubernetesCaptureState(state)
	v.Kind = enum.ManagedKubernetesCaptureKind(kind)
	if !knownKubernetesCaptureState(v.State) || !managedkubernetes.ValidCaptureReason(v.ReasonCode) || v.Version < 1 || v.AttemptCount < 0 || v.RepairGeneration < 0 {
		return kubernetesCapturePublicRecord{}, errs.ErrUnavailable
	}
	if (v.State == enum.KubernetesCaptureComplete) != (v.Kind != "") {
		return kubernetesCapturePublicRecord{}, errs.ErrUnavailable
	}
	result.task.id = id
	result.task.organizationID = current.organizationID
	result.task.streamID = streamID
	if err := readKubernetesCaptureArtifacts(ctx, tx, &result.task); err != nil {
		return kubernetesCapturePublicRecord{}, err
	}
	v.Artifacts = result.task.receipt.Artifacts
	v.Cleanup, err = readKubernetesStagingCleanup(ctx, tx, current.organizationID, streamID)
	if err != nil {
		return kubernetesCapturePublicRecord{}, err
	}
	result.until, err = repository.kubernetesCaptureViewAccessUntil(ctx, tx, current, *source, result.until)
	if err != nil {
		return kubernetesCapturePublicRecord{}, err
	}
	return result, nil
}

// readKubernetesCaptureRepairReceipt recovers metadata for the original actor/key without retrying
// mutation or exposing a foreign capture. Persisted receipt shape is checked before use.
// readKubernetesCaptureRepairReceipt восстанавливает metadata original actor/key без повторной
// mutation либо раскрытия чужого capture. Persisted receipt shape проверяется до использования.
func readKubernetesCaptureRepairReceipt(ctx context.Context, tx pgx.Tx, current scope, ref, key string) (*entity.KubernetesCaptureRepairReceipt, error) {
	if key == "" {
		return nil, nil
	}
	var digest string
	var raw []byte
	err := tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, kubernetesCaptureRepairOperation, key).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var receipt entity.KubernetesCaptureRepairReceipt
	if len(raw) > 4096 || !managedkubernetes.ValidDigest(digest) || internalrpcauth.DecodeStrictJSON(raw, &receipt) != nil || receipt.RepairGeneration < 1 || receipt.Version < 2 || receipt.AttemptCount < 0 || receipt.AcceptedAt.IsZero() {
		return nil, errs.ErrUnavailable
	}
	if receipt.CaptureRef != ref {
		return nil, errs.ErrNotFound
	}
	return &receipt, nil
}

// GetKubernetesCaptureStatus reads current historical metadata and an optional original-key receipt.
// It never provides a task fence, native grant, payload, private storage locator or fresh delivery credit.
// GetKubernetesCaptureStatus читает current historical metadata и optional original-key receipt.
// Task fence, native grant, payload, private storage locator либо fresh delivery credit не выдаются.
func (repository *Repository) GetKubernetesCaptureStatus(ctx context.Context, p value.Principal, input query.KubernetesCaptureStatusRead) (result entity.KubernetesCaptureStatusReadback, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := kubernetesInputAPICaller(p, "platform.integrations.kubernetes.capture.get"); err != nil {
		return result, err
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
	receipt, err := readKubernetesCaptureRepairReceipt(ctx, tx, current, input.CaptureRef, input.RepairIdempotencyKey)
	if err != nil {
		return result, err
	}
	cleanupReceipt, err := readKubernetesStagingCleanupReceipt(ctx, tx, current, input.CaptureRef, input.CleanupIdempotencyKey)
	if err != nil {
		return result, err
	}
	until, err := repository.kubernetesCaptureViewAccessUntil(ctx, tx, current, record.task.source, record.until)
	if err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if accessDeadlineExpired(until) {
		return result, errs.ErrNotFound
	}
	return entity.KubernetesCaptureStatusReadback{View: record.view, RepairReceipt: receipt, CleanupReceipt: cleanupReceipt}, nil
}

// RepairKubernetesCapture records one audited retry cycle under the original root and current
// canonical artifact.upload permission. It changes no stream, invocation, prepared payload or gate;
// readback owns progress and no execution/turn event is emitted for this technical artifact retry.
// RepairKubernetesCapture сохраняет audited retry cycle для original root с current canonical
// artifact.upload permission. Stream, invocation, prepared payload и gate не меняются; прогресс
// читается через status, execution/turn event для технического artifact retry не публикуется.
func (repository *Repository) RepairKubernetesCapture(ctx context.Context, p value.Principal, mutation value.Mutation, input command.KubernetesCaptureRepair) (result entity.KubernetesCaptureStatusReadback, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := kubernetesInputAPICaller(p, kubernetesCaptureRepairOperation); err != nil {
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
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID, kubernetesCaptureRepairOperation, mutation.IdempotencyKey); err != nil {
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
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, current.organizationID, current.actorID, kubernetesCaptureRepairOperation, mutation.IdempotencyKey).Scan(&storedDigest, &stored)
	var receipt *entity.KubernetesCaptureRepairReceipt
	if err == nil {
		if storedDigest != intent {
			return result, errs.ErrIdempotencyReuse
		}
		receipt, err = readKubernetesCaptureRepairReceipt(ctx, tx, current, input.CaptureRef, mutation.IdempotencyKey)
		if err != nil {
			return result, err
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	} else {
		if record.view.Version != *mutation.ExpectedVersion {
			return result, errs.ErrVersionMismatch
		}
		if record.view.State != enum.KubernetesCaptureWaitingOwner {
			return result, errs.ErrConflict
		}
		receipt = &entity.KubernetesCaptureRepairReceipt{CaptureRef: input.CaptureRef}
		err = tx.QueryRow(ctx, queryKubernetesCaptureRepair, pgx.StrictNamedArgs{"organization_id": current.organizationID, "capture_id": record.task.id, "expected_version": *mutation.ExpectedVersion}).Scan(&receipt.RepairGeneration, &receipt.Version, &receipt.AttemptCount, &receipt.AcceptedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrConflict
		}
		if err != nil {
			return result, errs.ErrUnavailable
		}
		stored, err = json.Marshal(receipt)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		if _, err = tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, current.organizationID, current.actorID, kubernetesCaptureRepairOperation, mutation.IdempotencyKey, intent, "KUBERNETES_CAPTURE", stored); err != nil {
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
		if _, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, projectID, current.actorID, kubernetesCaptureRepairOperation, "KUBERNETES_CAPTURE", input.CaptureRef, "i18n:KUBERNETES_CAPTURE_REPAIR_QUEUED", current.correlationRef); err != nil {
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
	return entity.KubernetesCaptureStatusReadback{View: record.view, RepairReceipt: receipt}, nil
}

// kubernetesCaptureViewAccessUntil repeats current historical visibility after blocking reads,
// retaining the first allowance instead of extending it with a later grant or restored binding.
// kubernetesCaptureViewAccessUntil повторяет current historical visibility после blocking reads,
// сохраняя первый allowance без продления поздним grant либо восстановленным binding.
func (repository *Repository) kubernetesCaptureViewAccessUntil(ctx context.Context, tx pgx.Tx, current scope, source kubernetesCaptureSource, first time.Time) (time.Time, error) {
	if accessDeadlineExpired(first) {
		return time.Time{}, errs.ErrNotFound
	}
	fresh, err := repository.requireAccessUntil(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: source.RootRunRef, ProjectRef: source.ProjectRef})
	if err != nil {
		return time.Time{}, concealAccessDenial(err, errs.ErrNotFound)
	}
	until := earlierAccessDeadline(first, fresh)
	if accessDeadlineExpired(until) {
		return time.Time{}, errs.ErrNotFound
	}
	return until, nil
}

// kubernetesCaptureRepairAccessUntil requires both current view and upload permission, carrying
// the earliest admitted deadline through the idempotency wait, writes and COMMIT acknowledgement.
// kubernetesCaptureRepairAccessUntil требует current view и upload permission с сохранением
// earliest admitted deadline через idempotency wait, записи и COMMIT acknowledgement.
func (repository *Repository) kubernetesCaptureRepairAccessUntil(ctx context.Context, tx pgx.Tx, current scope, source kubernetesCaptureSource, first time.Time) (time.Time, error) {
	until, err := repository.kubernetesCaptureViewAccessUntil(ctx, tx, current, source, first)
	if err != nil {
		return time.Time{}, err
	}
	upload, err := repository.requireArtifactUploadAccessUntil(ctx, tx, current, source.ProjectRef)
	if err != nil {
		return time.Time{}, err
	}
	until = earlierAccessDeadline(until, upload)
	if accessDeadlineExpired(until) {
		return time.Time{}, errs.ErrNotFound
	}
	return until, nil
}
