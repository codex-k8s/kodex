package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/artifactpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesFileChild resolves the immutable upload child before any source reservation.
// queryKubernetesFileChild разрешает immutable upload child до source reservation.
//
//go:embed sql/kubernetes_file__child.sql
var queryKubernetesFileChild string

// queryKubernetesFileSource locks one source and returns metadata without loading payload bytes.
// queryKubernetesFileSource блокирует source и возвращает metadata без payload bytes.
//
//go:embed sql/kubernetes_file__source.sql
var queryKubernetesFileSource string

// queryKubernetesFileSourceInsert records one original-child source intent before storage IO.
// queryKubernetesFileSourceInsert сохраняет original-child source intent до storage IO.
//
//go:embed sql/kubernetes_file__source_insert.sql
var queryKubernetesFileSourceInsert string

// queryKubernetesFileSourceManifest commits deterministic normalization pins before prepared PUT.
// queryKubernetesFileSourceManifest фиксирует deterministic normalization pins до prepared PUT.
//
//go:embed sql/kubernetes_file__source_manifest.sql
var queryKubernetesFileSourceManifest string

// queryKubernetesFileSourceReady records a verified immutable receipt before typed adoption.
// queryKubernetesFileSourceReady сохраняет verified immutable receipt до typed adoption.
//
//go:embed sql/kubernetes_file__source_ready.sql
var queryKubernetesFileSourceReady string

// queryKubernetesFileSourceAdopt binds prepared bytes to their private source instead of a public artifact.
// queryKubernetesFileSourceAdopt связывает prepared bytes с private source вместо public artifact.
//
//go:embed sql/kubernetes_file__source_adopt.sql
var queryKubernetesFileSourceAdopt string

// queryKubernetesFileSourceAllowance stores a freshly checked source allowance without changing bytes.
// queryKubernetesFileSourceAllowance сохраняет fresh source allowance, не меняя bytes.
//
//go:embed sql/kubernetes_file__source_allowance.sql
var queryKubernetesFileSourceAllowance string

// queryKubernetesFileSourceUncertain retains failed preparation without authorizing a second PUT.
// queryKubernetesFileSourceUncertain сохраняет failed preparation без разрешения second PUT.
//
//go:embed sql/kubernetes_file__source_uncertain.sql
var queryKubernetesFileSourceUncertain string

// queryKubernetesFileSourceFailure records a closed preparation outcome without effect credit or replay.
// queryKubernetesFileSourceFailure сохраняет closed preparation outcome без effect credit либо replay.
//
//go:embed sql/kubernetes_file__source_failure.sql
var queryKubernetesFileSourceFailure string

// queryKubernetesFileQuota counts physical sources separately from bounded INPUT stage objects.
// queryKubernetesFileQuota считает physical sources отдельно от bounded INPUT stage objects.
//
//go:embed sql/kubernetes_file__quota.sql
var queryKubernetesFileQuota string

// kubernetesFileSourceRecord retains exact owner pins and private receipt outside the safe view.
// A missing normalized receipt never becomes an empty successful source or fabricated EOF.
// kubernetesFileSourceRecord хранит exact owner pins и private receipt вне safe view.
// Missing normalized receipt не становится empty successful source либо fabricated EOF.
type kubernetesFileSourceRecord struct {
	view                                                                                                  entity.KubernetesFileInputView
	id, preparedID, manifestJSON, manifestDigest, fenceDigest, snapshotDigest, bindingDigest, inputDigest string
	writerDeadline                                                                                        time.Time
	authorizedUntil, accessUntil, purgedAt                                                                *time.Time
	maximum                                                                                               uint64
	generation                                                                                            int64
	object                                                                                                objectstorage.Receipt
}

// readKubernetesFileSourceTx reads exactly one original invocation/child or explicit source selector.
// Neither selector supplies authority; its caller must first resolve the original owner graph.
// readKubernetesFileSourceTx читает original invocation/child либо explicit source selector.
// Selector не выдаёт authority; caller сначала разрешает original owner graph.
func readKubernetesFileSourceTx(ctx context.Context, tx pgx.Tx, org, inv, source, child string) (kubernetesFileSourceRecord, error) {
	var record kubernetesFileSourceRecord
	v := &record.view
	err := tx.QueryRow(ctx, queryKubernetesFileSource, pgx.StrictNamedArgs{"organization_id": org, "invocation_ref": inv, "source_ref": source, "child_ref": child}).Scan(
		&record.id, &v.SourceRef, &v.InvocationRef, &v.PlanRef, &v.ChildRef, &v.StreamRef, &v.State, &v.Version,
		&v.ArtifactRef, &v.ArtifactRevisionRef, &v.ArtifactSHA256, &v.ArtifactSizeBytes, &v.TransferFormat, &v.NormalizedSHA256, &v.NormalizedSizeBytes, &v.NormalizationRevision,
		&v.StagedBytes, &v.AcknowledgedBytes, &v.EOFAcknowledged, &v.Reason, &v.CreatedAt, &v.HardDeadline, &record.writerDeadline, &record.authorizedUntil, &record.accessUntil,
		&record.preparedID, &record.manifestJSON, &record.manifestDigest, &record.object.Key, &record.object.VersionID, &record.object.ETag, &record.purgedAt,
		&record.maximum, &record.generation, &record.fenceDigest, &record.snapshotDigest, &record.bindingDigest, &record.inputDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, errs.ErrNotFound
	}
	if err != nil {
		return record, errs.ErrUnavailable
	}
	record.object.Digest = "sha256:" + v.NormalizedSHA256
	record.object.SizeBytes = int64(v.NormalizedSizeBytes)
	if !v.Valid() || v.Version < 1 || v.NormalizedSizeBytes > record.maximum || v.AcknowledgedBytes > v.StagedBytes || v.StagedBytes > v.NormalizedSizeBytes {
		return kubernetesFileSourceRecord{}, errs.ErrUnavailable
	}
	if record.manifestJSON != "" {
		var manifest managedkubernetes.CopyInputManifest
		sum := sha256.Sum256([]byte(record.manifestJSON))
		if internalrpcauth.DecodeStrictJSON([]byte(record.manifestJSON), &manifest) != nil || manifest.Validate() != nil || hex.EncodeToString(sum[:]) != record.manifestDigest || manifest.SHA256 != v.NormalizedSHA256 || manifest.SizeBytes != v.NormalizedSizeBytes {
			return kubernetesFileSourceRecord{}, errs.ErrUnavailable
		}
	}
	return record, nil
}

// lockKubernetesFilePreparation resolves current authority and exact plan/child/source before IO.
// The earliest admitted deadline is retained even when a restored alternative grant appears later.
// lockKubernetesFilePreparation разрешает current authority и exact plan/child/source до IO.
// Earliest admitted deadline сохраняется даже при появлении restored alternative grant.
func (repository *Repository) lockKubernetesFilePreparation(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, childRef string, first time.Time) (kubernetesOperationOwner, kubernetesFileOrigin, kubernetesFileSourceRecord, managedkubernetes.CopyProfile, time.Time, error) {
	var origin kubernetesFileOrigin
	var source kubernetesFileSourceRecord
	var profile managedkubernetes.CopyProfile
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		return owner, origin, source, profile, time.Time{}, err
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return owner, origin, source, profile, time.Time{}, err
	}
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return owner, origin, source, profile, time.Time{}, errs.ErrInvalid
	}
	var childID, childDigest, planID, planRef string
	var raw []byte
	var hard time.Time
	var allowed, attempted bool
	err = tx.QueryRow(ctx, queryKubernetesFileChild, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.candidate.id, "child_ref": childRef, "binding_digest": digest, "snapshot_digest": snapshot}).Scan(&childID, &childDigest, &raw, &planID, &planRef, &hard, &allowed, &attempted)
	defer clear(raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, origin, source, profile, time.Time{}, errs.ErrForbidden
	}
	if err != nil {
		return owner, origin, source, profile, time.Time{}, errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	input, _, decodeErr := managedkubernetes.DecodeOperationInput("kubernetes.cp.upload", raw)
	if decodeErr != nil || hex.EncodeToString(sum[:]) != childDigest || !allowed {
		return owner, origin, source, profile, time.Time{}, errs.ErrForbidden
	}
	profile, err = managedkubernetes.DecodeCopyProfile("kubernetes.cp.upload", input)
	if err != nil {
		return owner, origin, source, profile, time.Time{}, errs.ErrUnavailable
	}
	origin, until, err := repository.readKubernetesFileOriginTx(ctx, tx, owner, earlierAccessDeadline(first, hard))
	if err != nil {
		return owner, origin, source, profile, time.Time{}, err
	}
	if profile.ArtifactRef != origin.artifactRef || "sha256:"+profile.ArtifactSHA256 != origin.object.Digest {
		return owner, origin, source, profile, time.Time{}, errs.ErrForbidden
	}
	source, err = readKubernetesFileSourceTx(ctx, tx, current.organizationID, binding.InvocationRef, "", childRef)
	if err == nil {
		if source.snapshotDigest != snapshot || source.bindingDigest != digest || source.inputDigest != childDigest || source.generation != binding.Generation || source.fenceDigest != kubernetesHealthFenceDigest(binding.Fence) {
			return owner, origin, source, profile, time.Time{}, errs.ErrForbidden
		}
		if attempted || source.purgedAt != nil || source.view.State == "TRANSFERRED" || source.view.State == "REJECTED" || source.view.State == "CANCELLED" || source.view.State == "EXPIRED" || source.view.Reason == "INPUT_UNKNOWN" {
			return owner, origin, source, profile, time.Time{}, errs.ErrConflict
		}
		return owner, origin, source, profile, until, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return owner, origin, source, profile, time.Time{}, err
	}
	if attempted {
		return owner, origin, source, profile, time.Time{}, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryKubernetesOutputQuotaLock, pgx.StrictNamedArgs{"organization_id": current.organizationID}); err != nil {
		return owner, origin, source, profile, time.Time{}, errs.ErrUnavailable
	}
	var sourceBytes, sourceCount, stageBytes, stageCount int64
	if tx.QueryRow(ctx, queryKubernetesFileQuota, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&sourceBytes, &sourceCount) != nil || tx.QueryRow(ctx, queryKubernetesInputQuota, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&stageBytes, &stageCount) != nil {
		return owner, origin, source, profile, time.Time{}, errs.ErrUnavailable
	}
	if sourceBytes+stageBytes+int64(profile.MaximumBytes) > maximumKubernetesStagedBytes || sourceCount+stageCount >= int64(managedkubernetes.MaximumStagedObjects) {
		return owner, origin, source, profile, time.Time{}, errs.ErrKubernetesStagingQuota
	}
	ref, err := newRef("kfs")
	if err != nil {
		return owner, origin, source, profile, time.Time{}, err
	}
	writerUntil := minFileDeadline(until, time.Now().Add(45*time.Second))
	var id string
	err = tx.QueryRow(ctx, queryKubernetesFileSourceInsert, pgx.StrictNamedArgs{"source_ref": ref, "organization_id": current.organizationID, "invocation_id": owner.candidate.id, "child_id": childID, "plan_id": planID, "actor_id": origin.actor.actorID, "project_id": origin.projectID, "snapshot_digest": snapshot, "binding_digest": digest, "child_input_digest": childDigest, "generation": binding.Generation, "fence_digest": kubernetesHealthFenceDigest(binding.Fence), "maximum_bytes": profile.MaximumBytes, "transfer_format": profile.TransferFormat, "hard_deadline": hard, "writer_deadline": writerUntil}).Scan(&id)
	if err != nil {
		return owner, origin, source, profile, time.Time{}, mapWriteError(err)
	}
	source, err = readKubernetesFileSourceTx(ctx, tx, current.organizationID, binding.InvocationRef, ref, childRef)
	return owner, origin, source, profile, until, err
}

// minFileDeadline computes a finite minimum without treating a zero time as fresh authority.
// minFileDeadline вычисляет finite minimum, не считая zero time fresh authority.
func minFileDeadline(first, next time.Time) time.Time {
	if first.Before(next) {
		return first
	}
	return next
}

// readKubernetesFileOriginal performs one exact-version bounded read and joins its response body.
// The source is never replaced by latest; mismatch, cancellation and partial reads discard all bytes.
// readKubernetesFileOriginal выполняет exact-version bounded read и join response body.
// Source не заменяется latest; mismatch, cancellation и partial read отбрасывают все bytes.
func (repository *Repository) readKubernetesFileOriginal(ctx context.Context, origin kubernetesFileOrigin) ([]byte, error) {
	object, err := repository.objects.Get(ctx, origin.object.Key, origin.object.VersionID)
	if err != nil {
		if object.Body != nil {
			_ = object.Body.Close()
		}
		return nil, errs.ErrUnavailable
	}
	if object.Body == nil {
		return nil, errs.ErrUnavailable
	}
	if object.Receipt != origin.object {
		_ = object.Body.Close()
		return nil, errs.ErrConflict
	}
	raw, readErr := io.ReadAll(io.LimitReader(object.Body, origin.object.SizeBytes+1))
	closeErr := object.Body.Close()
	sum := sha256.Sum256(raw)
	if readErr != nil || closeErr != nil || ctx.Err() != nil {
		clear(raw)
		return nil, errs.ErrUnavailable
	}
	if int64(len(raw)) != origin.object.SizeBytes || "sha256:"+hex.EncodeToString(sum[:]) != origin.object.Digest {
		clear(raw)
		return nil, errs.ErrConflict
	}
	return raw, nil
}

// PrepareManagedKubernetesFileInput materializes only the frozen original child through one prepared intent.
// It issues no native admission, does not accept bytes or source selectors and never repeats an uncertain PUT.
// PrepareManagedKubernetesFileInput подготавливает frozen original child через один prepared intent.
// Native admission не выдаёт, bytes/source selectors не принимает, uncertain PUT не повторяет.
func (repository *Repository) PrepareManagedKubernetesFileInput(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, childRef string) (result entity.ManagedKubernetesFilePreparation, resultErr error) {
	if err := kubernetesOperationBrokerCaller(p, binding, "platform.runtime.kubernetes.file.prepare"); err != nil {
		return result, err
	}
	if _, ok := repository.objects.(objectstorage.RangeReader); !ok {
		return result, errs.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var failedSourceID, failedOrganizationID string
	defer func() {
		if resultErr != nil && failedSourceID != "" {
			repository.recordKubernetesFilePreparationFailure(ctx, failedOrganizationID, failedSourceID, resultErr)
		}
	}()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, origin, source, profile, until, err := repository.lockKubernetesFilePreparation(ctx, tx, current, binding, childRef, time.Time{})
	if err != nil {
		return result, err
	}
	failedSourceID, failedOrganizationID = source.id, current.organizationID
	if source.view.State == "READY" {
		if _, err := tx.Exec(ctx, queryKubernetesFileSourceAllowance, pgx.StrictNamedArgs{"source_id": source.id, "organization_id": current.organizationID, "authorized_until": until, "source_access_until": nullableAccessDeadline(origin.accessUntil)}); err != nil {
			return result, mapWriteError(err)
		}
		source, err = readKubernetesFileSourceTx(ctx, tx, current.organizationID, binding.InvocationRef, source.view.SourceRef, childRef)
		if err != nil {
			return result, err
		}
		if err := owner.creditError(ctx, until); err != nil {
			return result, err
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		if err := owner.creditError(ctx, until); err != nil {
			return result, err
		}
		return entity.ManagedKubernetesFilePreparation{View: source.view, AuthorizedUntil: until}, nil
	}
	if err := owner.creditError(ctx, until); err != nil {
		return result, err
	}
	if tx.Commit(ctx) != nil {
		return result, errs.ErrUnavailable
	}
	if err := owner.creditError(ctx, until); err != nil {
		return result, err
	}
	if source.preparedID != "" {
		work, stop := context.WithDeadline(ctx, until)
		defer stop()
		prepared, state, requestDigest, readErr := scanPreparedContent(repository.pool.QueryRow(work, queryPreparedContentReplay, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": origin.actor.actorID, "intent_operation": "kubernetes.file.prepare", "idempotency_key": source.view.SourceRef, "operation_key": "kubernetes.cp.upload",
		}))
		if readErr != nil || prepared.LedgerID != source.preparedID || prepared.Prepared.Digest != "sha256:"+source.view.NormalizedSHA256 || prepared.Prepared.SizeBytes != int64(source.view.NormalizedSizeBytes) {
			return result, errs.ErrConflict
		}
		if state == "UNKNOWN" || state == "IN_FLIGHT" {
			prepared, readErr = repository.reconcileKubernetesPreparedContent(work, prepared, requestDigest, "KUBERNETES_FILE_SOURCE")
		} else if state != "STAGED" {
			return result, errs.ErrConflict
		}
		if readErr != nil {
			return result, readErr
		}
		return repository.finishKubernetesFilePreparation(ctx, work, current, binding, childRef, source.view.SourceRef, prepared, until, origin.accessUntil)
	}
	work, stop := context.WithDeadline(ctx, minFileDeadline(until, source.writerDeadline))
	defer stop()
	raw, err := repository.readKubernetesFileOriginal(work, origin)
	if err != nil {
		return result, err
	}
	defer clear(raw)
	normalized, err := managedkubernetes.NormalizeCopyUpload(profile, raw)
	defer clear(normalized.Bytes)
	if err != nil {
		return result, errs.ErrInvalid
	}
	manifest, err := managedkubernetes.BuildCopyInputManifest(normalized)
	if err != nil {
		return result, errs.ErrInvalid
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	manifestSum := sha256.Sum256(manifestRaw)
	verdict, err := artifactpolicy.InspectKubernetesNormalizedCopyReader(bytes.NewReader(normalized.Bytes), int64(len(normalized.Bytes)))
	if err != nil || verdict.ScanState != artifactpolicy.ScanClean {
		return result, errs.ErrInvalid
	}
	verify, err := repository.pool.BeginTx(work, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = verify.Rollback(cleanup)
	}()
	fresh, _, saved, _, bound, err := repository.lockKubernetesFilePreparation(work, verify, current, binding, childRef, until)
	if err != nil {
		return result, err
	}
	until = earlierAccessDeadline(until, bound)
	if saved.view.NormalizedSHA256 == "" {
		tag, err := verify.Exec(work, queryKubernetesFileSourceManifest, pgx.StrictNamedArgs{"source_id": saved.id, "organization_id": current.organizationID, "payload_digest": normalized.SHA256, "byte_count": len(normalized.Bytes), "normalization_revision": normalized.NormalizationRevision, "manifest": string(manifestRaw), "manifest_digest": hex.EncodeToString(manifestSum[:]), "authorized_until": until, "source_access_until": nullableAccessDeadline(origin.accessUntil)})
		if err != nil || tag.RowsAffected() != 1 {
			return result, errs.ErrConflict
		}
	} else if saved.view.NormalizedSHA256 != normalized.SHA256 || saved.manifestJSON != string(manifestRaw) {
		return result, errs.ErrConflict
	}
	if err := fresh.creditError(work, until); err != nil {
		return result, err
	}
	if verify.Commit(work) != nil {
		return result, errs.ErrUnavailable
	}
	if err := fresh.creditError(work, until); err != nil {
		return result, err
	}
	prepared, err := repository.stagePreparedContent(work, preparedContentRequest{Binding: preparedContentBinding{
		ActorID: origin.actor.actorID, OrganizationID: current.organizationID, OrganizationRef: current.organizationRef, ProjectID: origin.projectID, ProjectRef: origin.projectRef,
		SourceProfile: "KUBERNETES_FILE_SOURCE", SourceProfileRef: source.view.SourceRef, SourceProfileVersion: binding.Generation, SourceContextDigest: source.snapshotDigest,
		SourceLeaseRef: binding.LeaseRef, SourceLeaseGeneration: binding.Generation, SourceFenceDigest: source.fenceDigest, SourceRunRef: origin.runRef,
		OperationKey: "kubernetes.cp.upload", IntentOperation: "kubernetes.file.prepare", IdempotencyKey: source.view.SourceRef, IntentDigest: source.inputDigest, SourceRevisionRef: origin.revisionRef,
	}, FileName: "source.tar", MediaType: verdict.MediaType, Digest: "sha256:" + normalized.SHA256, ScanState: verdict.ScanState, PreviewState: verdict.PreviewState, SizeBytes: int64(len(normalized.Bytes)), Body: bytes.NewReader(normalized.Bytes), FileSource: &kubernetesFilePreparedSource{ref: source.view.SourceRef, authorizedUntil: until}})
	if err != nil {
		return result, err
	}
	return repository.finishKubernetesFilePreparation(ctx, work, current, binding, childRef, source.view.SourceRef, prepared, until, origin.accessUntil)
}

// finishKubernetesFilePreparation rechecks original authority before atomically adopting exact private bytes.
// A late or unknown acknowledgement preserves storage facts while withholding source/native credit.
// finishKubernetesFilePreparation проверяет original authority до atomic adoption exact private bytes.
// Late либо unknown acknowledgement сохраняет storage facts, не выдавая source/native credit.
func (repository *Repository) finishKubernetesFilePreparation(ctx, work context.Context, current scope, binding value.ManagedKubernetesBinding, childRef, sourceRef string, prepared preparedContent, until, permissionUntil time.Time) (result entity.ManagedKubernetesFilePreparation, resultErr error) {
	finish, err := repository.pool.BeginTx(work, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = finish.Rollback(cleanup)
	}()
	finalOwner, finalOrigin, final, _, bound, err := repository.lockKubernetesFilePreparation(work, finish, current, binding, childRef, until)
	if err != nil {
		return result, err
	}
	until = earlierAccessDeadline(until, bound)
	permissionUntil = earlierAccessDeadline(permissionUntil, finalOrigin.accessUntil)
	tag, err := finish.Exec(work, queryKubernetesFileSourceReady, pgx.StrictNamedArgs{"source_id": final.id, "organization_id": current.organizationID, "prepared_id": prepared.LedgerID, "object_key": prepared.Prepared.ObjectKey, "object_version": prepared.Prepared.ObjectVersion, "object_etag": prepared.Prepared.ObjectETag, "authorized_until": until, "source_access_until": nullableAccessDeadline(permissionUntil)})
	if err != nil || tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	tag, err = finish.Exec(work, queryKubernetesFileSourceAdopt, pgx.StrictNamedArgs{"source_id": final.id, "organization_id": current.organizationID, "prepared_id": prepared.LedgerID})
	if err != nil || tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	final, err = readKubernetesFileSourceTx(work, finish, current.organizationID, binding.InvocationRef, sourceRef, childRef)
	if err != nil {
		return result, err
	}
	if err := finalOwner.creditError(work, until); err != nil {
		return result, err
	}
	if finish.Commit(work) != nil {
		return result, errs.ErrUnavailable
	}
	if err := finalOwner.creditError(work, until); err != nil {
		return result, err
	}
	return entity.ManagedKubernetesFilePreparation{View: final.view, AuthorizedUntil: until}, nil
}

// recordKubernetesFilePreparationFailure retains only safe local preparation classification.
// The independent bounded write never changes READY/admitted content, exposes diagnostics or creates an event.
// Exact public source Get is the authoritative readback; native execution remains separate.
// recordKubernetesFilePreparationFailure сохраняет лишь safe local preparation classification.
// Independent bounded write не меняет READY/admitted content, не раскрывает diagnostics и не создаёт event.
// Exact public source Get является authoritative readback; native execution остаётся отдельным.
func (repository *Repository) recordKubernetesFilePreparationFailure(ctx context.Context, org, id string, cause error) {
	state, reason := "UNKNOWN", "SOURCE_UNAVAILABLE"
	switch {
	case errors.Is(cause, errs.ErrInvalid):
		state, reason = "REJECTED", "CONTENT_REJECTED"
	case errors.Is(cause, errs.ErrForbidden):
		state, reason = "REJECTED", "PERMISSION_LOST"
	case errors.Is(cause, errs.ErrConflict):
		state, reason = "REJECTED", "SOURCE_CHANGED"
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, _ = repository.pool.Exec(cleanup, queryKubernetesFileSourceFailure, pgx.StrictNamedArgs{"source_id": id, "organization_id": org, "state": state, "reason": reason})
}
