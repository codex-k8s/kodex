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
)

// workspaceObservationLease separates immutable execution identity from mutable storage lifecycle.
// No field in this shape is a grant; BIND and past-fact authorization remain distinct below.
// workspaceObservationLease отделяет immutable execution identity от mutable storage lifecycle.
// Поля не являются grant; BIND и past-fact authorization ниже проверяются отдельно.
type workspaceObservationLease struct {
	storageID, storageRef, storageState, writerLeaseID, ackRaw, joinRaw string
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
		&result.writerLeaseID, &result.ackRaw, &result.joinRaw, &result.leaseID, &result.fenceDigest, &result.leaseGeneration,
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
	input.WorkspaceStorage, err = runtimeWorkspaceSnapshotBinding(snapshot)
	if err != nil || input.WorkspaceStorage == nil {
		return result, errs.ErrConflict
	}
	policyRaw, err := json.Marshal(snapshot["workspacePolicy"])
	var policy entity.RuntimeWorkspacePolicy
	if err != nil || json.Unmarshal(policyRaw, &policy) != nil {
		return result, errs.ErrConflict
	}
	input.WorkspacePolicy = runtimecontract.RuntimeWorkspacePolicy{Revision: policy.Revision, Root: policy.Root,
		Digest: policy.Digest, MaximumWritableBytes: policy.MaximumWritableBytes, MaximumFileCount: policy.MaximumFileCount,
		DenialReasons: append([]string(nil), policy.DenialReasons...)}
	for _, rule := range policy.Rules {
		input.WorkspacePolicy.Rules = append(input.WorkspacePolicy.Rules, runtimecontract.RuntimeWorkspacePathRule{Path: rule.Path, Access: rule.Access})
	}
	if input.WorkspacePolicy.Validate() != nil || input.WorkspaceStorage.ValidateForInput(*input) != nil {
		return result, errs.ErrConflict
	}
	fence := sha256.Sum256([]byte(payload.Fence))
	if payload.Generation < 1 || result.leaseGeneration != payload.Generation || result.fenceDigest != hex.EncodeToString(fence[:]) ||
		input.RuntimeRevisionDigest != payload.RuntimeRevisionDigest || input.WorkspaceStorage.WorkspaceRef != payload.WorkspaceRef ||
		input.WorkspaceStorage.Generation != payload.WorkspaceGeneration || uuid.Validate(payload.PodUID) != nil || uuid.Validate(payload.PVCUID) != nil {
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
	if !valid || payload.Phase != runtimecontract.WorkspaceObserveBind && payload.Phase != runtimecontract.WorkspaceObserveQuiescent {
		return errs.ErrInvalid
	}
	locked, err := readWorkspaceObservationLease(ctx, tx, current, payload)
	if err != nil {
		return err
	}
	observation := runtimecontract.RunnerWorkspaceObservation{RuntimeRevisionDigest: payload.RuntimeRevisionDigest,
		Phase: payload.Phase, WorkspaceRef: payload.WorkspaceRef, Generation: payload.WorkspaceGeneration}
	if payload.Phase == runtimecontract.WorkspaceObserveQuiescent {
		joined, err := runtimecontract.DecodeWorkspaceQuiescence(payload.Quiescence)
		if err != nil || !joined.Matches(locked.input, *locked.input.WorkspaceStorage, payload.PodUID, payload.PVCUID) {
			return errs.ErrInvalid
		}
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
		}
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"workspaceWriterAck": string(raw)}},
		projectID: locked.projectID, projectRef: locked.input.ProjectRef, resourceKind: "RUN", resourceRef: locked.input.RunRef,
		summary: "i18n:SESSION_WORKSPACE_OBSERVED"}, nil
}
