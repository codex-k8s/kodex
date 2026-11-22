package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCaptureArtifactReserve assigns one stable artifact per stream/channel before PUT.
// queryKubernetesCaptureArtifactReserve назначает один stable artifact на stream/channel до PUT.
//
//go:embed sql/kubernetes_capture__artifact_reserve.sql
var queryKubernetesCaptureArtifactReserve string

// queryKubernetesCaptureArtifactHead reads the exact current version before prepared-content staging.
// queryKubernetesCaptureArtifactHead читает exact current version до prepared-content staging.
//
//go:embed sql/kubernetes_capture__artifact_head.sql
var queryKubernetesCaptureArtifactHead string

// queryKubernetesCaptureArtifactCreate inserts canonical head/revision with deferred owner linkage.
// queryKubernetesCaptureArtifactCreate вставляет canonical head/revision с deferred owner linkage.
//
//go:embed sql/kubernetes_capture__artifact_create.sql
var queryKubernetesCaptureArtifactCreate string

// queryKubernetesCaptureArtifactPin binds the stable channel identity to its canonical artifact.
// queryKubernetesCaptureArtifactPin связывает stable channel identity с canonical artifact.
//
//go:embed sql/kubernetes_capture__artifact_pin.sql
var queryKubernetesCaptureArtifactPin string

// queryKubernetesCaptureArtifactContent stores the existing canonical AV/version receipt.
// queryKubernetesCaptureArtifactContent сохраняет existing canonical AV/version receipt.
//
//go:embed sql/kubernetes_capture__artifact_content.sql
var queryKubernetesCaptureArtifactContent string

// queryKubernetesCaptureArtifactOrigin stores immutable source/provenance for the adopted revision.
// queryKubernetesCaptureArtifactOrigin сохраняет immutable source/provenance принятой revision.
//
//go:embed sql/kubernetes_capture__artifact_origin.sql
var queryKubernetesCaptureArtifactOrigin string

// queryKubernetesCaptureArtifactBinding binds the captured revision through the existing RUN_RESULT.
// queryKubernetesCaptureArtifactBinding связывает captured revision через existing RUN_RESULT.
//
//go:embed sql/kubernetes_capture__artifact_binding.sql
var queryKubernetesCaptureArtifactBinding string

// queryKubernetesCaptureComplete closes only the exact still-live task after all channel adoptions.
// queryKubernetesCaptureComplete закрывает только exact still-live task после всех channel adoptions.
//
//go:embed sql/kubernetes_capture__complete.sql
var queryKubernetesCaptureComplete string

// kubernetesPreparedCapture pins the existing prepared-content result to its immutable channel body.
// kubernetesPreparedCapture связывает existing prepared-content result с immutable channel body.
type kubernetesPreparedCapture struct {
	content  kubernetesCaptureContent
	binding  preparedContentBinding
	prepared preparedContent
}

// stageKubernetesCapturedChannel reserves a stable owner identity, inspects the typed format and
// invokes the existing one-PUT ledger. It never creates an artifact or publishes inside live credit.
// stageKubernetesCapturedChannel резервирует stable owner identity, проверяет typed format и
// вызывает existing one-PUT ledger. Не создаёт artifact и не публикует внутри live credit.
func (repository *Repository) stageKubernetesCapturedChannel(ctx context.Context, record kubernetesCaptureTaskRecord, task value.ManagedKubernetesCaptureTaskBinding, content kubernetesCaptureContent) (result kubernetesPreparedCapture, resultErr error) {
	result.content = content
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	if _, err := repository.lockKubernetesCaptureOwnerTx(ctx, tx, record, task); err != nil {
		return result, err
	}
	ref, err := newRef("art")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var artifactID string
	if err := tx.QueryRow(ctx, queryKubernetesCaptureArtifactReserve, pgx.StrictNamedArgs{"organization_id": record.organizationID, "stream_id": record.streamID, "channel": content.Channel, "artifact_ref": ref}).Scan(&ref, &artifactID); err != nil {
		return result, errs.ErrUnavailable
	}
	source := record.source
	binding := preparedContentBinding{ActorID: source.RootActorID, OrganizationID: record.organizationID, OrganizationRef: source.OrganizationRef, ProjectID: source.ProjectID, ProjectRef: source.ProjectRef, SourceProfile: "KUBERNETES_OUTPUT", SourceProfileRef: task.CaptureRef, SourceProfileVersion: source.CaptureGeneration, SourceContextDigest: task.SnapshotDigest, SourceLeaseRef: source.OriginalLeaseRef, SourceLeaseGeneration: source.Generation, SourceFenceDigest: source.OriginalFenceDigest, SourceRunRef: source.RunRef, OperationKey: "kubernetes.output." + content.Channel, IntentOperation: "kubernetes.capture.finalize", IdempotencyKey: task.CaptureRef + ":" + content.Channel, IntentDigest: content.Origin.SourceDigest, TargetArtifactRef: ref, TargetArtifactID: artifactID}
	if artifactID != "" {
		var savedRef, state string
		var revision int64
		if err := tx.QueryRow(ctx, queryKubernetesCaptureArtifactHead, pgx.StrictNamedArgs{"organization_id": record.organizationID, "artifact_id": artifactID}).Scan(&artifactID, &savedRef, &binding.ExpectedArtifactVersion, &binding.SourceRevisionRef, &revision, &state); err != nil || savedRef != ref || state != "ACTIVE" {
			return result, errs.ErrNotFound
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	verdict, err := artifactpolicy.InspectKubernetesCapturedReader(content.ContentKind, content.FileName, content.MediaType, &kubernetesCaptureSpoolReader{spool: content.Body}, content.Body.size)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if verdict.ScanState != artifactpolicy.ScanClean {
		return result, errors.Join(errs.ErrInvalid, errKubernetesCaptureContentRejected)
	}
	prepared, err := repository.stagePreparedContent(ctx, preparedContentRequest{Binding: binding, FileName: content.FileName, MediaType: verdict.MediaType, Digest: content.Digest, ScanState: verdict.ScanState, PreviewState: verdict.PreviewState, SizeBytes: content.Body.size, Body: &kubernetesCaptureSpoolReader{spool: content.Body}, CaptureTask: &task})
	if err != nil {
		return result, err
	}
	result.binding = binding
	result.prepared = prepared
	return result, nil
}

// publishKubernetesCapturedChannelTx adopts one canonical artifact revision and origin atomically.
// It reuses prepared-content receipt checks and emits no native execution or delivery credit.
// publishKubernetesCapturedChannelTx атомарно принимает canonical artifact revision и origin.
// Использует prepared-content receipt checks; native execution либо delivery credit не создаёт.
func (repository *Repository) publishKubernetesCapturedChannelTx(ctx context.Context, tx pgx.Tx, current scope, record kubernetesCaptureTaskRecord, channel kubernetesPreparedCapture) error {
	p := channel.prepared
	binding := channel.binding
	scanJSON, err := artifactAntivirusJSON(p.Prepared.Antivirus)
	if err != nil {
		return err
	}
	receiptRef, err := newRef("obj")
	if err != nil {
		return errs.ErrUnavailable
	}
	var artifactID, revisionID string
	var revision int64
	if binding.TargetArtifactID == "" {
		err = tx.QueryRow(ctx, queryKubernetesCaptureArtifactCreate, pgx.StrictNamedArgs{"artifact_ref": p.ArtifactRef, "revision_ref": p.RevisionRef, "organization_id": record.organizationID, "project_id": record.source.ProjectID, "run_id": record.source.RunID, "node_id": record.source.NodeID, "actor_id": record.source.RootActorID, "file_name": p.FileName, "media_type": p.Prepared.MediaType, "size_bytes": p.Prepared.SizeBytes, "digest": p.Prepared.Digest, "scan_state": p.Prepared.ScanState, "receipt_ref": receiptRef, "preview_state": p.Prepared.PreviewState}).Scan(&artifactID, &revisionID, &revision)
		if err != nil {
			return mapWriteError(err)
		}
	} else {
		var ref, state, priorRevisionRef string
		var version int64
		err = tx.QueryRow(ctx, queryKubernetesCaptureArtifactHead, pgx.StrictNamedArgs{"organization_id": record.organizationID, "artifact_id": binding.TargetArtifactID}).Scan(&artifactID, &ref, &version, &priorRevisionRef, &revision, &state)
		if err != nil || ref != p.ArtifactRef || state != "ACTIVE" || version != binding.ExpectedArtifactVersion || priorRevisionRef != binding.SourceRevisionRef {
			return errs.ErrVersionMismatch
		}
		revision++
		if err := tx.QueryRow(ctx, queryArtifactRevisionCreate, artifactID, p.RevisionRef, revision, p.FileName, p.Prepared.MediaType, p.Prepared.SizeBytes, p.Prepared.Digest, "INTEGRATION_RESULT", p.Prepared.ScanState, receiptRef, p.Prepared.PreviewState, record.source.RootActorID).Scan(&revisionID); err != nil {
			return mapWriteError(err)
		}
		tag, err := tx.Exec(ctx, queryArtifactRevisionAdvanceHead, artifactID, revisionID, version)
		if err != nil || tag.RowsAffected() != 1 {
			return errs.ErrVersionMismatch
		}
	}
	if _, err := tx.Exec(ctx, queryKubernetesCaptureArtifactContent, pgx.StrictNamedArgs{"revision_id": revisionID, "object_key": p.Prepared.ObjectKey, "object_version": p.Prepared.ObjectVersion, "object_etag": p.Prepared.ObjectETag, "digest": p.Prepared.Digest, "size_bytes": p.Prepared.SizeBytes, "antivirus_report": scanJSON}); err != nil {
		return errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryKubernetesCaptureArtifactPin, pgx.StrictNamedArgs{"organization_id": record.organizationID, "stream_id": record.streamID, "channel": channel.content.Channel, "artifact_ref": p.ArtifactRef, "artifact_id": artifactID})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrUnavailable
	}
	origin, err := json.Marshal(channel.content.Origin)
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryKubernetesCaptureArtifactOrigin, pgx.StrictNamedArgs{"organization_id": record.organizationID, "capture_id": record.id, "stream_id": record.streamID, "channel": channel.content.Channel, "artifact_id": artifactID, "revision_id": revisionID, "artifact_ref": p.ArtifactRef, "revision_ref": p.RevisionRef, "prepared_content_id": p.LedgerID, "digest": p.Prepared.Digest, "size_bytes": p.Prepared.SizeBytes, "source_digest": channel.content.Origin.SourceDigest, "origin_json": string(origin)}); err != nil {
		return errs.ErrUnavailable
	}
	if err := repository.adoptPreparedContentTx(ctx, tx, current, p.LedgerID, revisionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, queryKubernetesCaptureArtifactBinding, pgx.StrictNamedArgs{"artifact_id": artifactID, "run_ref": record.source.RunRef, "actor_id": record.source.RootActorID}); err != nil {
		return errs.ErrUnavailable
	}
	_, err = repository.emitRunEvent(ctx, tx, current, record.source.ProjectID, record.source.RootRunID, p.ArtifactRef, "ARTIFACT_AVAILABLE", record.source.NodeRef, "", "", p.ArtifactRef, "i18n:RESULT_ARTIFACT_AVAILABLE", "", "")
	return err
}

// FinalizeManagedKubernetesCapture runs the retained-source consumer, separate from live execution.
// Lost replies recover by metadata Get; prepared-content unique intents prevent payload PUT replay.
// A newer seal revokes this task before adoption, and all channels commit in one owner transaction.
// FinalizeManagedKubernetesCapture выполняет retained-source consumer отдельно от live execution.
// Lost reply восстанавливается metadata Get; unique prepared-content intents запрещают PUT replay.
// Newer seal отзывает task до adoption; все channels фиксируются одной owner transaction.
func (repository *Repository) FinalizeManagedKubernetesCapture(ctx context.Context, p value.Principal, binding value.ManagedKubernetesCaptureTaskBinding) (result entity.ManagedKubernetesCaptureReceipt, resultErr error) {
	if err := repository.kubernetesCaptureCaller(ctx, p, binding, "platform.artifacts.kubernetes-capture.finalize"); err != nil {
		return result, err
	}
	if repository.kubernetesCaptureSlots == nil {
		return result, errs.ErrUnavailable
	}
	select {
	case repository.kubernetesCaptureSlots <- struct{}{}:
		defer func() { <-repository.kubernetesCaptureSlots }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	record, err := readKubernetesCaptureTask(ctx, repository.pool, binding)
	if err != nil {
		return result, err
	}
	defer func() {
		if captureKnownContentError(resultErr) {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer stop()
			rejected, rejectErr := repository.rejectKubernetesCaptureContent(cleanup, p, binding, record)
			if rejectErr == nil {
				result, resultErr = rejected, nil
			} else {
				resultErr = errors.Join(resultErr, rejectErr)
			}
		}
	}()
	if record.receipt.State != enum.KubernetesCaptureClaimed {
		if err := readKubernetesCaptureArtifacts(ctx, repository.pool, &record); err != nil {
			return result, err
		}
		return record.receipt, nil
	}
	if record.attemptState != "ACTIVE" || !record.expires.After(record.now) {
		return result, errs.ErrForbidden
	}
	ctx, stopDeadline := context.WithDeadline(ctx, record.expires)
	defer stopDeadline()
	preflight, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if _, err := repository.lockKubernetesCaptureOwnerTx(ctx, preflight, record, binding); err != nil {
		_ = preflight.Rollback(ctx)
		return result, err
	}
	if err := preflight.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	assembly, err := repository.assembleKubernetesCapture(ctx, record)
	if err != nil {
		return result, err
	}
	defer assembly.clear()
	contents, err := repository.normalizeKubernetesCaptureChannels(ctx, record, assembly)
	if err != nil {
		return result, err
	}
	defer func() {
		for index := range contents {
			_ = contents[index].Body.Close()
		}
	}()
	staged := make([]kubernetesPreparedCapture, 0, len(contents))
	for _, content := range contents {
		prepared, err := repository.stageKubernetesCapturedChannel(ctx, record, binding, content)
		if err != nil {
			return result, err
		}
		staged = append(staged, prepared)
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	current, err := repository.lockKubernetesCaptureOwnerTx(ctx, tx, record, binding)
	if err != nil {
		return result, err
	}
	for _, channel := range staged {
		if err := repository.publishKubernetesCapturedChannelTx(ctx, tx, current, record, channel); err != nil {
			return result, err
		}
	}
	args, err := kubernetesCaptureTaskArgs(binding)
	if err != nil {
		return result, err
	}
	args["capture_kind"] = string(assembly.Kind)
	args["source_digest"] = assembly.Digest
	args["acknowledged_prefix"] = assembly.Prefix
	args["dropped_bytes"] = assembly.DroppedBytes
	tag, err := tx.Exec(ctx, queryKubernetesCaptureComplete, args)
	if err != nil || tag.RowsAffected() != 1 {
		return result, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return result, errs.ErrUnavailable
	}
	record, err = readKubernetesCaptureTask(ctx, repository.pool, binding)
	if err != nil {
		return result, err
	}
	if err := readKubernetesCaptureArtifacts(ctx, repository.pool, &record); err != nil {
		return result, err
	}
	return record.receipt, nil
}

// captureKnownContentError separates unsafe content from storage uncertainty for future owner repair.
// captureKnownContentError отделяет unsafe content от storage uncertainty для owner repair.
func captureKnownContentError(err error) bool {
	return errors.Is(err, errKubernetesCaptureContentRejected)
}

// errKubernetesCaptureContentRejected marks only a proven format, archive or antivirus refusal.
// Generic parser/storage errors must not be mislabeled as a definitive unsafe-content decision.
// errKubernetesCaptureContentRejected обозначает лишь proven format/archive/antivirus refusal.
// Generic parser/storage errors не превращаются в definitive unsafe-content decision.
var errKubernetesCaptureContentRejected = errors.New("Kubernetes captured content is rejected")
