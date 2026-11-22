package platform

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesOutputCredit locks one current OUTPUT ordinal; it cannot mint or renew credit.
// queryKubernetesOutputCredit блокирует current OUTPUT ordinal, не создавая и не продлевая credit.
//
//go:embed sql/kubernetes_output__credit.sql
var queryKubernetesOutputCredit string

// queryKubernetesOutputExisting locks one private one-write intent without returning its bytes.
// queryKubernetesOutputExisting блокирует private one-write intent без возврата bytes.
//
//go:embed sql/kubernetes_output__existing.sql
var queryKubernetesOutputExisting string

// queryKubernetesOutputQuotaLock serializes bounded reservations only inside the verified organization.
// queryKubernetesOutputQuotaLock сериализует bounded reservations внутри verified organization.
//
//go:embed sql/kubernetes_output__quota_lock.sql
var queryKubernetesOutputQuotaLock string

// queryKubernetesOutputQuota counts durable reservations, including unknown writes awaiting cleanup.
// queryKubernetesOutputQuota считает durable reservations, включая unknown writes до cleanup.
//
//go:embed sql/kubernetes_output__quota.sql
var queryKubernetesOutputQuota string

// queryKubernetesOutputReserve commits a server-assigned private key before the only storage PUT.
// queryKubernetesOutputReserve фиксирует server-assigned private key до единственного storage PUT.
//
//go:embed sql/kubernetes_output__reserve.sql
var queryKubernetesOutputReserve string

// queryKubernetesOutputAdmit binds known content to the exact current ordinal and immutable deadline.
// queryKubernetesOutputAdmit связывает known content с exact current ordinal и immutable deadline.
//
//go:embed sql/kubernetes_output__admit.sql
var queryKubernetesOutputAdmit string

// queryKubernetesOutputRecord retains late storage facts independently from current disclosure authority.
// queryKubernetesOutputRecord сохраняет late storage facts независимо от current disclosure authority.
//
//go:embed sql/kubernetes_output__record.sql
var queryKubernetesOutputRecord string

// queryKubernetesOutputRead returns exact original-binding metadata, never bytes or renewed credit.
// queryKubernetesOutputRead возвращает exact original-binding metadata без bytes и renewed credit.
//
//go:embed sql/kubernetes_output__read.sql
var queryKubernetesOutputRead string

// maximumKubernetesStagedBytes bounds all outstanding private reservations within one organization.
// maximumKubernetesStagedBytes ограничивает все outstanding private reservations одной organization.
const maximumKubernetesStagedBytes = int64(managedkubernetes.MaximumStagedBytes)

// kubernetesOutputStage separates private object facts from the safe original-delivery receipt.
// kubernetesOutputStage отделяет private object facts от safe original-delivery receipt.
type kubernetesOutputStage struct {
	receipt                entity.ManagedKubernetesOutputFrameReceipt
	id, key, version, etag string
	until                  time.Time
}

// kubernetesOutputReceipt returns credit only for accepted content; storage facts alone disclose nothing.
// kubernetesOutputReceipt возвращает credit лишь accepted content; storage facts не раскрывают bytes.
func kubernetesOutputReceipt(stage kubernetesOutputStage) entity.ManagedKubernetesOutputFrameReceipt {
	result := stage.receipt
	if result.State == enum.KubernetesOutputAccepted {
		result.AuthorizedUntil = stage.until
	}
	return result
}

// AcceptManagedKubernetesOutputFrame reserves one intent, performs one PUT and admits known bytes only
// after a second complete owner check. Storage uncertainty or process loss never authorizes another PUT.
// Private staging is not artifact publication, employee disclosure, a frame ACK or business success.
// AcceptManagedKubernetesOutputFrame резервирует intent, выполняет один PUT и допускает known bytes
// только после повторной полной owner check. Uncertainty/process loss не разрешают новый PUT.
// Private staging не является artifact publication, employee disclosure, frame ACK либо business success.
func (repository *Repository) AcceptManagedKubernetesOutputFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesAcceptOutputFrame) (entity.ManagedKubernetesOutputFrameReceipt, error) {
	if err := kubernetesInvocationGatewayCaller(p, binding, "platform.runtime.kubernetes.output.accept"); err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, err
	}
	if !input.Valid() {
		return entity.ManagedKubernetesOutputFrameReceipt{}, errs.ErrInvalid
	}
	owned := bytes.Clone(input.Payload)
	defer clear(owned)
	input.Payload = owned
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, err
	}
	stage, write, err := repository.reserveKubernetesOutput(ctx, current, binding, input)
	if err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, err
	}
	if !write {
		return kubernetesOutputReceipt(stage), nil
	}
	// The committed intent survives cancellation; the storage adapter itself permits only one PUT.
	// Committed intent переживает cancellation; storage adapter выполняет только один PUT.
	putCtx, cancel := context.WithDeadline(ctx, stage.until)
	observed, putErr := repository.putArtifactObject(putCtx, stage.key, platformrepo.ArtifactUpload{Reader: bytes.NewReader(owned), MediaType: "application/octet-stream", Digest: "sha256:" + input.PayloadDigest, SizeBytes: int64(input.ByteCount)})
	cancel()
	state := enum.KubernetesOutputUnknown
	if putErr == nil {
		state = enum.KubernetesOutputRejected
		if validKubernetesOutputObject(stage.key, input.PayloadDigest, input.ByteCount, observed) {
			stage.version, stage.etag = observed.VersionID, observed.ETag
			state = enum.KubernetesOutputStaged
		}
	}
	cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelCleanup()
	if err := repository.recordKubernetesOutput(cleanup, current.organizationID, stage, state); err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, err
	}
	stage.receipt.State = state
	if state != enum.KubernetesOutputStaged {
		return kubernetesOutputReceipt(stage), nil
	}
	admitted, _, err := repository.reserveKubernetesOutput(ctx, current, binding, input)
	if !accessInfrastructureFailure(err) && (errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound)) {
		return kubernetesOutputReceipt(stage), nil
	}
	if err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, err
	}
	return kubernetesOutputReceipt(admitted), nil
}

// lockKubernetesOutputCredit locks the canonical graph, stream/effect and exact current frame in
// that order. It rechecks child grants, registered pins, ordinal and deadline without issuing credit.
// lockKubernetesOutputCredit блокирует canonical graph, stream/effect и exact current frame
// в этом порядке. Повторно проверяет child grants/pins/ordinal/deadline без выдачи credit.
func (repository *Repository) lockKubernetesOutputCredit(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesAcceptOutputFrame) (streamID string, until time.Time, digest string, resultErr error) {
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		return "", time.Time{}, "", err
	}
	if input.Channel == "PORT_FORWARD" && len(input.Payload) > 0 {
		profile, profileErr := managedkubernetes.DecodePortForwardProfile(owner.input)
		frame, frameErr := managedkubernetes.DecodePortForwardConnectionFrame(input.Payload)
		if owner.candidate.operation != "kubernetes.port-forward" || profileErr != nil || frameErr != nil || frame.ValidateOutput(len(profile.Ports)) != nil {
			return "", time.Time{}, "", errs.ErrInvalid
		}
	}
	digest, err = managedkubernetes.BindingDigest(binding)
	if err != nil {
		return "", time.Time{}, "", errs.ErrInvalid
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return "", time.Time{}, "", err
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.candidate.id, "binding_digest": digest, "snapshot_digest": snapshot, "stream_ref": input.StreamRef, "generation": binding.Generation, "lease_expires_at": owner.lease.ExpiresAt, "channel": input.Channel}
	var stream kubernetesFrameStream
	err = tx.QueryRow(ctx, queryKubernetesFrameStream, args).Scan(&stream.id, &stream.state, &stream.expires, &stream.maximumFrame, &stream.maximumTotal, &stream.logicalBytes, &stream.deliveredBytes, &stream.inputSequence, &stream.outputSequence, &stream.inputChannels, &stream.outputChannels, &stream.stdinClosed, &stream.previousInputPending, &stream.previousOutputPending, &stream.now)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, "", errs.ErrForbidden
	}
	if err != nil {
		return "", time.Time{}, "", errs.ErrUnavailable
	}
	if stream.state != "ACTIVE" {
		return "", time.Time{}, "", errs.ErrForbidden
	}
	if input.Channel == "PORT_FORWARD" {
		stored, err := readKubernetesPortForwardFrameTx(ctx, tx, current.organizationID, stream.id, "OUTPUT", input.Sequence)
		if err != nil {
			return "", time.Time{}, "", err
		}
		if !stored.openAdmitted || stored.digest != input.PayloadDigest || stored.byteCount != input.ByteCount {
			return "", time.Time{}, "", errs.ErrForbidden
		}
		if len(input.Payload) > 0 {
			native, err := managedkubernetes.PortForwardOutputWitness(input.Payload)
			if err != nil || native.ConnectionRef != stored.ref || native.Generation != stored.generation || uint32(native.Slot) != stored.slot || native.NativeOrdinal != stored.nativeOrdinal || kubernetesPortForwardActionName(native.Action) != stored.action {
				return "", time.Time{}, "", errs.ErrInvalid
			}
		}
	}
	creditArgs := pgx.StrictNamedArgs{"stream_id": stream.id, "organization_id": current.organizationID, "sequence": input.Sequence, "delivery_ordinal": input.DeliveryOrdinal, "channel": input.Channel, "payload_digest": input.PayloadDigest, "byte_count": input.ByteCount}
	err = tx.QueryRow(ctx, queryKubernetesOutputCredit, creditArgs).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", time.Time{}, "", errs.ErrForbidden
	}
	if err != nil {
		return "", time.Time{}, "", errs.ErrUnavailable
	}
	if stream.expires.Before(until) {
		return "", time.Time{}, "", errs.ErrForbidden
	}
	return stream.id, until, digest, nil
}

// reserveKubernetesOutput checks the canonical graph and exact current credit before reserving or
// re-admitting the same known version. A later ordinal can reuse bytes but cannot repeat an unknown PUT.
// reserveKubernetesOutput проверяет canonical graph и exact current credit перед reservation либо
// повторным допуском known version. Новый ordinal использует bytes, не повторяя unknown PUT.
func (repository *Repository) reserveKubernetesOutput(ctx context.Context, current scope, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesAcceptOutputFrame) (stage kubernetesOutputStage, write bool, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return stage, false, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	streamID, until, digest, err := repository.lockKubernetesOutputCredit(ctx, tx, current, binding, input)
	if err != nil {
		return stage, false, err
	}
	stage.until = until
	stage.receipt = entity.ManagedKubernetesOutputFrameReceipt{InvocationRef: binding.InvocationRef, StreamRef: input.StreamRef, Generation: binding.Generation, Sequence: input.Sequence, BindingDigest: digest}
	existingArgs := pgx.StrictNamedArgs{"stream_id": streamID, "organization_id": current.organizationID, "sequence": input.Sequence}
	var previousUntil time.Time
	err = tx.QueryRow(ctx, queryKubernetesOutputExisting, existingArgs).Scan(&stage.id, &stage.receipt.Ref, &stage.receipt.Channel, &stage.receipt.PayloadDigest, &stage.receipt.ByteCount, &stage.receipt.DeliveryOrdinal, &stage.receipt.State, &previousUntil, &stage.key, &stage.version, &stage.etag)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return stage, false, errs.ErrUnavailable
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := tx.Exec(ctx, queryKubernetesOutputQuotaLock, pgx.StrictNamedArgs{"organization_id": current.organizationID}); err != nil {
			return stage, false, errs.ErrUnavailable
		}
		var total, count int64
		if err := tx.QueryRow(ctx, queryKubernetesOutputQuota, pgx.StrictNamedArgs{"organization_id": current.organizationID}).Scan(&total, &count); err != nil {
			return stage, false, errs.ErrUnavailable
		}
		if total+int64(input.ByteCount) > maximumKubernetesStagedBytes || count >= int64(managedkubernetes.MaximumStagedObjects) {
			return stage, false, errs.ErrKubernetesStagingQuota
		}
		stage.receipt.Ref, err = newRef("kof")
		if err != nil {
			return stage, false, err
		}
		stage.receipt.Channel, stage.receipt.PayloadDigest, stage.receipt.ByteCount, stage.receipt.DeliveryOrdinal, stage.receipt.State = input.Channel, input.PayloadDigest, input.ByteCount, input.DeliveryOrdinal, enum.KubernetesOutputAttempting
		stage.key = "organizations/" + current.organizationID + "/kubernetes-output-stages/" + stage.receipt.Ref
		args := pgx.StrictNamedArgs{"ref": stage.receipt.Ref, "organization_id": current.organizationID, "stream_id": streamID, "sequence": input.Sequence, "channel": input.Channel, "payload_digest": input.PayloadDigest, "byte_count": input.ByteCount, "delivery_ordinal": input.DeliveryOrdinal, "authorized_until": stage.until, "object_key": stage.key}
		if err := tx.QueryRow(ctx, queryKubernetesOutputReserve, args).Scan(&stage.id); err != nil {
			return stage, false, mapWriteError(err)
		}
		write = true
	} else {
		if stage.receipt.Channel != input.Channel || stage.receipt.PayloadDigest != input.PayloadDigest || stage.receipt.ByteCount != input.ByteCount {
			return stage, false, errs.ErrConflict
		}
		if stage.receipt.State == enum.KubernetesOutputStaged || stage.receipt.State == enum.KubernetesOutputAccepted {
			args := pgx.StrictNamedArgs{"id": stage.id, "organization_id": current.organizationID, "delivery_ordinal": input.DeliveryOrdinal, "authorized_until": stage.until}
			tag, err := tx.Exec(ctx, queryKubernetesOutputAdmit, args)
			if err != nil {
				return stage, false, mapWriteError(err)
			}
			if tag.RowsAffected() != 1 {
				return stage, false, errs.ErrConflict
			}
			stage.receipt.State, stage.receipt.DeliveryOrdinal = enum.KubernetesOutputAccepted, input.DeliveryOrdinal
		} else {
			if stage.receipt.DeliveryOrdinal != input.DeliveryOrdinal {
				return stage, false, errs.ErrConflict
			}
			stage.until = previousUntil
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return stage, false, mapWriteError(err)
	}
	return stage, write, nil
}

// recordKubernetesOutput preserves one late materialization fact without minting any current credit.
// A missing/failed durable write stays unreported and never authorizes a second object PUT.
// recordKubernetesOutput сохраняет один late materialization fact без current credit.
// Отсутствующий/failed durable write остаётся unreported и не разрешает второй object PUT.
func (repository *Repository) recordKubernetesOutput(ctx context.Context, org string, stage kubernetesOutputStage, state enum.ManagedKubernetesOutputFrameState) error {
	args := pgx.StrictNamedArgs{"id": stage.id, "organization_id": org, "ref": stage.receipt.Ref, "state": string(state), "object_version": stage.version, "object_etag": stage.etag}
	tag, err := repository.pool.Exec(ctx, queryKubernetesOutputRecord, args)
	if err != nil {
		return mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}

// GetManagedKubernetesOutputFrame reconciles only the original metadata tuple. Closed or superseded
// delivery grants are projected without credit; no bytes, new PUT, artifact or extended deadline exists.
// GetManagedKubernetesOutputFrame сверяет лишь original metadata tuple. Closed/superseded grants
// возвращаются без credit; bytes/new PUT/artifact/extended deadline отсутствуют.
func (repository *Repository) GetManagedKubernetesOutputFrame(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesOutputFrameLookup) (result entity.ManagedKubernetesOutputFrameReceipt, resultErr error) {
	if err := kubernetesInvocationGatewayCaller(p, binding, "platform.runtime.kubernetes.output.read"); err != nil {
		return result, err
	}
	if !input.Valid() {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	args, err := kubernetesInvocationCompletionArgs(current.organizationID, binding)
	if err != nil {
		return result, err
	}
	args["stream_ref"], args["sequence"], args["delivery_ordinal"] = input.StreamRef, input.Sequence, input.DeliveryOrdinal
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	var until time.Time
	var eligible bool
	err = tx.QueryRow(ctx, queryKubernetesOutputRead, args).Scan(&result.Ref, &result.Channel, &result.PayloadDigest, &result.ByteCount, &result.State, &until, &eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	result.InvocationRef, result.StreamRef, result.Generation, result.Sequence, result.DeliveryOrdinal = binding.InvocationRef, input.StreamRef, binding.Generation, input.Sequence, input.DeliveryOrdinal
	result.BindingDigest, _ = managedkubernetes.BindingDigest(binding)
	if result.State == enum.KubernetesOutputAccepted {
		if eligible {
			admitted := command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: input, Channel: result.Channel, PayloadDigest: result.PayloadDigest, ByteCount: result.ByteCount}
			streamID, currentUntil, _, currentErr := repository.lockKubernetesOutputCredit(ctx, tx, current, binding, admitted)
			if currentErr != nil && (accessInfrastructureFailure(currentErr) || !errors.Is(currentErr, errs.ErrForbidden) && !errors.Is(currentErr, errs.ErrNotFound)) {
				return result, currentErr
			}
			eligible = currentErr == nil && currentUntil.Equal(until)
			if eligible {
				var stored kubernetesOutputStage
				stored.receipt = result
				args := pgx.StrictNamedArgs{"stream_id": streamID, "organization_id": current.organizationID, "sequence": input.Sequence}
				scanErr := tx.QueryRow(ctx, queryKubernetesOutputExisting, args).Scan(&stored.id, &stored.receipt.Ref, &stored.receipt.Channel, &stored.receipt.PayloadDigest, &stored.receipt.ByteCount, &stored.receipt.DeliveryOrdinal, &stored.receipt.State, &stored.until, &stored.key, &stored.version, &stored.etag)
				if scanErr != nil {
					return result, errs.ErrUnavailable
				}
				eligible = stored.receipt == result && stored.until.Equal(until) && stored.version != "" && stored.etag != ""
			}
		}
		if eligible {
			result.AuthorizedUntil = until
		} else {
			result.State = enum.KubernetesOutputCancelled
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedKubernetesOutputFrameReceipt{}, mapWriteError(err)
	}
	return result, nil
}

// validKubernetesOutputObject admits only the exact owner key, bytes and nonempty bounded version.
// Malformed successful receipts remain rejected facts; no foreign key is adopted or deleted.
// validKubernetesOutputObject допускает лишь exact owner key/bytes и nonempty bounded version.
// Malformed successful receipt остаётся rejected fact; foreign key не принимается и не удаляется.
func validKubernetesOutputObject(key, digest string, size uint32, receipt objectstorage.Receipt) bool {
	return receipt.Key == key && receipt.Digest == "sha256:"+digest && receipt.SizeBytes == int64(size) &&
		len(receipt.VersionID) > 0 && !strings.EqualFold(receipt.VersionID, "null") && len(receipt.VersionID) <= 1024 && utf8.ValidString(receipt.VersionID) && !strings.ContainsRune(receipt.VersionID, '\x00') &&
		len(receipt.ETag) > 0 && len(receipt.ETag) <= 1024 && utf8.ValidString(receipt.ETag) && !strings.ContainsRune(receipt.ETag, '\x00')
}
