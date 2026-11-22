package platform

import (
	"context"
	_ "embed"
	"errors"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"strconv"
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

// queryKubernetesFileStreamSource resolves the single source already bound by Begin to this stream.
// queryKubernetesFileStreamSource разрешает единственный source, связанный Begin с этим stream.
//
//go:embed sql/kubernetes_file__stream_source.sql
var queryKubernetesFileStreamSource string

// queryKubernetesFileFrameInsert binds an existing INPUT intent to one exact normalized chunk.
// queryKubernetesFileFrameInsert связывает existing INPUT intent с exact normalized chunk.
//
//go:embed sql/kubernetes_file__frame_insert.sql
var queryKubernetesFileFrameInsert string

// queryKubernetesFileFrameCount advances only after all earlier immutable chunks were acknowledged.
// queryKubernetesFileFrameCount продвигается лишь после ACK всех прежних immutable chunks.
//
//go:embed sql/kubernetes_file__frame_count.sql
var queryKubernetesFileFrameCount string

// queryKubernetesFileQueue commits only source-bound metadata; no object key or PUT receipt is fabricated.
// queryKubernetesFileQueue фиксирует лишь source-bound metadata; object key либо PUT receipt не фабрикуется.
//
//go:embed sql/kubernetes_file__queue.sql
var queryKubernetesFileQueue string

// queryKubernetesFileFrameBinding resolves the persisted source tuple for Claim and native admission.
// queryKubernetesFileFrameBinding разрешает persisted source tuple для Claim и native admission.
//
//go:embed sql/kubernetes_file__frame_binding.sql
var queryKubernetesFileFrameBinding string

// currentKubernetesFileStreamTx proves current source ACL and clamps the existing INPUT ceiling.
// It accepts no artifact selector and cannot replace a source, stream, original context or native attempt.
// currentKubernetesFileStreamTx доказывает current source ACL и ограничивает existing INPUT ceiling.
// Не принимает artifact selector, не заменяет source, stream, original context либо native attempt.
func (repository *Repository) currentKubernetesFileStreamTx(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, record *kubernetesInputRecord, first time.Time) (kubernetesFileSourceRecord, kubernetesFileOrigin, error) {
	var source kubernetesFileSourceRecord
	var origin kubernetesFileOrigin
	if record.view.Operation != "kubernetes.cp.upload" {
		return source, origin, errs.ErrInvalid
	}
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		return source, origin, err
	}
	var ref string
	err = tx.QueryRow(ctx, queryKubernetesFileStreamSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "stream_ref": record.view.StreamRef}).Scan(&ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return source, origin, errs.ErrForbidden
	}
	if err != nil {
		return source, origin, errs.ErrUnavailable
	}
	source, err = readKubernetesFileSourceTx(ctx, tx, current.organizationID, binding.InvocationRef, ref, "")
	if err != nil {
		return source, origin, err
	}
	if source.view.State != "READY" && source.view.State != "TRANSFERRED" || source.purgedAt != nil || source.view.StreamRef != record.view.StreamRef || source.bindingDigest != record.bindingDigest || source.snapshotDigest != record.snapshotDigest || source.generation != binding.Generation {
		return source, origin, errs.ErrForbidden
	}
	origin, until, err := repository.readKubernetesFileOriginTx(ctx, tx, owner, earlierAccessDeadline(first, record.ceiling))
	if err != nil {
		return source, origin, err
	}
	if origin.artifactRef != source.view.ArtifactRef || origin.revisionRef != source.view.ArtifactRevisionRef {
		return source, origin, errs.ErrForbidden
	}
	if source.accessUntil != nil {
		until = earlierAccessDeadline(until, *source.accessUntil)
	}
	if err := owner.creditError(ctx, until); err != nil {
		return source, origin, err
	}
	record.ceiling = until
	record.fileSource = &source
	return source, origin, nil
}

// stageKubernetesFileNextInput commits the next metadata-only INPUT intent without reading or writing source bytes.
// A later durable Claim authorizes one bounded Range; previous unknown deliveries are never replaced.
// stageKubernetesFileNextInput фиксирует следующий metadata-only INPUT intent без чтения либо записи source bytes.
// Последующий durable Claim разрешает один bounded Range; прежние unknown deliveries не заменяются.
func (repository *Repository) stageKubernetesFileNextInput(ctx context.Context, current scope, binding value.ManagedKubernetesBinding, streamRef string) (ceiling time.Time, resultErr error) {
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return ceiling, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	record, stage, err := repository.lockKubernetesInputClaim(ctx, tx, current, binding, streamRef)
	if err != nil {
		return ceiling, err
	}
	ceiling = record.ceiling
	if record.view.Operation != "kubernetes.cp.upload" {
		if tx.Commit(ctx) != nil {
			return ceiling, errs.ErrUnavailable
		}
		return ceiling, nil
	}
	if stage.id != "" && stage.metadata.State != enum.KubernetesInputAcknowledged {
		if tx.Commit(ctx) != nil {
			return ceiling, errs.ErrUnavailable
		}
		return ceiling, nil
	}
	source, origin, err := repository.currentKubernetesFileStreamTx(ctx, tx, current, binding, &record, time.Time{})
	if err != nil {
		return ceiling, err
	}
	ceiling = record.ceiling
	if source.view.EOFAcknowledged || source.view.State == "TRANSFERRED" {
		if tx.Commit(ctx) != nil {
			return ceiling, errs.ErrUnavailable
		}
		return ceiling, nil
	}
	var ordinal uint32
	if tx.QueryRow(ctx, queryKubernetesFileFrameCount, pgx.StrictNamedArgs{"organization_id": current.organizationID, "source_id": source.id}).Scan(&ordinal) != nil {
		return ceiling, errs.ErrUnavailable
	}
	var manifest managedkubernetes.CopyInputManifest
	if internalrpcauth.DecodeStrictJSON([]byte(source.manifestJSON), &manifest) != nil || manifest.Validate() != nil {
		return ceiling, errs.ErrUnavailable
	}
	offset, length, digest, eof, err := manifest.Slice(uint64(ordinal))
	if err != nil {
		return ceiling, errs.ErrConflict
	}
	permissionUntil := time.Time{}
	if source.accessUntil != nil {
		permissionUntil = *source.accessUntil
	}
	if _, err := tx.Exec(ctx, queryKubernetesFileSourceAllowance, pgx.StrictNamedArgs{"source_id": source.id, "organization_id": current.organizationID, "authorized_until": record.ceiling, "source_access_until": nullableAccessDeadline(permissionUntil)}); err != nil {
		return ceiling, mapWriteError(err)
	}
	stageScope := current
	stageScope.actorID = origin.actor.actorID
	recipe := kubernetesInputRecipe{InvocationRef: binding.InvocationRef, StreamRef: streamRef, Channel: "FILE_CONTENT", PayloadDigest: digest, ByteCount: uint32(length), EndOfInput: eof}
	version := record.view.Version
	key := "file:" + source.view.SourceRef + ":" + strconv.FormatUint(uint64(ordinal), 10)
	reservation, err := reserveKubernetesInputRecipeTx(ctx, tx, stageScope, record, value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, recipe)
	if err != nil {
		return ceiling, err
	}
	if reservation.existing != nil {
		if tx.Commit(ctx) != nil {
			return ceiling, errs.ErrUnavailable
		}
		return ceiling, nil
	}
	if _, err := tx.Exec(ctx, queryKubernetesFileFrameInsert, pgx.StrictNamedArgs{"source_id": source.id, "stream_id": record.id, "organization_id": current.organizationID, "stage_id": reservation.stageID, "ordinal": ordinal, "byte_offset": offset, "byte_count": length, "payload_digest": digest, "end_of_input": eof}); err != nil {
		return ceiling, mapWriteError(err)
	}
	tag, err := tx.Exec(ctx, queryKubernetesFileQueue, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": record.id, "stage_id": reservation.stageID})
	if err != nil || tag.RowsAffected() != 1 {
		return ceiling, errs.ErrUnavailable
	}
	if ctx.Err() != nil {
		return ceiling, errs.ErrUnavailable
	}
	if !ceiling.After(time.Now()) {
		return ceiling, errs.ErrForbidden
	}
	if tx.Commit(ctx) != nil {
		return ceiling, errs.ErrUnavailable
	}
	return ceiling, nil
}

// validateKubernetesFileStageTx checks the immutable source binding before releasing or admitting bytes.
// Its caller independently checks current source authority and the original monotonic delivery deadline.
// validateKubernetesFileStageTx проверяет immutable source binding до выдачи либо admission bytes.
// Caller отдельно проверяет current source authority и original monotonic delivery deadline.
func validateKubernetesFileStageTx(ctx context.Context, tx pgx.Tx, org, stream string, stage kubernetesInputStage, expected *kubernetesFileSourceRecord) error {
	if stage.metadata.Channel != "FILE_CONTENT" {
		return nil
	}
	_, err := kubernetesFileRangeTx(ctx, tx, org, stream, stage, expected)
	return err
}

// kubernetesFileRangeTx resolves a bounded immutable range, never an object operation or delivery attempt.
// The caller must commit its exact one-attempt Claim before invoking storage with this recipe.
// kubernetesFileRangeTx разрешает bounded immutable range, не выполняет object operation либо delivery attempt.
// Caller фиксирует exact one-attempt Claim до storage call с этим recipe.
func kubernetesFileRangeTx(ctx context.Context, tx pgx.Tx, org, stream string, stage kubernetesInputStage, expected *kubernetesFileSourceRecord) (objectstorage.RangeRead, error) {
	var read objectstorage.RangeRead
	if expected == nil || stage.metadata.Channel != "FILE_CONTENT" || stage.key != "" || stage.version != "" || stage.etag != "" {
		return read, errs.ErrForbidden
	}
	var ref, state, digest, sourceDigest string
	var until, access, purged *time.Time
	var ordinal uint32
	var offset, length uint64
	var eof bool
	err := tx.QueryRow(ctx, queryKubernetesFileFrameBinding, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "stage_id": stage.id}).Scan(&ref, &state, &until, &access, &purged, &ordinal, &offset, &length, &digest, &eof, &read.Object.Key, &read.Object.VersionID, &read.Object.ETag, &sourceDigest, &read.Object.SizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return read, errs.ErrForbidden
	}
	if err != nil {
		return read, errs.ErrUnavailable
	}
	if state != "READY" || purged != nil || until == nil || access != nil && !access.After(time.Now()) {
		return read, errs.ErrForbidden
	}
	read.Object.Digest = "sha256:" + sourceDigest
	return bindKubernetesFileRange(*expected, ref, read.Object, ordinal, offset, length, digest, eof, stage.metadata)
}

// bindKubernetesFileRange ties the frame to the current owner-selected source and immutable object version, never latest.
// Manifest.Slice remains the single canonical chunk recipe; changed refs, versions, indexes, ranges or hashes fail closed.
// bindKubernetesFileRange связывает frame с current owner-selected source и immutable object version, не latest.
// Manifest.Slice остаётся единственным canonical chunk recipe; изменённые refs/versions/indexes/ranges/hashes закрываются.
func bindKubernetesFileRange(expected kubernetesFileSourceRecord, ref string, receipt objectstorage.Receipt, ordinal uint32, offset, length uint64, digest string, eof bool, metadata entity.KubernetesStreamInputSubmission) (objectstorage.RangeRead, error) {
	var empty objectstorage.RangeRead
	if expected.view.State != "READY" || expected.purgedAt != nil || ref != expected.view.SourceRef || receipt != expected.object || metadata.Channel != "FILE_CONTENT" {
		return empty, errs.ErrForbidden
	}
	var manifest managedkubernetes.CopyInputManifest
	if internalrpcauth.DecodeStrictJSON([]byte(expected.manifestJSON), &manifest) != nil || manifest.Validate() != nil || manifest.SHA256 != expected.view.NormalizedSHA256 || manifest.SizeBytes != expected.view.NormalizedSizeBytes || receipt.Digest != "sha256:"+manifest.SHA256 || receipt.SizeBytes != int64(manifest.SizeBytes) {
		return empty, errs.ErrForbidden
	}
	wantOffset, wantLength, wantDigest, wantEOF, err := manifest.Slice(uint64(ordinal))
	if err != nil || offset != wantOffset || length != wantLength || digest != wantDigest || eof != wantEOF || length != uint64(metadata.ByteCount) || digest != metadata.PayloadDigest || eof != metadata.EndOfInput {
		return empty, errs.ErrForbidden
	}
	read := objectstorage.RangeRead{Object: receipt, Offset: int64(offset), Length: int64(length), Digest: "sha256:" + digest}
	if read.Validate() != nil {
		return empty, errs.ErrForbidden
	}
	return read, nil
}

// admitKubernetesFileFrameTx rechecks current source policy and exact claimed chunk before native credit.
// It does not read storage, replace the delivery deadline or admit an unbound final EOF.
// admitKubernetesFileFrameTx проверяет current source policy и exact claimed chunk до native credit.
// Storage не читает, delivery deadline не заменяет, unbound final EOF не допускает.
func (repository *Repository) admitKubernetesFileFrameTx(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginFrame) (time.Time, error) {
	record, stage, err := repository.lockKubernetesInputClaim(ctx, tx, current, binding, input.StreamRef)
	if err != nil {
		return time.Time{}, err
	}
	if record.view.Operation != "kubernetes.cp.upload" || stage.metadata.Channel != "FILE_CONTENT" || stage.metadata.State != enum.KubernetesInputClaimed || stage.metadata.Sequence != input.Sequence || stage.metadata.PayloadDigest != input.PayloadDigest || stage.metadata.ByteCount != input.ByteCount || stage.metadata.EndOfInput != input.EndOfInput || stage.deadline == nil {
		return time.Time{}, errs.ErrForbidden
	}
	if err := validateKubernetesFileStageTx(ctx, tx, current.organizationID, record.id, stage, record.fileSource); err != nil {
		return time.Time{}, err
	}
	until := minFileDeadline(record.ceiling, *stage.deadline)
	if !until.After(time.Now()) {
		return time.Time{}, errs.ErrForbidden
	}
	return until, nil
}
