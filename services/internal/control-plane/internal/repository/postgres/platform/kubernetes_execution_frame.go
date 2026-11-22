package platform

import (
	"context"
	_ "embed"
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesFrameStream locks the exact mutable stream after canonical current owner admission.
// queryKubernetesFrameStream блокирует exact mutable stream после canonical current owner admission.
//
//go:embed sql/kubernetes_execution__frame_stream.sql
var queryKubernetesFrameStream string

// queryKubernetesFrameRead locks only an already-created sequence; it never creates credit.
// queryKubernetesFrameRead блокирует только already-created sequence; не создаёт credit.
//
//go:embed sql/kubernetes_execution__frame_read.sql
var queryKubernetesFrameRead string

// queryKubernetesFrameInsert records the immutable payload tuple before its first external delivery.
// queryKubernetesFrameInsert записывает immutable payload tuple до первой external delivery.
//
//go:embed sql/kubernetes_execution__frame_insert.sql
var queryKubernetesFrameInsert string

// queryKubernetesFrameAdvance updates one sequence and both logical and actual delivery budgets.
// queryKubernetesFrameAdvance меняет одну sequence и бюджеты logical и actual delivery.
//
//go:embed sql/kubernetes_execution__frame_advance.sql
var queryKubernetesFrameAdvance string

// queryKubernetesFrameReplay issues only another bounded delivery of the exact retained OUTPUT tuple.
// queryKubernetesFrameReplay выдаёт лишь следующую bounded delivery exact retained OUTPUT tuple.
//
//go:embed sql/kubernetes_execution__frame_replay.sql
var queryKubernetesFrameReplay string

// queryKubernetesFrameAcknowledge retains a late exact delivery fact under its original private claim.
// It does not require a live lease and cannot reopen a stream, renew credit or change payload metadata.
// queryKubernetesFrameAcknowledge сохраняет late exact delivery fact под original private claim.
// Не требует live lease и не открывает stream, не продлевает credit, не меняет payload metadata.
//
//go:embed sql/kubernetes_execution__frame_acknowledge.sql
var queryKubernetesFrameAcknowledge string

// kubernetesFrameStream carries locked accounting metadata without payload or credential material.
// kubernetesFrameStream хранит locked accounting metadata без payload либо credential material.
type kubernetesFrameStream struct {
	id, state                                                                 string
	expires, now                                                              time.Time
	maximumFrame                                                              uint32
	maximumTotal, logicalBytes, deliveredBytes, inputSequence, outputSequence uint64
	inputChannels, outputChannels                                             []string
	stdinClosed, previousInputPending, previousOutputPending                  bool
}

// kubernetesFrameTuple retains one immutable frame and its bounded delivery/acknowledgement facts.
// kubernetesFrameTuple хранит один immutable frame и bounded delivery/acknowledgement facts.
type kubernetesFrameTuple struct {
	channel, digest string
	byteCount       uint32
	endOfInput      bool
	deliveries      int
	acknowledged    bool
}

// BeginManagedKubernetesFrame grants one fresh bounded sequence after the full current owner graph.
// INPUT uncertainty never authorizes resend; OUTPUT replay requires the identical tuple and fresh
// eligibility, is capped at four deliveries, and consumes actual bytes without recounting logical bytes.
// BeginManagedKubernetesFrame выдаёт одну fresh bounded sequence после полного current owner graph.
// INPUT uncertainty не разрешает resend; OUTPUT replay требует identical tuple и fresh eligibility,
// ограничен четырьмя deliveries и расходует actual bytes без повторного учёта logical bytes.
func (repository *Repository) BeginManagedKubernetesFrame(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginFrame) (result entity.ManagedKubernetesFrameAdmission, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.frame.begin"); err != nil {
		return result, err
	}
	if !managedkubernetes.ValidLocator(input.StreamRef) || len(input.StreamRef) > 96 || !managedkubernetes.ValidDigest(input.PayloadDigest) || !managedkubernetes.ValidChannel(input.Channel) ||
		(input.Direction != enum.KubernetesFrameInput && input.Direction != enum.KubernetesFrameOutput) || !managedkubernetes.ValidFrameSequence(input.Sequence) || input.ByteCount > managedkubernetes.MaximumFrameBytes || (input.ByteCount == 0 && !input.EndOfInput) || input.EndOfInput && input.Direction != enum.KubernetesFrameInput || (input.Direction == enum.KubernetesFrameOutput && input.NativeAuthorizedUntil.IsZero()) || (input.Direction == enum.KubernetesFrameInput && !input.NativeAuthorizedUntil.IsZero()) {
		return result, errs.ErrInvalid
	}
	if input.Direction == enum.KubernetesFrameOutput && input.Channel == "PORT_FORWARD" {
		if input.PortForward == nil || input.PortForward.ValidateOutput(input.ByteCount, managedkubernetes.MaximumForwardPorts) != nil {
			return result, errs.ErrInvalid
		}
	} else if input.PortForward != nil {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		return result, err
	}
	if input.Channel == "PORT_FORWARD" {
		profile, profileErr := managedkubernetes.DecodePortForwardProfile(owner.input)
		if owner.candidate.operation != "kubernetes.port-forward" || profileErr != nil || input.PortForward != nil && int(input.PortForward.Slot) >= len(profile.Ports) {
			return result, errs.ErrInvalid
		}
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return result, errs.ErrInvalid
	}
	snapshotDigest, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return result, err
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": owner.candidate.id, "binding_digest": bindingDigest, "snapshot_digest": snapshotDigest, "stream_ref": input.StreamRef, "generation": binding.Generation, "lease_expires_at": owner.lease.ExpiresAt, "channel": input.Channel}
	var stream kubernetesFrameStream
	err = tx.QueryRow(ctx, queryKubernetesFrameStream, args).Scan(&stream.id, &stream.state, &stream.expires, &stream.maximumFrame, &stream.maximumTotal, &stream.logicalBytes, &stream.deliveredBytes, &stream.inputSequence, &stream.outputSequence, &stream.inputChannels, &stream.outputChannels, &stream.stdinClosed, &stream.previousInputPending, &stream.previousOutputPending, &stream.now)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if stream.state != "ACTIVE" || input.ByteCount > stream.maximumFrame {
		return result, errs.ErrForbidden
	}
	if input.Direction == enum.KubernetesFrameOutput {
		stream.expires, err = kubernetesOutputFrameDeadline(input.NativeAuthorizedUntil, stream.now, stream.expires)
		if err != nil {
			return result, err
		}
	}
	sequence, channels, pending := stream.inputSequence, stream.inputChannels, stream.previousInputPending
	if input.Direction == enum.KubernetesFrameOutput {
		sequence, channels, pending = stream.outputSequence, stream.outputChannels, stream.previousOutputPending
	}
	if !slices.Contains(channels, input.Channel) {
		return result, errs.ErrForbidden
	}
	frameArgs := pgx.StrictNamedArgs{"stream_id": stream.id, "organization_id": current.organizationID, "direction": string(input.Direction), "sequence": input.Sequence}
	var tuple kubernetesFrameTuple
	err = tx.QueryRow(ctx, queryKubernetesFrameRead, frameArgs).Scan(&tuple.channel, &tuple.digest, &tuple.byteCount, &tuple.endOfInput, &tuple.deliveries, &tuple.acknowledged)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrUnavailable
	}
	if errors.Is(err, pgx.ErrNoRows) && (input.Sequence != sequence+1 || pending) {
		return result, errs.ErrConflict
	}
	retained := err == nil
	var pfRecord kubernetesPortForwardFrameRecord
	if input.Direction == enum.KubernetesFrameOutput && input.Channel == "PORT_FORWARD" {
		var closed bool
		pfRecord, closed, err = prepareKubernetesPortForwardOutputTx(ctx, tx, current.organizationID, stream.id, input, retained)
		if err != nil {
			return result, err
		}
		if closed {
			if tx.Commit(ctx) != nil {
				return result, errs.ErrUnavailable
			}
			return entity.ManagedKubernetesFrameAdmission{Disposition: enum.KubernetesFrameClosed}, nil
		}
	}
	if retained {
		if tuple.channel != input.Channel || tuple.digest != input.PayloadDigest || tuple.byteCount != input.ByteCount || tuple.endOfInput != input.EndOfInput {
			return result, errs.ErrConflict
		}
		if tuple.acknowledged {
			result.Disposition = enum.KubernetesFrameAcknowledged
		} else if input.Direction == enum.KubernetesFrameInput || tuple.deliveries >= managedkubernetes.MaximumOutputDeliveries || stream.deliveredBytes+uint64(input.ByteCount) > stream.maximumTotal {
			result.Disposition = enum.KubernetesFrameUnknown
		} else {
			replayArgs := pgx.StrictNamedArgs{"stream_id": stream.id, "organization_id": current.organizationID, "sequence": input.Sequence, "grant_expires_at": stream.expires, "byte_count": input.ByteCount}
			if _, err := tx.Exec(ctx, queryKubernetesFrameReplay, replayArgs); err != nil {
				return result, mapWriteError(err)
			}
			result.Disposition, result.ExpiresAt = enum.KubernetesFrameSend, stream.expires
			result.DeliveryOrdinal = uint32(tuple.deliveries + 1)
		}
	} else {
		if input.Sequence != sequence+1 || pending || stream.logicalBytes+uint64(input.ByteCount) > stream.maximumTotal || stream.deliveredBytes+uint64(input.ByteCount) > stream.maximumTotal || input.Direction == enum.KubernetesFrameInput && stream.stdinClosed {
			return result, errs.ErrConflict
		}
		if input.Channel == "PORT_FORWARD" {
			if err := kubernetesPortForwardNewByteBudget(ctx, tx, current.organizationID, stream.id, stream.logicalBytes, stream.maximumTotal, input.ByteCount); err != nil {
				return result, err
			}
		}
		if input.Direction == enum.KubernetesFrameInput {
			var sourceUntil time.Time
			if input.Channel == "FILE_CONTENT" {
				fileUntil, err := repository.admitKubernetesFileFrameTx(ctx, tx, current, binding, input)
				if err != nil {
					return result, err
				}
				if fileUntil.Before(stream.expires) {
					stream.expires = fileUntil
				}
			}
			err := tx.QueryRow(ctx, queryKubernetesInputAdmit, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": stream.id, "sequence": input.Sequence, "channel": input.Channel, "payload_digest": input.PayloadDigest, "byte_count": input.ByteCount, "end_of_input": input.EndOfInput}).Scan(&sourceUntil)
			if errors.Is(err, pgx.ErrNoRows) {
				return result, errs.ErrForbidden
			}
			if err != nil {
				return result, errs.ErrUnavailable
			}
			if err := admitKubernetesPortForwardInputTx(ctx, tx, current.organizationID, stream.id, input); err != nil {
				return result, err
			}
			if sourceUntil.Before(stream.expires) {
				stream.expires = sourceUntil
			}
		}
		insertArgs := pgx.StrictNamedArgs{"stream_id": stream.id, "organization_id": current.organizationID, "direction": string(input.Direction), "sequence": input.Sequence, "channel": input.Channel, "payload_digest": input.PayloadDigest, "byte_count": input.ByteCount, "end_of_input": input.EndOfInput, "grant_expires_at": stream.expires}
		if _, err := tx.Exec(ctx, queryKubernetesFrameInsert, insertArgs); err != nil {
			return result, mapWriteError(err)
		}
		if input.Direction == enum.KubernetesFrameOutput && input.Channel == "PORT_FORWARD" {
			if err := bindKubernetesPortForwardOutputTx(ctx, tx, current.organizationID, stream.id, input, pfRecord); err != nil {
				return result, err
			}
		}
		advanceArgs := pgx.StrictNamedArgs{"stream_id": stream.id, "organization_id": current.organizationID, "direction": string(input.Direction), "sequence": input.Sequence, "byte_count": input.ByteCount, "close_channel": ""}
		if input.Direction == enum.KubernetesFrameInput && input.EndOfInput {
			advanceArgs["close_channel"] = input.Channel
		}
		if _, err := tx.Exec(ctx, queryKubernetesFrameAdvance, advanceArgs); err != nil {
			return result, mapWriteError(err)
		}
		result.Disposition, result.ExpiresAt = enum.KubernetesFrameSend, stream.expires
		result.PortForwardRetention, err = kubernetesPortForwardRetentionTx(ctx, tx, current.organizationID, stream.id, input)
		if err != nil {
			return result, err
		}
		if input.Direction == enum.KubernetesFrameOutput {
			result.DeliveryOrdinal = 1
		}
	}
	if result.Disposition == enum.KubernetesFrameSend {
		if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
			return entity.ManagedKubernetesFrameAdmission{}, creditErr
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedKubernetesFrameAdmission{}, mapWriteError(err)
	}
	if result.Disposition == enum.KubernetesFrameSend {
		if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
			return entity.ManagedKubernetesFrameAdmission{}, creditErr
		}
	}
	return result, nil
}

// CompleteManagedKubernetesFrame records only an already-admitted exact frame receipt. Late ACKs
// remain facts after lease expiry/revocation; OUTPUT additionally requires the accepted private stage
// for its exact current ordinal. No state, sequence, byte budget or credit can be reopened.
// CompleteManagedKubernetesFrame записывает лишь already-admitted exact frame receipt. Late ACKs
// сохраняются после lease expiry/revocation; OUTPUT требует accepted private stage текущего ordinal.
// State, sequence, byte budget и credit не открываются.
func (repository *Repository) CompleteManagedKubernetesFrame(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCompleteFrame) (result uint64, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.frame.complete"); err != nil {
		return 0, err
	}
	if !managedkubernetes.ValidLocator(input.StreamRef) || len(input.StreamRef) > 96 || !managedkubernetes.ValidDigest(input.PayloadDigest) || (input.Direction != enum.KubernetesFrameInput && input.Direction != enum.KubernetesFrameOutput) || !managedkubernetes.ValidFrameSequence(input.Sequence) {
		return 0, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return 0, err
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return 0, errs.ErrInvalid
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "generation": binding.Generation, "input_digest": binding.InputDigest, "binding_digest": bindingDigest, "stream_ref": input.StreamRef, "direction": string(input.Direction), "sequence": input.Sequence, "payload_digest": input.PayloadDigest}
	err = tx.QueryRow(ctx, queryKubernetesFrameAcknowledge, args).Scan(&result)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errs.ErrForbidden
	}
	if err != nil {
		return 0, mapWriteError(err)
	}
	if input.Direction == enum.KubernetesFrameInput {
		if _, err := tx.Exec(ctx, queryKubernetesInputAck, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_ref": input.StreamRef, "sequence": input.Sequence, "payload_digest": input.PayloadDigest}); err != nil {
			return 0, mapWriteError(err)
		}
	}
	if err := acknowledgeKubernetesPortForwardByRefTx(ctx, tx, current.organizationID, input.StreamRef, string(input.Direction), input.Sequence, input.PayloadDigest); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, mapWriteError(err)
	}
	return result, nil
}

// kubernetesOutputFrameDeadline binds exact persisted microsecond credit to a broker-only native witness.
// It only narrows the current owner lease/hard ceiling and uses the database clock for its five-second cap.
// kubernetesOutputFrameDeadline связывает exact persisted microsecond credit с broker-only native witness.
// Только сужает current owner lease/hard ceiling и использует DB clock для five-second cap.
func kubernetesOutputFrameDeadline(nativeUntil, now, ceiling time.Time) (time.Time, error) {
	until := ceiling.UTC().Truncate(time.Microsecond)
	for _, bound := range []time.Time{nativeUntil.UTC().Truncate(time.Microsecond), now.UTC().Truncate(time.Microsecond).Add(5 * time.Second)} {
		if bound.Before(until) {
			until = bound
		}
	}
	if nativeUntil.IsZero() || !until.After(now) {
		return time.Time{}, errs.ErrForbidden
	}
	return until, nil
}
