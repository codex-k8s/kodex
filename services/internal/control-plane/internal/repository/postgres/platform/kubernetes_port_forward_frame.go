package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesPortForwardFrameRead rejoins an immutable stage/frame to its exact original incarnation under the parent lock.
// queryKubernetesPortForwardFrameRead связывает immutable stage/frame с exact original incarnation под parent lock.
//
//go:embed sql/kubernetes_pf__frame_read.sql
var queryKubernetesPortForwardFrameRead string

// queryKubernetesPortForwardWitnessRead checks native coordinates only inside the already verified original stream.
// queryKubernetesPortForwardWitnessRead проверяет native coordinates лишь внутри already verified original stream.
//
//go:embed sql/kubernetes_pf__witness_read.sql
var queryKubernetesPortForwardWitnessRead string

// queryKubernetesPortForwardOutputBind seals one actual owner OUTPUT sequence without changing its existing budget or stage engine.
// queryKubernetesPortForwardOutputBind закрепляет actual owner OUTPUT sequence без изменения existing budget либо stage engine.
//
//go:embed sql/kubernetes_pf__output_bind.sql
var queryKubernetesPortForwardOutputBind string

// queryKubernetesPortForwardInputAdmission records native OPEN eligibility only after the existing INPUT stage has been admitted.
// queryKubernetesPortForwardInputAdmission записывает native OPEN eligibility лишь после admission existing INPUT stage.
//
//go:embed sql/kubernetes_pf__input_admission.sql
var queryKubernetesPortForwardInputAdmission string

// queryKubernetesPortForwardFrameAck preserves monotonic local facts after exact existing frame ACK, including late facts without reopening.
// queryKubernetesPortForwardFrameAck сохраняет monotonic local facts после exact existing frame ACK, включая late facts без reopen.
//
//go:embed sql/kubernetes_pf__frame_ack.sql
var queryKubernetesPortForwardFrameAck string

// queryKubernetesPortForwardParentJoin records local pair cleanup only after a verified typed parent join receipt.
// queryKubernetesPortForwardParentJoin сохраняет local pair cleanup лишь после verified typed parent join receipt.
//
//go:embed sql/kubernetes_pf__parent_join.sql
var queryKubernetesPortForwardParentJoin string

// queryKubernetesPortForwardNativeOrdinal rejects an already admitted native event masquerading as a new unadmitted candidate.
// queryKubernetesPortForwardNativeOrdinal отклоняет already admitted native event, выданный за новый unadmitted candidate.
//
//go:embed sql/kubernetes_pf__native_ordinal.sql
var queryKubernetesPortForwardNativeOrdinal string

// kubernetesPortForwardFrameRecord separates immutable connection/frame binding from monotonic lifecycle observations.
// kubernetesPortForwardFrameRecord отделяет immutable connection/frame binding от monotonic lifecycle observations.
type kubernetesPortForwardFrameRecord struct {
	id, ref, state, openStage, closeStage, action, digest, stage string
	generation, nativeOrdinal                                    uint64
	slot, byteCount                                              uint32
	writeClosed, openAdmitted                                    bool
}

// readKubernetesPortForwardFrameTx reads no payload and grants no effect; every caller already owns the canonical parent stream lock.
// readKubernetesPortForwardFrameTx не читает payload и не выдаёт effect; caller уже владеет canonical parent stream lock.
func readKubernetesPortForwardFrameTx(ctx context.Context, tx pgx.Tx, org, stream, direction string, sequence uint64) (kubernetesPortForwardFrameRecord, error) {
	var record kubernetesPortForwardFrameRecord
	err := tx.QueryRow(ctx, queryKubernetesPortForwardFrameRead, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "direction": direction, "sequence": sequence}).Scan(&record.id, &record.ref, &record.generation, &record.slot, &record.state, &record.writeClosed, &record.openStage, &record.closeStage, &record.openAdmitted, &record.action, &record.nativeOrdinal, &record.digest, &record.byteCount, &record.stage)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, errs.ErrForbidden
	}
	if err != nil {
		return record, errs.ErrUnavailable
	}
	return record, nil
}

// kubernetesPortForwardActionName maps the finite transport action without accepting arbitrary SQL or caller state labels.
// kubernetesPortForwardActionName отображает finite transport action без arbitrary SQL либо caller state labels.
func kubernetesPortForwardActionName(action managedkubernetes.PortForwardConnectionAction) string {
	switch action {
	case managedkubernetes.PortForwardConnectionOpen:
		return "OPEN"
	case managedkubernetes.PortForwardConnectionData:
		return "DATA"
	case managedkubernetes.PortForwardConnectionHalfClose:
		return "HALF_CLOSE"
	case managedkubernetes.PortForwardConnectionClose:
		return "CLOSE"
	}
	return ""
}

// kubernetesPortForwardInputAllowed distinguishes staged OPEN/CLOSE from ordinary data, without promoting uncertainty into admission.
// kubernetesPortForwardInputAllowed отличает staged OPEN/CLOSE от ordinary data без повышения uncertainty до admission.
func kubernetesPortForwardInputAllowed(record kubernetesPortForwardFrameRecord) bool {
	switch record.action {
	case "OPEN":
		return record.stage == record.openStage && record.state == "PREPARED"
	case "DATA", "HALF_CLOSE":
		return record.state == "OPEN" && record.openAdmitted && !record.writeClosed
	case "CLOSE":
		return record.state == "CLOSED" && record.stage == record.closeStage && record.closeStage != "" && record.openAdmitted
	}
	return false
}

// validateKubernetesPortForwardStageTx rejects closed/foreign INPUT before source GET, and again compares whole canonical bytes before release.
// validateKubernetesPortForwardStageTx отклоняет closed/foreign INPUT до source GET и повторно сравнивает whole canonical bytes до выдачи.
func validateKubernetesPortForwardStageTx(ctx context.Context, tx pgx.Tx, org, stream string, stage kubernetesInputStage, payload []byte) error {
	if stage.metadata.Channel != "PORT_FORWARD" {
		return nil
	}
	record, err := readKubernetesPortForwardFrameTx(ctx, tx, org, stream, "INPUT", stage.metadata.Sequence)
	if err != nil {
		return err
	}
	if record.stage != stage.id || record.digest != stage.metadata.PayloadDigest || record.byteCount != stage.metadata.ByteCount || !kubernetesPortForwardInputAllowed(record) {
		return errs.ErrForbidden
	}
	if payload != nil {
		frame, err := managedkubernetes.DecodePortForwardConnectionFrame(payload)
		if err != nil || frame.ValidateInput(managedkubernetes.MaximumForwardPorts) != nil || frame.ConnectionRef != record.ref || frame.Generation != record.generation || uint32(frame.Slot) != record.slot || kubernetesPortForwardActionName(frame.Action) != record.action {
			return errs.ErrInvalid
		}
	}
	return nil
}

// validateKubernetesPortForwardPublicInputTx accepts only current DATA/half-close for the actor-owned incarnation; OPEN/CLOSE have dedicated typed commands.
// validateKubernetesPortForwardPublicInputTx принимает лишь current DATA/half-close для actor-owned incarnation; OPEN/CLOSE имеют dedicated typed commands.
func validateKubernetesPortForwardPublicInputTx(ctx context.Context, tx pgx.Tx, current scope, parent kubernetesInputRecord, input command.KubernetesStreamInputSubmit) (string, string, error) {
	if input.Channel != "PORT_FORWARD" {
		return "", "", nil
	}
	frame, err := managedkubernetes.DecodePortForwardConnectionFrame(input.Payload)
	if err != nil || frame.ValidateInput(managedkubernetes.MaximumForwardPorts) != nil || frame.Action != managedkubernetes.PortForwardConnectionData && frame.Action != managedkubernetes.PortForwardConnectionHalfClose {
		return "", "", errs.ErrInvalid
	}
	row, err := readKubernetesPortForwardConnection(ctx, tx, current, parent, frame.ConnectionRef, "")
	if err != nil {
		return "", "", err
	}
	if uint64(row.view.Generation) != frame.Generation || row.view.Slot != uint32(frame.Slot) || row.view.State != "OPEN" || !row.admitted || row.view.LocalWriteClosed {
		return "", "", errs.ErrForbidden
	}
	return row.id, kubernetesPortForwardActionName(frame.Action), nil
}

// admitKubernetesPortForwardInputTx runs after ordinary stage admission in the same transaction; OPENING therefore has a durable native admission witness.
// admitKubernetesPortForwardInputTx выполняется после ordinary stage admission в той же transaction; OPENING получает durable native admission witness.
func admitKubernetesPortForwardInputTx(ctx context.Context, tx pgx.Tx, org, stream string, input command.ManagedKubernetesBeginFrame) error {
	if input.Channel != "PORT_FORWARD" {
		return nil
	}
	record, err := readKubernetesPortForwardFrameTx(ctx, tx, org, stream, "INPUT", input.Sequence)
	if err != nil {
		return err
	}
	if record.digest != input.PayloadDigest || record.byteCount != input.ByteCount || !kubernetesPortForwardInputAllowed(record) {
		return errs.ErrForbidden
	}
	if record.action == "OPEN" {
		_, err = tx.Exec(ctx, queryKubernetesPortForwardInputAdmission, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "connection_id": record.id})
		return mapWriteError(err)
	}
	return nil
}

// acknowledgeKubernetesPortForwardFrameTx records only exact already acknowledged native facts; terminal owner state never reopens.
// acknowledgeKubernetesPortForwardFrameTx записывает лишь exact already acknowledged native facts; terminal owner state не открывается.
func acknowledgeKubernetesPortForwardFrameTx(ctx context.Context, tx pgx.Tx, org, stream, direction string, sequence uint64, digest string) error {
	record, err := readKubernetesPortForwardFrameTx(ctx, tx, org, stream, direction, sequence)
	if err != nil {
		return err
	}
	if record.digest != digest {
		return errs.ErrConflict
	}
	_, err = tx.Exec(ctx, queryKubernetesPortForwardFrameAck, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "connection_id": record.id, "direction": direction, "action": record.action})
	return mapWriteError(err)
}

// prepareKubernetesPortForwardOutputTx verifies exact admitted incarnation before new sequence credit or rejoining retained history.
// A CLOSED new candidate records a deduplicated bounded loss fact, but receives no frame, sequence, grant or ACK.
// prepareKubernetesPortForwardOutputTx проверяет exact admitted incarnation до new sequence credit либо rejoin retained history.
// CLOSED new candidate сохраняет deduplicated bounded loss fact, но не получает frame, sequence, grant либо ACK.
func prepareKubernetesPortForwardOutputTx(ctx context.Context, tx pgx.Tx, org, stream string, input command.ManagedKubernetesBeginFrame, retained bool) (kubernetesPortForwardFrameRecord, bool, error) {
	var record kubernetesPortForwardFrameRecord
	witness := input.PortForward
	if input.Channel != "PORT_FORWARD" || input.Direction != enum.KubernetesFrameOutput || witness == nil || witness.ValidateOutput(input.ByteCount, managedkubernetes.MaximumForwardPorts) != nil {
		return record, false, errs.ErrInvalid
	}
	if retained {
		var err error
		record, err = readKubernetesPortForwardFrameTx(ctx, tx, org, stream, "OUTPUT", input.Sequence)
		if err != nil {
			return record, false, err
		}
		if record.ref != witness.ConnectionRef || record.generation != witness.Generation || record.slot != uint32(witness.Slot) || record.action != kubernetesPortForwardActionName(witness.Action) || record.nativeOrdinal != witness.NativeOrdinal || record.digest != input.PayloadDigest || record.byteCount != input.ByteCount || !record.openAdmitted {
			return record, false, errs.ErrConflict
		}
		return record, false, nil
	}
	err := tx.QueryRow(ctx, queryKubernetesPortForwardWitnessRead, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "connection_ref": witness.ConnectionRef, "generation": witness.Generation}).Scan(&record.id, &record.ref, &record.generation, &record.slot, &record.state, &record.writeClosed, &record.openStage, &record.closeStage, &record.openAdmitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, false, errs.ErrForbidden
	}
	if err != nil {
		return record, false, errs.ErrUnavailable
	}
	if record.slot != uint32(witness.Slot) || !record.openAdmitted {
		return record, false, errs.ErrForbidden
	}
	var admitted bool
	if tx.QueryRow(ctx, queryKubernetesPortForwardNativeOrdinal, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "connection_id": record.id, "native_ordinal": witness.NativeOrdinal}).Scan(&admitted) != nil {
		return record, false, errs.ErrUnavailable
	}
	if admitted {
		return record, false, errs.ErrConflict
	}
	record.action, record.nativeOrdinal = kubernetesPortForwardActionName(witness.Action), witness.NativeOrdinal
	if record.state == "CLOSED" {
		err := markKubernetesCapturePartialTx(ctx, tx, org, stream, kubernetesCaptureLoss{SourceRef: record.ref, SourceGeneration: int64(record.generation), NativeOrdinal: witness.NativeOrdinal, PayloadDigest: input.PayloadDigest, ByteCount: input.ByteCount, ReasonCode: kubernetesCapturePFClosed})
		return record, err == nil, err
	}
	if record.state != "OPEN" && record.state != "OPENING" {
		return record, false, errs.ErrForbidden
	}
	return record, false, nil
}

// bindKubernetesPortForwardOutputTx attaches one already-created owner sequence to the native witness in the same transaction.
// bindKubernetesPortForwardOutputTx связывает already-created owner sequence с native witness в той же transaction.
func bindKubernetesPortForwardOutputTx(ctx context.Context, tx pgx.Tx, org, stream string, input command.ManagedKubernetesBeginFrame, record kubernetesPortForwardFrameRecord) error {
	_, err := tx.Exec(ctx, queryKubernetesPortForwardOutputBind, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream, "sequence": input.Sequence, "connection_id": record.id, "action": record.action, "native_ordinal": record.nativeOrdinal, "payload_digest": input.PayloadDigest, "byte_count": input.ByteCount})
	return mapWriteError(err)
}

// kubernetesPortForwardNewByteBudget includes distinct unadmitted native losses without refunding them into fresh INPUT/OUTPUT credit.
// kubernetesPortForwardNewByteBudget включает distinct unadmitted native losses без возврата их bytes в fresh INPUT/OUTPUT credit.
func kubernetesPortForwardNewByteBudget(ctx context.Context, tx pgx.Tx, org, stream string, logical, maximum uint64, count uint32) error {
	var used uint64
	if tx.QueryRow(ctx, queryKubernetesCaptureLossQuota, pgx.StrictNamedArgs{"organization_id": org, "stream_id": stream}).Scan(&used) != nil {
		return errs.ErrUnavailable
	}
	if logical > maximum || used > maximum-logical || uint64(count) > maximum-logical-used {
		return errs.ErrConflict
	}
	return nil
}

// queryKubernetesPortForwardFrameContext resolves only the frame already verified by the ordinary late ACK query.
// queryKubernetesPortForwardFrameContext разрешает лишь frame, уже проверенный ordinary late ACK query.
//
//go:embed sql/kubernetes_pf__frame_context.sql
var queryKubernetesPortForwardFrameContext string

// acknowledgeKubernetesPortForwardByRefTx attaches local facts after original binding ACK without demanding a new execution grant.
// acknowledgeKubernetesPortForwardByRefTx добавляет local facts после original binding ACK без требования нового execution grant.
func acknowledgeKubernetesPortForwardByRefTx(ctx context.Context, tx pgx.Tx, org, ref, direction string, sequence uint64, digest string) error {
	var stream, channel string
	if err := tx.QueryRow(ctx, queryKubernetesPortForwardFrameContext, pgx.StrictNamedArgs{"organization_id": org, "stream_ref": ref, "direction": direction, "sequence": sequence, "payload_digest": digest}).Scan(&stream, &channel); err != nil {
		return errs.ErrUnavailable
	}
	if channel != "PORT_FORWARD" {
		return nil
	}
	return acknowledgeKubernetesPortForwardFrameTx(ctx, tx, org, stream, direction, sequence, digest)
}
