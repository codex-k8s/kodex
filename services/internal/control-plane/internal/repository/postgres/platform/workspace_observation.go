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

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var (
	// queryWorkspaceObservationLease locks only mutable lease/storage rows, never immutable revisions.
	// queryWorkspaceObservationLease блокирует только mutable lease/storage, не immutable revisions.
	//go:embed sql/workspace_observation__lease.sql
	queryWorkspaceObservationLease string
	// queryWorkspaceObservationBind records the exact current writer through version/generation CAS.
	// queryWorkspaceObservationBind сохраняет exact current writer через version/generation CAS.
	//go:embed sql/workspace_observation__bind.sql
	queryWorkspaceObservationBind string
	// queryWorkspaceObservationQuiescent records only the matching already-known stopped writer.
	// queryWorkspaceObservationQuiescent сохраняет только matching already-known stopped writer.
	//go:embed sql/workspace_observation__quiescent.sql
	queryWorkspaceObservationQuiescent string
	// queryWorkspaceObservationBlock preserves unknown work without changing its run or authority.
	// queryWorkspaceObservationBlock сохраняет unknown work без изменения run или authority.
	//go:embed sql/workspace_observation__block.sql
	queryWorkspaceObservationBlock string
	// queryWorkspaceObservationReleased gates checkpoint work on acknowledged physical Pod disappearance.
	// queryWorkspaceObservationReleased допускает checkpoint work после подтверждённого исчезновения Pod.
	//go:embed sql/workspace_observation__released.sql
	queryWorkspaceObservationReleased string
	// queryWorkspaceObservationReceipt retains semantic stopped-writer replay beyond generic request TTL.
	// queryWorkspaceObservationReceipt сохраняет semantic stopped-writer replay дольше generic request TTL.
	//go:embed sql/workspace_observation__receipt.sql
	queryWorkspaceObservationReceipt string
)

// workspaceObservationLease separates immutable execution identity from mutable storage lifecycle.
// No field in this shape is a grant; BIND and past-fact authorization remain distinct below.
// workspaceObservationLease отделяет immutable execution identity от mutable storage lifecycle.
// Поля не являются grant; BIND и past-fact authorization ниже проверяются отдельно.
type workspaceObservationLease struct {
	storageID, storageRef, storageState, writerLeaseID, ackRaw, joinRaw string
	blockReason                                                         string
	writerReleased                                                      bool
	storageGeneration, storageVersion                                   int64
	leaseID, fenceDigest, leaseState, projectID                         string
	leaseGeneration                                                     int64
	expiresAt                                                           time.Time
	active                                                              bool
	input                                                               runtimecontract.RunnerInput
}

// runtimeWorkspaceSnapshotBinding preserves absence and strictly parses the owner-selected binding.
// A present null or unknown field is corruption, never a request to fall back to the legacy profile.
// runtimeWorkspaceSnapshotBinding сохраняет отсутствие и строго разбирает owner-selected binding.
// Present null или unknown field — corruption, не запрос fallback к legacy profile.
func runtimeWorkspaceSnapshotBinding(snapshot map[string]any) (*runtimecontract.WorkspaceStorageBinding, error) {
	value, present := snapshot["workspaceStorage"]
	if !present {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil || bytes.Equal(raw, []byte("null")) || len(raw) > runtimecontract.MaximumWorkspaceManifestBytes {
		return nil, errs.ErrConflict
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var binding runtimecontract.WorkspaceStorageBinding
	if decoder.Decode(&binding) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errs.ErrConflict
	}
	if _, err := binding.GenerationSubPath(); err != nil {
		return nil, errs.ErrConflict
	}
	return &binding, nil
}

// readWorkspaceObservationLease resolves scope only through the frozen owner revision and current rows.
// Expired/cancelled leases remain readable solely for exact past-fact observation, never new writes.
// readWorkspaceObservationLease выводит scope только из frozen owner revision и current rows.
// Expired/cancelled lease читается лишь для exact past-fact observation, не новых записей.
func readWorkspaceObservationLease(ctx context.Context, tx pgx.Tx, current scope, payload command.WorkspaceObservationInput) (workspaceObservationLease, error) {
	var result workspaceObservationLease
	var snapshotRaw []byte
	input := &result.input
	err := tx.QueryRow(ctx, queryWorkspaceObservationLease, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "lease_ref": payload.LeaseRef,
	}).Scan(&result.storageID, &result.storageRef, &result.storageGeneration, &result.storageState, &result.storageVersion,
		&result.writerLeaseID, &result.ackRaw, &result.joinRaw, &result.blockReason, &result.writerReleased, &result.leaseID, &result.fenceDigest, &result.leaseGeneration,
		&result.leaseState, &result.expiresAt, &input.RuntimeRevisionRef, &input.RuntimeRevisionVersion, &input.RuntimeRevisionDigest,
		&input.Attempt, &snapshotRaw, &input.OrganizationRef, &input.ProjectRef, &input.SessionRef, &input.AgentRef,
		&input.RunRef, &input.NodeRef, &input.TurnRef, &input.InputDigest, &result.projectID, &result.active)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var snapshot map[string]any
	if json.Unmarshal(snapshotRaw, &snapshot) != nil {
		return result, errs.ErrConflict
	}
	input.Mode, input.AssistantScope = runtimecontract.RunnerModeTurn, runtimecontract.AssistantScopeNone
	input.LeaseRef, input.LeaseFence, input.LeaseGeneration = payload.LeaseRef, payload.Fence, payload.Generation
	input.ExecutionBindingDigest = payload.ExecutionBindingDigest
	input.RoleRuntimeContractRevision = uint64(runtimeRevisionMapInt64(snapshot, "roleRuntimeContractRevision"))
	input.WorkspaceStorage, err = runtimeWorkspaceSnapshotBinding(snapshot)
	if err != nil || input.WorkspaceStorage == nil {
		return result, errs.ErrConflict
	}
	policyRaw, err := json.Marshal(snapshot["workspacePolicy"])
	var policy entity.RuntimeWorkspacePolicy
	if err != nil || json.Unmarshal(policyRaw, &policy) != nil {
		return result, errs.ErrConflict
	}
	input.WorkspacePolicy = runtimeWorkspacePolicyContract(policy)
	if input.WorkspacePolicy.Validate() != nil || input.WorkspaceStorage.ValidateForInput(*input) != nil {
		return result, errs.ErrConflict
	}
	fence := sha256.Sum256([]byte(payload.Fence))
	if payload.Generation < 1 || result.leaseGeneration != payload.Generation || result.fenceDigest != hex.EncodeToString(fence[:]) ||
		input.RuntimeRevisionDigest != payload.RuntimeRevisionDigest || input.WorkspaceStorage.WorkspaceRef != payload.WorkspaceRef ||
		input.WorkspaceStorage.Generation != payload.WorkspaceGeneration ||
		(payload.Phase == runtimecontract.WorkspaceObserveBind || payload.Phase == runtimecontract.WorkspaceObserveQuiescent) &&
			(uuid.Validate(payload.PodUID) != nil || uuid.Validate(payload.PVCUID) != nil ||
				input.WorkspaceStorage.VolumeReady != nil && payload.PVCUID != input.WorkspaceStorage.VolumeReady.Preparation.PVCUID) {
		return result, errs.ErrForbidden
	}
	return result, nil
}

// authorizeWorkspaceObservationTx fences BIND before even an idempotent receipt may be replayed.
// QUIESCENT replay may return a historical ACK after terminal, but cannot create a new writer.
// authorizeWorkspaceObservationTx ограничивает BIND до replay idempotent receipt.
// QUIESCENT replay может вернуть historical ACK после terminal, но не создать нового writer.
func (repository *Repository) authorizeWorkspaceObservationTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, valid := input.Payload.(command.WorkspaceObservationInput)
	if !valid || payload.Phase != runtimecontract.WorkspaceObserveBind && payload.Phase != runtimecontract.WorkspaceObserveQuiescent && payload.Phase != runtimecontract.WorkspaceObserveBlocked && payload.Phase != runtimecontract.WorkspaceObserveReleased {
		return errs.ErrInvalid
	}
	locked, err := readWorkspaceObservationLease(ctx, tx, current, payload)
	if err != nil {
		return err
	}
	if payload.Phase == runtimecontract.WorkspaceObserveReleased {
		if len(payload.Quiescence) != 0 || payload.PodUID != "" || payload.PVCUID != "" || payload.BlockReason != "" {
			return errs.ErrInvalid
		}
		return nil
	}
	if payload.Phase == runtimecontract.WorkspaceObserveBlocked {
		ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement([]byte(locked.ackRaw))
		if err != nil || !ack.Matches(locked.input) || !runtimecontract.ValidWorkspaceBlockReason(payload.BlockReason) || len(payload.Quiescence) != 0 ||
			payload.PodUID != "" || payload.PVCUID != "" || locked.writerLeaseID != locked.leaseID || locked.storageGeneration != payload.WorkspaceGeneration {
			return errs.ErrForbidden
		}
		return nil
	}
	if payload.BlockReason != "" {
		return errs.ErrInvalid
	}
	observation := runtimecontract.RunnerWorkspaceObservation{RuntimeRevisionDigest: payload.RuntimeRevisionDigest,
		Phase: payload.Phase, WorkspaceRef: payload.WorkspaceRef, Generation: payload.WorkspaceGeneration}
	if payload.Phase == runtimecontract.WorkspaceObserveQuiescent {
		joined, err := runtimecontract.DecodeWorkspaceQuiescence(payload.Quiescence)
		if err != nil || !joined.Matches(locked.input, *locked.input.WorkspaceStorage, payload.PodUID, payload.PVCUID) {
			return errs.ErrInvalid
		}
		observation.Phase = runtimecontract.WorkspaceObserveQuiescent
		observation.Quiescence = &joined
	} else if len(payload.Quiescence) != 0 || repository.roleImages.WorkspaceStorageProfile != runtimecontract.WorkspaceStorageProfile ||
		!locked.active || locked.leaseState != "CLAIMED" || !locked.expiresAt.After(time.Now()) ||
		locked.storageRef != payload.WorkspaceRef || locked.storageGeneration != payload.WorkspaceGeneration ||
		locked.writerLeaseID != locked.leaseID || locked.storageState != "WRITER_PENDING" && locked.storageState != "WRITING" {
		return errs.ErrForbidden
	}
	if observation.ValidateForInput(locked.input) != nil {
		return errs.ErrInvalid
	}
	return nil
}

// observeWorkspace persists writer metadata under the owner command's audit/idempotency transaction.
// Terminal observations only append matching past quiescence; runs, leases, grants and RR never change.
// observeWorkspace сохраняет writer metadata в owner command audit/idempotency transaction.
// Terminal observation добавляет matching past quiescence; runs, leases, grants и RR не меняются.
func (repository *Repository) observeWorkspace(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, valid := input.Payload.(command.WorkspaceObservationInput)
	if !valid {
		return commandOutcome{}, errs.ErrInvalid
	}
	locked, err := readWorkspaceObservationLease(ctx, tx, current, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	if locked.storageRef != payload.WorkspaceRef || locked.storageGeneration != payload.WorkspaceGeneration || locked.writerLeaseID != locked.leaseID {
		return commandOutcome{}, errs.ErrConflict
	}
	if payload.Phase == runtimecontract.WorkspaceObserveReleased {
		return repository.observeWorkspaceReleased(ctx, tx, current, locked)
	}
	if payload.Phase == runtimecontract.WorkspaceObserveBlocked {
		return repository.observeWorkspaceBlocked(ctx, tx, current, locked, payload)
	}
	writer := runtimecontract.WorkspaceWriterCommitment{RunRef: locked.input.RunRef, NodeRef: locked.input.NodeRef, TurnRef: locked.input.TurnRef,
		RuntimeRevisionRef: locked.input.RuntimeRevisionRef, RuntimeRevisionVersion: locked.input.RuntimeRevisionVersion,
		RuntimeRevisionDigest: locked.input.RuntimeRevisionDigest, ExecutionBindingDigest: payload.ExecutionBindingDigest,
		InputDigest: locked.input.InputDigest, LeaseRef: payload.LeaseRef, LeaseGeneration: payload.Generation, Attempt: locked.input.Attempt,
		PodUID: payload.PodUID, PVCUID: payload.PVCUID}
	ack := runtimecontract.WorkspaceWriterAcknowledgement{Schema: runtimecontract.WorkspaceWriterAcknowledgementSchema,
		Scope: locked.input.WorkspaceStorage.Scope, WorkspaceRef: payload.WorkspaceRef, Generation: payload.WorkspaceGeneration, Writer: writer}
	raw, err := runtimecontract.EncodeWorkspaceWriterAcknowledgement(ack)
	if err != nil || !ack.Matches(locked.input) {
		return commandOutcome{}, errs.ErrInvalid
	}
	arguments := pgx.StrictNamedArgs{"organization_id": current.organizationID, "workspace_id": locked.storageID,
		"workspace_generation": locked.storageGeneration, "version": locked.storageVersion, "lease_id": locked.leaseID, "ack": string(raw)}
	var version int64
	switch payload.Phase {
	case runtimecontract.WorkspaceObserveBind:
		if locked.ackRaw != "" {
			if locked.ackRaw != string(raw) || locked.storageState != "WRITING" {
				return commandOutcome{}, errs.ErrConflict
			}
		} else if err := tx.QueryRow(ctx, queryWorkspaceObservationBind, arguments).Scan(&version); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		locked.storageState = "WRITING"
	case runtimecontract.WorkspaceObserveQuiescent:
		if locked.ackRaw != string(raw) {
			return commandOutcome{}, errs.ErrForbidden
		}
		if locked.joinRaw != "" {
			if locked.joinRaw != string(payload.Quiescence) {
				return commandOutcome{}, errs.ErrIdempotencyReuse
			}
		} else {
			arguments["record"] = string(payload.Quiescence)
			if err := tx.QueryRow(ctx, queryWorkspaceObservationQuiescent, arguments).Scan(&version); err != nil {
				return commandOutcome{}, errs.ErrConflict
			}
			locked.storageState, locked.blockReason, locked.joinRaw = "CHECKPOINT_PENDING", "", string(payload.Quiescence)
		}
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"workspaceWriterAck": string(raw), "workspaceCommittedPhase": payload.Phase, "workspaceCommittedQuiescence": string(payload.Quiescence), "workspaceStorageState": locked.storageState,
		"workspaceQuiescence": locked.joinRaw, "workspaceBlockReason": locked.blockReason, "workspaceCurrentTuple": true, "workspaceWriterReleased": locked.writerReleased}},
		projectID: locked.projectID, projectRef: locked.input.ProjectRef, resourceKind: "RUN", resourceRef: locked.input.RunRef,
		summary: "i18n:SESSION_WORKSPACE_OBSERVED"}, nil
}

// observeWorkspaceBlocked records the first closed cause without overwriting a positive saved join.
// Missing-after-ACK readback returns that exact stored past fact; neither branch grants a new writer.
// observeWorkspaceBlocked сохраняет первую closed cause, не заменяя positive saved join.
// Missing-after-ACK readback возвращает exact stored past fact; новый writer не разрешается.
func (repository *Repository) observeWorkspaceBlocked(ctx context.Context, tx pgx.Tx, current scope, locked workspaceObservationLease, payload command.WorkspaceObservationInput) (commandOutcome, error) {
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement([]byte(locked.ackRaw))
	if err != nil || !ack.Matches(locked.input) || !runtimecontract.ValidWorkspaceBlockReason(payload.BlockReason) {
		return commandOutcome{}, errs.ErrForbidden
	}
	if locked.joinRaw == "" {
		if locked.storageState != "BLOCKED" {
			err = tx.QueryRow(ctx, queryWorkspaceObservationBlock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "workspace_id": locked.storageID,
				"workspace_generation": locked.storageGeneration, "version": locked.storageVersion, "lease_id": locked.leaseID, "ack": locked.ackRaw, "reason": payload.BlockReason}).Scan(&locked.blockReason)
			if err != nil {
				return commandOutcome{}, errs.ErrConflict
			}
			locked.storageState = "BLOCKED"
		}
	} else {
		record, err := runtimecontract.DecodeWorkspaceQuiescence([]byte(locked.joinRaw))
		if err != nil || !record.Matches(locked.input, *locked.input.WorkspaceStorage, ack.Writer.PodUID, ack.Writer.PVCUID) {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"workspaceWriterAck": locked.ackRaw, "workspaceCommittedPhase": runtimecontract.WorkspaceObserveBlocked, "workspaceCommittedQuiescence": "", "workspaceStorageState": locked.storageState,
		"workspaceQuiescence": locked.joinRaw, "workspaceBlockReason": locked.blockReason, "workspaceCurrentTuple": true, "workspaceWriterReleased": locked.writerReleased}}, projectID: locked.projectID, projectRef: locked.input.ProjectRef,
		resourceKind: "RUN", resourceRef: locked.input.RunRef, summary: "i18n:SESSION_WORKSPACE_OBSERVED"}, nil
}

// workspaceObservationReceiptMetadata retains only the immutable owner ACK, not a cached current phase.
// The response's current tuple fields are a separate locked readback on both first delivery and replay.
// workspaceObservationReceiptMetadata сохраняет лишь immutable owner ACK, не cached current phase.
// Current tuple fields ответа — отдельный locked readback при первой выдаче и replay.
func workspaceObservationReceiptMetadata(kind command.Kind, result command.Result) command.Result {
	if kind != command.ObserveWorkspace {
		return result
	}
	result.Runtime = map[string]any{"workspaceWriterAck": result.Runtime["workspaceWriterAck"], "workspaceCommittedPhase": result.Runtime["workspaceCommittedPhase"], "workspaceCommittedQuiescence": result.Runtime["workspaceCommittedQuiescence"]}
	return result
}

// refreshWorkspaceObservationReceipt reads the exact current writer without rewriting a stored receipt.
// Historical generation replay returns only its ACK; it cannot authorize current cleanup or another writer.
// refreshWorkspaceObservationReceipt читает exact current writer без изменения stored receipt.
// Replay historical generation возвращает лишь ACK, не право current cleanup или нового writer.
func (repository *Repository) refreshWorkspaceObservationReceipt(ctx context.Context, tx pgx.Tx, current scope, input command.Command, result *command.Result) error {
	if input.Kind != command.ObserveWorkspace {
		return nil
	}
	payload, ok := input.Payload.(command.WorkspaceObservationInput)
	if !ok || result == nil {
		return errs.ErrInvalid
	}
	locked, err := readWorkspaceObservationLease(ctx, tx, current, payload)
	if err != nil {
		return err
	}
	*result, err = workspaceObservationCurrentReadback(locked, payload, *result)
	return err
}

// workspaceObservationCurrentReadback projects locked metadata into a new map, leaving receipt bytes immutable.
// Old generations keep their committed outcome without borrowing another generation's current phase.
// workspaceObservationCurrentReadback выводит locked metadata в новую map, не меняя receipt bytes.
// Old generation сохраняет committed outcome, не заимствуя current phase нового поколения.
func workspaceObservationCurrentReadback(locked workspaceObservationLease, payload command.WorkspaceObservationInput, result command.Result) (command.Result, error) {
	stored, ok := result.Runtime["workspaceWriterAck"].(string)
	if !ok {
		return command.Result{}, errs.ErrConflict
	}
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement([]byte(stored))
	if err != nil {
		return command.Result{}, errs.ErrConflict
	}
	result.Runtime = map[string]any{"workspaceWriterAck": stored,
		"workspaceCommittedPhase":      result.Runtime["workspaceCommittedPhase"],
		"workspaceCommittedQuiescence": result.Runtime["workspaceCommittedQuiescence"],
		"workspaceCurrentTuple":        false}
	if locked.storageGeneration != payload.WorkspaceGeneration || locked.writerLeaseID != locked.leaseID || locked.ackRaw != stored {
		return result, nil
	}
	if !ack.Matches(locked.input) || !runtimecontract.ValidWorkspaceStorageState(locked.storageState) {
		return command.Result{}, errs.ErrConflict
	}
	if locked.joinRaw != "" {
		joined, err := runtimecontract.DecodeWorkspaceQuiescence([]byte(locked.joinRaw))
		if err != nil || joined.Writer != ack.Writer || !joined.Matches(locked.input, *locked.input.WorkspaceStorage, ack.Writer.PodUID, ack.Writer.PVCUID) {
			return command.Result{}, errs.ErrConflict
		}
	}
	result.Runtime["workspaceCurrentTuple"] = true
	result.Runtime["workspaceStorageState"] = locked.storageState
	result.Runtime["workspaceQuiescence"] = locked.joinRaw
	result.Runtime["workspaceBlockReason"] = locked.blockReason
	result.Runtime["workspaceWriterReleased"] = locked.writerReleased
	return result, nil
}

// observeWorkspaceReleased commits absence only for the already-known same positively joined writer.
// Its immutable receipt is replayable after later generations, without opening their current authority.
// observeWorkspaceReleased фиксирует отсутствие лишь для уже известного same positively joined writer.
// Immutable receipt доступна после новых generations без открытия их current authority.
func (repository *Repository) observeWorkspaceReleased(ctx context.Context, tx pgx.Tx, current scope, locked workspaceObservationLease) (commandOutcome, error) {
	ack, err := runtimecontract.DecodeWorkspaceWriterAcknowledgement([]byte(locked.ackRaw))
	joined, joinErr := runtimecontract.DecodeWorkspaceQuiescence([]byte(locked.joinRaw))
	if err != nil || joinErr != nil || !ack.Matches(locked.input) || joined.Writer != ack.Writer ||
		!joined.Matches(locked.input, *locked.input.WorkspaceStorage, ack.Writer.PodUID, ack.Writer.PVCUID) {
		return commandOutcome{}, errs.ErrForbidden
	}
	if !locked.writerReleased {
		var version int64
		if err := tx.QueryRow(ctx, queryWorkspaceObservationReleased, pgx.StrictNamedArgs{"organization_id": current.organizationID, "workspace_id": locked.storageID,
			"workspace_generation": locked.storageGeneration, "version": locked.storageVersion, "lease_id": locked.leaseID, "ack": locked.ackRaw, "record": locked.joinRaw}).Scan(&version); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		locked.writerReleased = true
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"workspaceWriterAck": locked.ackRaw, "workspaceCommittedPhase": runtimecontract.WorkspaceObserveReleased,
		"workspaceCommittedQuiescence": locked.joinRaw, "workspaceCurrentTuple": true, "workspaceStorageState": locked.storageState, "workspaceQuiescence": locked.joinRaw,
		"workspaceWriterReleased": true, "workspaceBlockReason": locked.blockReason}}, projectID: locked.projectID, projectRef: locked.input.ProjectRef,
		resourceKind: "RUN", resourceRef: locked.input.RunRef, summary: "i18n:SESSION_WORKSPACE_OBSERVED"}, nil
}
