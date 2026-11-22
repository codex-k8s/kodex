package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesEffectRecord reads the exact child and its single historical attempt.
// queryKubernetesEffectRecord читает exact child и его единственную historical attempt.
//
//go:embed sql/kubernetes_execution__effect_record.sql
var queryKubernetesEffectRecord string

// queryKubernetesEffectInsert durably records ATTEMPTING before one external attempt.
// queryKubernetesEffectInsert durable фиксирует ATTEMPTING до одного external attempt.
//
//go:embed sql/kubernetes_execution__effect_insert.sql
var queryKubernetesEffectInsert string

// queryKubernetesStreamInsert reserves finite stream credit separately from the short handshake lease.
// queryKubernetesStreamInsert резервирует finite stream credit отдельно от short handshake lease.
//
//go:embed sql/kubernetes_execution__stream_insert.sql
var queryKubernetesStreamInsert string

// kubernetesEffectRecord separates immutable child data from accepted output and mutable admission.
// kubernetesEffectRecord разделяет immutable child data, accepted output и mutable admission.
type kubernetesEffectRecord struct {
	childID, operation, inputDigest, inputJSON, risk, approvalPolicy string
	deadline, now                                                    time.Time
	current                                                          bool
	effectID, effectRef, state                                       string
	generation                                                       int64
	admissionExpires                                                 *time.Time
	resultJSON, resultDigest, submissionDigest, outcome, reason      string
	applied                                                          bool
	streamRef                                                        string
	streamDeadline                                                   *time.Time
	streamGeneration                                                 int64
	maximumFrame                                                     uint32
	maximumTotal                                                     uint64
	inputChannels, outputChannels                                    []string
}

// readKubernetesEffectRecordTx checks owner-assigned plan/child keys without fabricating an attempt.
// readKubernetesEffectRecordTx проверяет owner-assigned plan/child keys без выдуманной attempt.
func readKubernetesEffectRecordTx(ctx context.Context, tx pgx.Tx, owner kubernetesOperationOwner, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginEffect) (record kubernetesEffectRecord, resultErr error) {
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return record, errs.ErrInvalid
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return record, err
	}
	args := pgx.StrictNamedArgs{"organization_id": owner.current.organizationID, "invocation_id": owner.candidate.id, "binding_digest": bindingDigest, "snapshot_digest": snapshot, "plan_ref": input.PlanRef, "plan_digest": input.PlanDigest, "child_ref": input.ChildRef, "child_input_digest": input.ChildInputDigest}
	err = tx.QueryRow(ctx, queryKubernetesEffectRecord, args).Scan(&record.childID, &record.operation, &record.inputDigest, &record.inputJSON, &record.risk, &record.approvalPolicy, &record.deadline, &record.current, &record.effectID, &record.effectRef, &record.generation, &record.state, &record.admissionExpires, &record.resultJSON, &record.resultDigest, &record.submissionDigest, &record.outcome, &record.reason, &record.applied, &record.streamRef, &record.streamDeadline, &record.streamGeneration, &record.maximumFrame, &record.maximumTotal, &record.inputChannels, &record.outputChannels, &record.now)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, errs.ErrForbidden
	}
	if err != nil {
		return record, errs.ErrUnavailable
	}
	return record, nil
}

// streamGrant returns only the immutable reservation; it never refreshes the hard stream lifetime.
// streamGrant возвращает только immutable reservation без продления hard stream lifetime.
func (record kubernetesEffectRecord) streamGrant() *entity.ManagedKubernetesStreamGrant {
	if record.streamRef == "" || record.streamDeadline == nil {
		return nil
	}
	return &entity.ManagedKubernetesStreamGrant{Ref: record.streamRef, Generation: record.streamGeneration, ExpiresAt: *record.streamDeadline, MaximumFrameBytes: record.maximumFrame, MaximumTotalBytes: record.maximumTotal, InputChannels: append([]string(nil), record.inputChannels...), OutputChannels: append([]string(nil), record.outputChannels...)}
}

// BeginManagedKubernetesEffect grants one durable first attempt or an exact historical readback.
// Pending HumanGate commits no effect. ATTEMPTING never becomes another attempt after lost ACK;
// accepted output requires fresh owner/child eligibility and a new bounded output deadline.
// BeginManagedKubernetesEffect выдаёт durable first attempt либо exact historical readback.
// Pending HumanGate не создаёт effect. ATTEMPTING не становится новой attempt после lost ACK;
// accepted output требует fresh owner/child eligibility и нового bounded output deadline.
func (repository *Repository) BeginManagedKubernetesEffect(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesBeginEffect) (result entity.ManagedKubernetesEffectAdmission, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.effect.begin"); err != nil {
		return result, err
	}
	if input.PlanRevision != 1 {
		return result, errs.ErrConflict
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
	record, err := readKubernetesEffectRecordTx(ctx, tx, owner, binding, input)
	if err != nil {
		return result, err
	}
	if !record.current || !record.deadline.After(record.now) {
		return result, errs.ErrForbidden
	}
	expires := owner.lease.ExpiresAt
	if limit := record.now.Add(30 * time.Second); limit.Before(expires) {
		expires = limit
	}
	if record.deadline.Before(expires) {
		expires = record.deadline
	}
	if !expires.After(record.now) {
		return result, errs.ErrForbidden
	}
	if record.effectRef != "" {
		result = entity.ManagedKubernetesEffectAdmission{Ref: record.effectRef, Generation: record.generation, InputDigest: record.inputDigest, ExpiresAt: expires, ReconcileOnly: true}
		if record.streamRef != "" {
			stream, streamErr := readKubernetesStreamRecordTx(ctx, tx, current, binding, record.streamRef)
			if streamErr != nil {
				return entity.ManagedKubernetesEffectAdmission{}, streamErr
			}
			result.History, err = stream.history(binding, record.streamRef)
			if err != nil {
				return entity.ManagedKubernetesEffectAdmission{}, err
			}
		}
		if record.applied {
			if managedkubernetes.ValidateEffectResult([]byte(record.resultJSON), record.resultDigest, record.outcome, record.reason) != nil || record.state != record.outcome || record.resultDigest != record.submissionDigest {
				return entity.ManagedKubernetesEffectAdmission{}, errs.ErrUnavailable
			}
			result.AcceptedResult = &entity.ManagedKubernetesAcceptedEffectResult{Outcome: enum.ManagedKubernetesEffectOutcome(record.outcome), ResultJSON: []byte(record.resultJSON), ResultDigest: record.resultDigest, ReasonCode: record.reason}
			result.Stream = nil
		}
		if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
			if result.AcceptedResult != nil {
				clear(result.AcceptedResult.ResultJSON)
			}
			return entity.ManagedKubernetesEffectAdmission{}, creditErr
		}
		if tx.Commit(ctx) != nil {
			return entity.ManagedKubernetesEffectAdmission{}, errs.ErrUnavailable
		}
		if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
			if result.AcceptedResult != nil {
				clear(result.AcceptedResult.ResultJSON)
			}
			return entity.ManagedKubernetesEffectAdmission{}, creditErr
		}
		return result, nil
	}
	if record.approvalPolicy == "HUMAN_EACH_EFFECT" {
		waiting, err := repository.requireKubernetesChildGateTx(ctx, tx, owner, input.PlanRef, record.childID, input.ChildRef)
		if err != nil {
			return result, err
		}
		if waiting != nil {
			if tx.Commit(ctx) != nil {
				return result, errs.ErrUnavailable
			}
			return result, waiting
		}
	}
	fileSource, err := repository.checkKubernetesFileBeginTx(ctx, tx, owner, binding, input, record)
	if err != nil {
		return result, err
	}
	if fileSource.id != "" && fileSource.until.Before(expires) {
		expires = fileSource.until
	}
	ref, err := newRef("kef")
	if err != nil {
		return result, err
	}
	var effectID string
	var admittedUntil time.Time
	if err := tx.QueryRow(ctx, queryKubernetesEffectInsert, pgx.StrictNamedArgs{"effect_ref": ref, "organization_id": current.organizationID, "child_id": record.childID, "authorized_until": expires}).Scan(&effectID, &admittedUntil); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, errs.ErrForbidden
		}
		return result, mapWriteError(err)
	}
	result = entity.ManagedKubernetesEffectAdmission{Ref: ref, Generation: 1, InputDigest: record.inputDigest, ExpiresAt: admittedUntil}
	descriptor, known := integrationpackage.KubernetesOperationByKey(record.operation)
	if !known {
		return entity.ManagedKubernetesEffectAdmission{}, errs.ErrUnavailable
	}
	if descriptor.Execution == integrationpackage.KubernetesStream {
		streamRef, err := newRef("kst")
		if err != nil {
			return result, err
		}
		streamDeadline := record.deadline
		if !owner.accessUntil.IsZero() && owner.accessUntil.Before(streamDeadline) {
			streamDeadline = owner.accessUntil
		}
		if fileSource.id != "" && !fileSource.accessUntil.IsZero() && fileSource.accessUntil.Before(streamDeadline) {
			streamDeadline = fileSource.accessUntil
		}
		inputs, outputs := kubernetesStreamChannels(record.operation, []byte(record.inputJSON))
		if len(outputs) == 0 {
			return result, errs.ErrUnavailable
		}
		// maximumTotalBytes bounds logical output and all retained-buffer deliveries together.
		// maximumTotalBytes ограничивает logical output и все retained-buffer deliveries вместе.
		const maximumTotalBytes uint64 = 16 << 20
		tag, err := tx.Exec(ctx, queryKubernetesStreamInsert, pgx.StrictNamedArgs{"stream_ref": streamRef, "organization_id": current.organizationID, "effect_id": effectID, "generation": binding.Generation, "hard_deadline": streamDeadline, "maximum_total_bytes": maximumTotalBytes, "input_channels": inputs, "output_channels": outputs})
		if err != nil {
			return result, mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return result, errs.ErrForbidden
		}
		if fileSource.id != "" {
			tag, err := tx.Exec(ctx, queryKubernetesFileBindStream, pgx.StrictNamedArgs{"source_id": fileSource.id, "organization_id": current.organizationID, "stream_ref": streamRef})
			if err != nil || tag.RowsAffected() != 1 {
				return result, errs.ErrForbidden
			}
		}
		result.Stream = &entity.ManagedKubernetesStreamGrant{Ref: streamRef, Generation: binding.Generation, ExpiresAt: streamDeadline, MaximumFrameBytes: managedkubernetes.MaximumFrameBytes, MaximumTotalBytes: maximumTotalBytes, InputChannels: inputs, OutputChannels: outputs}
	}
	if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
		return entity.ManagedKubernetesEffectAdmission{}, creditErr
	}
	if tx.Commit(ctx) != nil {
		return entity.ManagedKubernetesEffectAdmission{}, errs.ErrUnavailable
	}
	if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
		return entity.ManagedKubernetesEffectAdmission{}, creditErr
	}
	return result, nil
}

// kubernetesStreamChannels binds one registered operation to its finite transport channel family.
// Input presence still follows validated options in the native consumer; no arbitrary channel is admitted.
// kubernetesStreamChannels связывает registered operation с finite transport channel family.
// Input presence отдельно следует validated options native consumer; arbitrary channel не допускается.
func kubernetesStreamChannels(operation string, raw []byte) ([]string, []string) {
	input, _, err := managedkubernetes.DecodeOperationInput(operation, raw)
	if err != nil {
		return nil, nil
	}
	var options struct {
		Stdin bool `json:"stdin"`
		TTY   bool `json:"tty"`
	}
	if len(input.Options) != 0 && json.Unmarshal(input.Options, &options) != nil {
		return nil, nil
	}
	switch operation {
	case "kubernetes.watch", "kubernetes.watch.all-namespaces", "kubernetes.logs":
		return []string{}, []string{"STDOUT"}
	case "kubernetes.exec", "kubernetes.attach", "kubernetes.debug":
		inputs := []string{}
		if options.Stdin {
			inputs = append(inputs, "STDIN")
		}
		if options.TTY {
			inputs = append(inputs, "TERMINAL_RESIZE")
		}
		return inputs, []string{"STDOUT", "STDERR", "CONTROL"}
	case "kubernetes.port-forward":
		return []string{"PORT_FORWARD"}, []string{"PORT_FORWARD", "CONTROL"}
	case "kubernetes.cp.upload":
		return []string{"FILE_CONTENT"}, []string{"STDOUT", "STDERR", "CONTROL"}
	case "kubernetes.cp.download":
		return []string{}, []string{"FILE_CONTENT", "STDERR", "CONTROL"}
	default:
		return nil, nil
	}
}
