package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesStreamRecord locks original stream metadata without issuing or renewing credit.
// queryKubernetesStreamRecord блокирует original stream metadata без выдачи либо продления credit.
//
//go:embed sql/kubernetes_execution__stream_record.sql
var queryKubernetesStreamRecord string

// queryKubernetesStreamOpen activates the reserved stream after its first exact handshake receipt.
// queryKubernetesStreamOpen активирует reserved stream после first exact handshake receipt.
//
//go:embed sql/kubernetes_execution__stream_open.sql
var queryKubernetesStreamOpen string

// queryKubernetesStreamClose closes all frame credit and stores one payload-free terminal receipt.
// queryKubernetesStreamClose закрывает все frame credits и хранит одну payload-free terminal receipt.
//
//go:embed sql/kubernetes_execution__stream_close.sql
var queryKubernetesStreamClose string

// kubernetesStreamRecord retains original origin pins separately from mutable local state.
// It contains no stream payload, credential material or transport URL supplied by the broker.
// kubernetesStreamRecord хранит original origin pins отдельно от mutable local state.
// Не содержит stream payload, credential material либо transport URL от broker.
type kubernetesStreamRecord struct {
	id, effectRef, effectState, snapshotDigest, inputDigest, operation, targetRef, nodeRef string
	effectGeneration, targetRevision, nodeGeneration, generation                           int64
	state                                                                                  enum.ManagedKubernetesStreamState
	effectExpires, hardDeadline, now, planDeadline                                         time.Time
	maximumFrame                                                                           uint32
	maximumTotal                                                                           uint64
	inputChannels, outputChannels                                                          []string
	handshakeDigest, handshakeProtocol, terminalDigest                                     string
	remoteOutcome                                                                          enum.ManagedKubernetesEffectOutcome
	childCurrent                                                                           bool
	invocationState                                                                        string
	leaseExpired                                                                           bool
	nodeProtocol, nodeServingKey                                                           string
	handshakeJSON, terminalJSON                                                            []byte
}

// readKubernetesStreamRecordTx validates the complete private original binding before metadata use.
// Historical reads do not require a live lease; callers must independently gate all fresh credit.
// readKubernetesStreamRecordTx проверяет полный private original binding до metadata use.
// Historical reads не требуют live lease; caller отдельно допускает весь fresh credit.
func readKubernetesStreamRecordTx(ctx context.Context, tx pgx.Tx, current scope, binding value.ManagedKubernetesBinding, streamRef string) (result kubernetesStreamRecord, resultErr error) {
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return result, errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "input_digest": binding.InputDigest, "binding_digest": digest, "generation": binding.Generation, "stream_ref": streamRef}
	return scanKubernetesStreamRecord(tx.QueryRow(ctx, queryKubernetesStreamRecord, args))
}

// scanKubernetesStreamRecord decodes one owner row for both live binding reads and sealed capture.
// scanKubernetesStreamRecord читает owner row для live binding и sealed capture одним decoder.
func scanKubernetesStreamRecord(row pgx.Row) (result kubernetesStreamRecord, resultErr error) {
	err := row.Scan(&result.id, &result.effectRef, &result.effectGeneration, &result.effectState, &result.effectExpires, &result.snapshotDigest, &result.inputDigest, &result.operation, &result.targetRef, &result.targetRevision, &result.nodeRef, &result.nodeGeneration, &result.generation, &result.state, &result.hardDeadline, &result.maximumFrame, &result.maximumTotal, &result.inputChannels, &result.outputChannels, &result.handshakeDigest, &result.handshakeProtocol, &result.terminalDigest, &result.remoteOutcome, &result.childCurrent, &result.now, &result.invocationState, &result.leaseExpired, &result.planDeadline, &result.nodeProtocol, &result.nodeServingKey, &result.handshakeJSON, &result.terminalJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

// origin reconstructs immutable receipt lineage from owner rows and the exact verified claim.
// origin восстанавливает immutable receipt lineage из owner rows и exact verified claim.
func (record kubernetesStreamRecord) origin(binding value.ManagedKubernetesBinding, streamRef string) managedkubernetes.StreamReceiptOrigin {
	digest, _ := managedkubernetes.BindingDigest(binding)
	return record.originFromDigest(digest, streamRef)
}

// originFromDigest reuses an owner-persisted binding commitment without inventing a plaintext fence.
// originFromDigest использует owner-persisted binding commitment без выдуманного plaintext fence.
func (record kubernetesStreamRecord) originFromDigest(digest, streamRef string) managedkubernetes.StreamReceiptOrigin {
	return managedkubernetes.StreamReceiptOrigin{SnapshotDigest: record.snapshotDigest, BindingDigest: digest, ChildInputDigest: record.inputDigest, EffectRef: record.effectRef, EffectGeneration: record.effectGeneration, StreamRef: streamRef, StreamGeneration: record.generation, Operation: record.operation, TargetRef: record.targetRef, TargetRevision: record.targetRevision}
}

// grant copies the original hard deadline and finite channel budgets, never a renewed lease.
// grant копирует original hard deadline и finite channel budgets, не renewed lease.
func (record kubernetesStreamRecord) grant(ref string) entity.ManagedKubernetesStreamGrant {
	return entity.ManagedKubernetesStreamGrant{Ref: ref, Generation: record.generation, ExpiresAt: record.hardDeadline, MaximumFrameBytes: record.maximumFrame, MaximumTotalBytes: record.maximumTotal, InputChannels: append([]string(nil), record.inputChannels...), OutputChannels: append([]string(nil), record.outputChannels...)}
}

// OpenManagedKubernetesStream accepts one exact observed handshake after fresh owner/child checks.
// A lost RPC ACK can replay the same receipt; it never creates a second handshake or extends time.
// OpenManagedKubernetesStream принимает один exact observed handshake после fresh owner/child checks.
// Lost RPC ACK допускает replay той же receipt без второго handshake либо продления времени.
func (repository *Repository) OpenManagedKubernetesStream(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesOpenStream) (result entity.ManagedKubernetesStreamGrant, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.stream.open"); err != nil {
		return result, err
	}
	digest, err := input.HandshakeReceipt.Digest()
	if err != nil || digest != input.HandshakeReceiptDigest || !managedkubernetes.ValidLocator(input.StreamRef) || len(input.StreamRef) > 96 {
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
	record, err := readKubernetesStreamRecordTx(ctx, tx, current, binding, input.StreamRef)
	if err != nil {
		return result, err
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return result, err
	}
	if !record.childCurrent || snapshot != record.snapshotDigest || record.effectRef != input.EffectRef || record.effectGeneration != input.EffectGeneration || record.effectState != "ATTEMPTING" || !record.hardDeadline.After(record.now) || input.HandshakeReceipt.Origin != record.origin(binding, input.StreamRef) {
		return result, errs.ErrForbidden
	}
	receipt := input.HandshakeReceipt
	if record.nodeRef != "" {
		matched := false
		for _, entry := range owner.selected.source.registry.Nodes {
			pin := entry.Transport
			if !entry.Retired && pin.Ref == record.nodeRef && pin.Generation == record.nodeGeneration && pin.TargetRef == record.targetRef && pin.TargetRevision == record.targetRevision && pin.NotBefore.Compare(record.now) <= 0 && pin.ExpiresAt.After(record.now) && receipt.TransportRef == pin.Ref && receipt.TransportGeneration == pin.Generation && receipt.Protocol == string(pin.Protocol) && receipt.PeerSPKISHA256 == pin.ServerPublicKeySHA256 {
				matched = true
			}
		}
		if !matched {
			return result, errs.ErrForbidden
		}
	} else if receipt.Protocol != managedkubernetes.HTTPWatchV1 && receipt.Protocol != managedkubernetes.HTTPLogsV1 {
		return result, errs.ErrForbidden
	}
	if record.state == enum.KubernetesStreamActive {
		if record.handshakeDigest != digest {
			return result, errs.ErrConflict
		}
	} else if record.state == enum.KubernetesStreamPrepared && record.handshakeDigest == "" && record.effectExpires.After(record.now) {
		raw, err := json.Marshal(receipt)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		tag, err := tx.Exec(ctx, queryKubernetesStreamOpen, pgx.StrictNamedArgs{"stream_id": record.id, "organization_id": current.organizationID, "handshake_digest": digest, "handshake_json": string(raw)})
		if err != nil {
			return result, mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return result, errs.ErrConflict
		}
	} else {
		return result, errs.ErrForbidden
	}
	result = record.grant(input.StreamRef)
	if !owner.accessUntil.IsZero() && owner.accessUntil.Before(result.ExpiresAt) {
		result.ExpiresAt = owner.accessUntil
	}
	if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
		return entity.ManagedKubernetesStreamGrant{}, creditErr
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedKubernetesStreamGrant{}, mapWriteError(err)
	}
	if creditErr := owner.creditError(ctx, result.ExpiresAt); creditErr != nil {
		return entity.ManagedKubernetesStreamGrant{}, creditErr
	}
	return result, nil
}

// CloseManagedKubernetesStream retains one exact terminal fact even after authority has closed.
// Infrastructure errors roll back; proven denial/expiry can only narrow local state. Late known
// remote evidence never reopens frame/effect credit or promotes another submitted receipt.
// CloseManagedKubernetesStream сохраняет один exact terminal fact даже после закрытия authority.
// Infrastructure errors вызывают rollback; proven denial/expiry только сужает local state. Late
// known remote evidence не открывает frame/effect credit и не повышает другую submitted receipt.
func (repository *Repository) CloseManagedKubernetesStream(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCloseStream) (result entity.ManagedKubernetesStreamReceipt, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.stream.close"); err != nil {
		return result, err
	}
	knownExpected := input.ExpectedState == enum.KubernetesStreamPrepared || input.ExpectedState == enum.KubernetesStreamActive || input.ExpectedState.Terminal()
	digest, err := input.TerminalReceipt.Digest()
	if err != nil || !knownExpected || digest != input.TerminalReceiptDigest || !input.TerminalState.Terminal() || !managedkubernetes.ValidLocator(input.StreamRef) || len(input.StreamRef) > 96 {
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
	_, authorityErr := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if authorityErr != nil && (accessInfrastructureFailure(authorityErr) || !errors.Is(authorityErr, errs.ErrForbidden) && !errors.Is(authorityErr, errs.ErrNotFound) && !errors.Is(authorityErr, errs.ErrConflict)) {
		return result, authorityErr
	}
	record, err := readKubernetesStreamRecordTx(ctx, tx, current, binding, input.StreamRef)
	if err != nil {
		return result, err
	}
	receipt := input.TerminalReceipt
	exitMatches := input.ObservedExitCode == nil && receipt.ExitCode == nil || input.ObservedExitCode != nil && receipt.ExitCode != nil && *input.ObservedExitCode == *receipt.ExitCode
	if receipt.Origin != record.origin(binding, input.StreamRef) || receipt.LocalState != string(input.TerminalState) || receipt.RemoteOutcome != string(input.RemoteOutcome) || receipt.TransportJoined != input.TransportJoined || !exitMatches {
		return result, errs.ErrConflict
	}
	observedDigest, observedJSON := "", ""
	if input.ObservedHandshake != nil {
		observedDigest, err = input.ObservedHandshake.Digest()
		if err != nil || observedDigest != receipt.HandshakeDigest || !record.acceptsHandshake(*input.ObservedHandshake, binding, input.StreamRef) {
			return result, errs.ErrConflict
		}
		raw, marshalErr := json.Marshal(input.ObservedHandshake)
		if marshalErr != nil {
			return result, errs.ErrUnavailable
		}
		observedJSON = string(raw)
	}
	if record.handshakeDigest != "" {
		if receipt.HandshakeDigest != record.handshakeDigest || receipt.Protocol != record.handshakeProtocol {
			return result, errs.ErrConflict
		}
	} else if input.ObservedHandshake != nil {
		if input.ExpectedState != enum.KubernetesStreamPrepared && input.ExpectedState != enum.KubernetesStreamActive || input.TerminalState == enum.KubernetesStreamCompleted {
			return result, errs.ErrConflict
		}
	} else if receipt.HandshakeDigest != "" || receipt.Protocol != record.expectedProtocol() || receipt.LocalState != "LOST" || receipt.RemoteOutcome != "UNKNOWN" {
		return result, errs.ErrConflict
	} else if receipt.ReasonCode == "HANDSHAKE_FAILED" {
		if input.ExpectedState != enum.KubernetesStreamPrepared || !receipt.TransportJoined {
			return result, errs.ErrConflict
		}
	} else if receipt.ReasonCode == "OWNER_STATE_LOST" {
		if receipt.TransportJoined || input.ExpectedState != enum.KubernetesStreamPrepared && !(record.state.Terminal() && input.ExpectedState == record.state) {
			return result, errs.ErrConflict
		}
	} else {
		return result, errs.ErrConflict
	}
	if record.terminalDigest != "" {
		if record.terminalDigest != digest || record.remoteOutcome != input.RemoteOutcome {
			return result, errs.ErrConflict
		}
		result = entity.ManagedKubernetesStreamReceipt{State: record.state, RemoteOutcome: input.RemoteOutcome}
	} else {
		state := input.TerminalState
		if record.state.Terminal() {
			state = record.state
		} else if record.state != input.ExpectedState && !(record.state == enum.KubernetesStreamPrepared && input.ExpectedState == enum.KubernetesStreamActive && input.ObservedHandshake != nil) {
			return result, errs.ErrConflict
		} else if record.invocationState == "CANCELLED" || record.invocationState == "REJECTED" {
			state = enum.KubernetesStreamCanceled
		} else if record.leaseExpired || !record.hardDeadline.After(record.now) || !record.planDeadline.After(record.now) {
			state = enum.KubernetesStreamExpired
		} else if authorityErr != nil || !record.childCurrent {
			state = enum.KubernetesStreamRevoked
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return result, errs.ErrUnavailable
		}
		tag, err := tx.Exec(ctx, queryKubernetesStreamClose, pgx.StrictNamedArgs{"stream_id": record.id, "organization_id": current.organizationID, "state": string(state), "terminal_digest": digest, "terminal_json": string(raw), "remote_outcome": string(input.RemoteOutcome), "observed_handshake_digest": observedDigest, "observed_handshake_json": observedJSON})
		if err != nil {
			return result, mapWriteError(err)
		}
		if tag.RowsAffected() != 1 {
			return result, errs.ErrConflict
		}
		result = entity.ManagedKubernetesStreamReceipt{State: state, RemoteOutcome: input.RemoteOutcome}
	}
	if input.TransportJoined {
		if _, err := tx.Exec(ctx, queryKubernetesPortForwardParentJoin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "stream_id": record.id}); err != nil {
			return result, mapWriteError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedKubernetesStreamReceipt{}, mapWriteError(err)
	}
	return result, nil
}

// expectedProtocol derives only the immutable owner's recorded transport family, including failure
// before a successful handshake. It does not infer a missing Node pin from operation input.
// expectedProtocol выводит только immutable owner transport family, включая failure до handshake.
// Отсутствующий Node pin не выводится из operation input.
func (record kubernetesStreamRecord) expectedProtocol() string {
	if record.nodeRef != "" {
		return record.nodeProtocol
	}
	switch record.operation {
	case "kubernetes.watch", "kubernetes.watch.all-namespaces":
		return managedkubernetes.HTTPWatchV1
	case "kubernetes.logs":
		return managedkubernetes.HTTPLogsV1
	default:
		return ""
	}
}

// acceptsHandshake validates late captured metadata against immutable original transport pins.
// Retirement may close authority but cannot erase an original receipt or authorize another stream.
// acceptsHandshake проверяет late captured metadata по immutable original transport pins.
// Retirement закрывает authority, но не стирает original receipt и не разрешает другой stream.
func (record kubernetesStreamRecord) acceptsHandshake(receipt managedkubernetes.HandshakeReceipt, binding value.ManagedKubernetesBinding, streamRef string) bool {
	return record.acceptsHandshakeOrigin(receipt, record.origin(binding, streamRef))
}

// acceptsHandshakeOrigin validates historical native evidence against the exact owner origin.
// acceptsHandshakeOrigin проверяет historical native evidence по exact owner origin.
func (record kubernetesStreamRecord) acceptsHandshakeOrigin(receipt managedkubernetes.HandshakeReceipt, origin managedkubernetes.StreamReceiptOrigin) bool {
	if receipt.Validate() != nil || receipt.Origin != origin || receipt.Protocol != record.expectedProtocol() {
		return false
	}
	if record.nodeRef != "" {
		return receipt.TransportRef == record.nodeRef && receipt.TransportGeneration == record.nodeGeneration && receipt.PeerSPKISHA256 == record.nodeServingKey
	}
	return receipt.TransportRef == record.targetRef && receipt.TransportGeneration == record.targetRevision
}
