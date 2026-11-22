package platform

import (
	"bytes"
	"context"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// kubernetesInputReservation retains one committed private-object intent or exact historical metadata, never payload authority.
// kubernetesInputReservation хранит committed private-object intent либо exact historical metadata, не payload authority.
type kubernetesInputReservation struct {
	stageID, streamID, key, ref string
	existing                    *entity.KubernetesStreamInputReadback
}

// kubernetesInputRecipe holds already validated source metadata without pretending to own payload bytes.
// ByteCount comes from public verified bytes or an immutable FILE manifest; it is never a caller quota override.
// kubernetesInputRecipe хранит уже проверенные source metadata, не имитируя payload bytes.
// ByteCount берётся из public verified bytes либо immutable FILE manifest, не caller quota override.
type kubernetesInputRecipe struct {
	InvocationRef, StreamRef, Channel, PayloadDigest string
	ByteCount                                        uint32
	EndOfInput                                       bool
}

// submission converts metadata for the existing commitment encoder; no payload is fabricated or disclosed.
// submission передаёт metadata existing commitment encoder; payload не фабрикуется и не раскрывается.
func (input kubernetesInputRecipe) submission() command.KubernetesStreamInputSubmit {
	return command.KubernetesStreamInputSubmit{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, Channel: input.Channel, PayloadDigest: input.PayloadDigest, EndOfInput: input.EndOfInput}
}

// reserveKubernetesInputTx reuses existing idempotency/OCC/quota and one intent under already verified origin/source and stream locks.
// Its caller must validate the exact source first and commit this same transaction before the single object PUT.
// reserveKubernetesInputTx использует existing idempotency/OCC/quota и один intent под already verified origin/source и stream locks.
// Caller сначала проверяет exact source и фиксирует эту же transaction до single object PUT.
func reserveKubernetesInputTx(ctx context.Context, tx pgx.Tx, current scope, record kubernetesInputRecord, mutation value.Mutation, input command.KubernetesStreamInputSubmit) (kubernetesInputReservation, error) {
	return reserveKubernetesInputRecipeTx(ctx, tx, current, record, mutation, kubernetesInputRecipe{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, Channel: input.Channel, PayloadDigest: input.PayloadDigest, ByteCount: uint32(len(input.Payload)), EndOfInput: input.EndOfInput})
}

// reserveKubernetesInputRecipeTx preserves the same idempotency/OCC/quota and one-intent ordering for typed private sources.
// Its caller has already validated either public bytes or the exact owner-pinned FILE chunk; no payload is read here.
// reserveKubernetesInputRecipeTx сохраняет existing idempotency/OCC/quota и one-intent ordering для typed private sources.
// Caller уже проверил public bytes либо exact owner-pinned FILE chunk; payload здесь не читается.
func reserveKubernetesInputRecipeTx(ctx context.Context, tx pgx.Tx, current scope, record kubernetesInputRecord, mutation value.Mutation, input kubernetesInputRecipe) (kubernetesInputReservation, error) {
	var reservation kubernetesInputReservation
	if mutation.ExpectedVersion == nil || *mutation.ExpectedVersion < 1 {
		return reservation, errs.ErrInvalid
	}
	intent := inputSubmissionIntent(input.submission(), *mutation.ExpectedVersion)
	existing, err := readKubernetesInputStage(ctx, tx, current.organizationID, record.id, current.actorID, mutation.IdempotencyKey, record.view)
	if err != nil {
		return reservation, err
	}
	if existing.id != "" {
		if existing.intentDigest != intent {
			return reservation, errs.ErrIdempotencyReuse
		}
		reservation.existing = &entity.KubernetesStreamInputReadback{View: record.view, Submission: &existing.metadata}
		return reservation, nil
	}
	if record.view.Version != *mutation.ExpectedVersion {
		return reservation, errs.ErrVersionMismatch
	}
	if record.view.State != "ACTIVE" || record.invocationState != "RUNNING" || !record.view.HardDeadline.After(record.now) || record.view.Pending != nil || record.sequence >= managedkubernetes.MaximumFrameSequence || record.logical+uint64(input.ByteCount) > record.view.MaximumTotalBytes || record.delivered+uint64(input.ByteCount) > record.view.MaximumTotalBytes {
		return reservation, errs.ErrConflict
	}
	if input.Channel == "PORT_FORWARD" {
		if err := kubernetesPortForwardNewByteBudget(ctx, tx, current.organizationID, record.id, record.logical, record.view.MaximumTotalBytes, uint32(input.ByteCount)); err != nil {
			return reservation, err
		}
	}
	var childCurrent bool
	if err := tx.QueryRow(ctx, queryKubernetesInputCurrent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": record.id}).Scan(&childCurrent); err != nil {
		return reservation, errs.ErrUnavailable
	}
	if !childCurrent {
		return reservation, errs.ErrForbidden
	}
	if _, err := tx.Exec(ctx, queryKubernetesOutputQuotaLock, pgx.StrictNamedArgs{"organization_id": current.organizationID}); err != nil {
		return reservation, errs.ErrUnavailable
	}
	var total, count int64
	if err := tx.QueryRow(ctx, queryKubernetesInputQuota, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&total, &count); err != nil {
		return reservation, errs.ErrUnavailable
	}
	var sourceBytes, sourceCount int64
	if err := tx.QueryRow(ctx, queryKubernetesFileQuota, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&sourceBytes, &sourceCount); err != nil {
		return reservation, errs.ErrUnavailable
	}
	total += sourceBytes
	count += sourceCount
	addedBytes, addedObjects := int64(input.ByteCount), int64(1)
	if input.Channel == "FILE_CONTENT" {
		addedBytes, addedObjects = 0, 0
	}
	if total+addedBytes > maximumKubernetesStagedBytes || count+addedObjects > int64(managedkubernetes.MaximumStagedObjects) {
		return reservation, errs.ErrKubernetesStagingQuota
	}
	ref, err := newRef("kif")
	if err != nil {
		return reservation, err
	}
	key := "organizations/" + current.organizationID + "/kubernetes-input-stages/" + ref
	if input.Channel == "FILE_CONTENT" {
		key = ""
	}
	var id string
	if err := tx.QueryRow(ctx, queryKubernetesInputReserve, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "stream_id": record.id, "actor_id": current.actorID, "submission_key": mutation.IdempotencyKey, "intent_digest": intent, "sequence": record.sequence + 1, "channel": input.Channel, "payload_digest": input.PayloadDigest, "byte_count": int(input.ByteCount), "end_of_input": input.EndOfInput, "object_key": key}).Scan(&id); err != nil {
		return reservation, mapWriteError(err)
	}
	return kubernetesInputReservation{stageID: id, streamID: record.id, key: key, ref: ref}, nil
}

// finishKubernetesInputReservation performs one private PUT, records late exact storage facts under stream-before-stage locks, then reads safe original-key metadata.
// The caller retains exclusive payload ownership until this synchronous helper returns; UNKNOWN never retries or replaces the object.
// finishKubernetesInputReservation выполняет один private PUT, сохраняет late exact storage facts под stream-before-stage locks, затем читает safe original-key metadata.
// Caller сохраняет exclusive payload ownership до возврата synchronous helper; UNKNOWN не повторяет и не заменяет object.
func (repository *Repository) finishKubernetesInputReservation(ctx context.Context, current scope, p value.Principal, reservation kubernetesInputReservation, input command.KubernetesStreamInputSubmit, submissionKey string) (entity.KubernetesStreamInputReadback, error) {
	if err := repository.putKubernetesInputReservation(ctx, current, reservation, input); err != nil {
		return entity.KubernetesStreamInputReadback{}, err
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	// Metadata read is independent from the completed caller; no payload is resent.
	// Metadata read независим от completed caller; payload повторно не отправляется.
	return repository.readKubernetesInputReadback(cleanup, current, p, query.KubernetesStreamInputRead{InvocationRef: input.InvocationRef, StreamRef: input.StreamRef, SubmissionIdempotencyKey: submissionKey})
}

// putKubernetesInputReservation performs the existing one PUT and stream-before-stage late-fact recording only.
// Only public bytes use this PUT path; FILE stages are metadata and proceed directly to single-attempt Claim.
// putKubernetesInputReservation выполняет existing one PUT и stream-before-stage late-fact recording.
// Только public bytes используют PUT; FILE metadata сразу продолжает single-attempt Claim.
func (repository *Repository) putKubernetesInputReservation(ctx context.Context, current scope, reservation kubernetesInputReservation, input command.KubernetesStreamInputSubmit) error {
	if input.Channel == "FILE_CONTENT" || reservation.key == "" {
		return errs.ErrInvalid
	}
	object, putErr := repository.putArtifactObject(ctx, reservation.key, platformrepo.ArtifactUpload{Reader: bytes.NewReader(input.Payload), MediaType: "application/octet-stream", Digest: "sha256:" + input.PayloadDigest, SizeBytes: int64(len(input.Payload))})
	state := string(enum.KubernetesInputUnknown)
	version, etag := "", ""
	if putErr == nil && validKubernetesOutputObject(reservation.key, input.PayloadDigest, uint32(len(input.Payload)), object) {
		state = string(enum.KubernetesInputQueued)
		version, etag = object.VersionID, object.ETag
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := repository.recordKubernetesInputObject(cleanup, current.organizationID, reservation.streamID, reservation.stageID, state, version, etag); err != nil {
		return err
	}
	return nil
}
