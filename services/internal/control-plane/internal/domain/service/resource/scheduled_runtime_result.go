package resource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// waitScheduledRuntimeOwner атомарно принимает закрытый signed runtime result,
// сохраняет его artifact и переводит весь scheduled graph в WAITING_OWNER.
// Маршрут и получатель берутся только из server-owned occurrence/process.
func (service *Service) waitScheduledRuntimeOwner(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
	graph lockedOwnerGraph,
	input CompleteRuntimeExecutionInput,
	primary RuntimeOutput,
	requestHash string,
	now time.Time,
) (RuntimeExecution, error) {
	turn, process := graph.Turn, graph.Process
	turnSpec, turnOK := turn.Spec.(entity.TurnSpec)
	processSpec, processOK := process.Spec.(entity.ProcessRunSpec)
	current, currentErr := currentExecution(processSpec)
	occurrence, run := graph.Occurrence, graph.Run
	if !turnOK || !processOK || currentErr != nil || execution.ScheduleOccurrenceID == "" ||
		occurrence.ID != execution.ScheduleOccurrenceID || processSpec.OccurrenceID != occurrence.ID ||
		processSpec.ScheduleID != occurrence.ScheduleID || process.State != enum.StateRunning ||
		(turn.State != enum.StateClaimed && turn.State != enum.StateRunning) ||
		!executionMatchesTurn(current, turn, turnSpec) ||
		validateScheduledRunBinding(occurrence, run) != nil ||
		!scheduledExecutionMayWaitOwner(occurrence.State, run.State) ||
		occurrence.ExecutionTurnID != turn.ID || occurrence.ExecutionProcessRunID != process.ID ||
		occurrence.RoomID == "" {
		return RuntimeExecution{}, errs.ErrStateConflict
	}
	lease, err := tx.GetTurnLeaseForUpdate(ctx, turn.ID)
	if err != nil || lease.Attempt != execution.Attempt || lease.AuthorityGeneration != execution.GrantGeneration ||
		lease.TokenHash == "" || !lease.ExpiresAt.After(now) {
		return RuntimeExecution{}, errs.ErrStateConflict
	}
	attempt, err := tx.GetTurnAttemptForUpdate(ctx, turn.ID, execution.Attempt)
	if err != nil || attempt.State != "CLAIMED" || attempt.AuthorityGeneration != execution.GrantGeneration ||
		attempt.InputSHA256 != execution.ImmutableInputSHA256 || !attempt.FinishedAt.IsZero() {
		return RuntimeExecution{}, errs.ErrStateConflict
	}

	artifact, err := service.materializeScheduledRuntimeResult(
		ctx, tx, principal, execution, primary, now,
	)
	if err != nil {
		return RuntimeExecution{}, err
	}
	if err := tx.DeleteTurnLease(ctx, turn.ID, lease.Fence); err != nil {
		return RuntimeExecution{}, err
	}
	attempt.State, attempt.Outcome, attempt.FinishedAt = "WAITING_OWNER", "owner_gate_pending", now
	if err := tx.FinishTurnAttempt(ctx, attempt); err != nil {
		return RuntimeExecution{}, err
	}
	turnSpec.ResultArtifactID, turnSpec.ResultArtifactVersion, turnSpec.ResultArtifactSHA256 =
		artifact.ID, artifact.Version, primary.ArtifactSHA256
	turnSpec.Outcome = "owner_gate_pending"
	waitingTurn, err := turn.ReplaceAndTransition(turnSpec, enum.StateWaitingOwner, now)
	if err != nil {
		return RuntimeExecution{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, waitingTurn, turn.Version); err != nil {
		return RuntimeExecution{}, err
	}

	gateID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("control-plane:scheduled-owner-gate:"+execution.ID+":"+primary.ArtifactSHA256)).String()
	deliveryID := uuid.NewSHA1(uuid.NameSpaceURL, []byte("control-plane:scheduled-owner-delivery:"+gateID)).String()
	expiresAt := now.Add(7 * 24 * time.Hour).UTC().Truncate(time.Microsecond)
	deliveryPayloadSHA256, err := canonicalHash(struct {
		Version                                                        int
		DeliveryID, GateID, ProcessID, SessionID, TurnID, ResultSHA256 string
		Attempt                                                        uint32
		ImmutableInput, RecipientActorID                               string
		ExpiresAt                                                      time.Time
		ScheduleID, OccurrenceID, NotificationRoomID                   string
	}{1, deliveryID, gateID, process.ID, execution.SessionID, turn.ID, primary.ArtifactSHA256,
		execution.Attempt, execution.ImmutableInputSHA256, processSpec.RootInitiatorActorID,
		expiresAt, processSpec.ScheduleID, processSpec.OccurrenceID, occurrence.RoomID})
	if err != nil {
		return RuntimeExecution{}, errs.ErrInternal
	}
	gate, err := entity.New(gateID, principal.OrganizationID, principal.ProjectID, process.ID,
		process.OwnerActorID, enum.KindOwnerGate, "Owner gate "+process.ID, entity.OwnerGateSpec{
			ProcessRunID: process.ID, ResultRef: "control-plane-inline:" + artifact.ID,
			ResultSHA256: primary.ArtifactSHA256, ExpiresAt: expiresAt,
			RootInitiatorActorID: processSpec.RootInitiatorActorID, SessionID: execution.SessionID,
			TurnID: turn.ID, Attempt: execution.Attempt, ImmutableInputSHA256: execution.ImmutableInputSHA256,
			RecipientActorID:   processSpec.RootInitiatorActorID,
			DeliveryWorkloadID: service.ownerGateDeliveryWorkload,
			DeliverySPIFFEID:   service.ownerGateDeliverySPIFFEID,
			DeliveryID:         deliveryID, DeliveryPayloadSHA256: deliveryPayloadSHA256,
			ScheduleID: processSpec.ScheduleID, OccurrenceID: occurrence.ID,
			NotificationRoomID: occurrence.RoomID,
		}, now)
	if err != nil {
		return RuntimeExecution{}, errs.ErrStateConflict
	}
	if err := tx.Insert(ctx, gate); err != nil {
		return RuntimeExecution{}, err
	}
	processSpec.ClearContinuation()
	waitingProcess, err := process.ReplaceAndTransition(processSpec, enum.StateWaitingOwner, now)
	if err != nil {
		return RuntimeExecution{}, errs.ErrStateConflict
	}
	if err := tx.Update(ctx, waitingProcess, process.Version); err != nil {
		return RuntimeExecution{}, err
	}
	expectedToken := occurrence.TokenHash
	occurrence.State, occurrence.Outcome = "WAITING_OWNER", "requires_human"
	occurrence.ResultArtifactID = artifact.ID
	occurrence.ClaimantWorkloadID, occurrence.TokenHash = "", ""
	occurrence.AuthorityGeneration = 0
	occurrence.LeaseExpiresAt, occurrence.UpdatedAt = time.Time{}, now
	if err := tx.UpdateScheduleOccurrence(ctx, occurrence, occurrence.Attempt, expectedToken); err != nil {
		return RuntimeExecution{}, err
	}
	if err := tx.WaitScheduledRun(ctx, domainrepo.ScheduledRun{
		OccurrenceID: occurrence.ID, Attempt: occurrence.Attempt,
		Outcome: "requires_human", ResultArtifactID: artifact.ID,
	}); err != nil {
		return RuntimeExecution{}, err
	}
	if err := appendScheduleOccurrenceAudit(ctx, tx, principal, "runtime_requires_owner", occurrence); err != nil {
		return RuntimeExecution{}, err
	}

	expectedVersion, expectedFence := execution.Version, execution.Fence
	execution.Version++
	execution.Fence++
	execution.State, execution.TerminalOutcome = "SUSPENDED", "SUSPENDED"
	execution.TerminalReference, execution.TerminalSHA256 = gate.ID, requestHash
	execution.CodexSessionID, execution.CodexArchiveRelativePath = input.CodexSessionID, input.ArchiveRelativePath
	execution.CodexArchiveSHA256, execution.CodexArchiveProvenance = input.ArchiveSHA256, input.ArchiveProvenance
	execution.LeaseID, execution.LeaseTokenSHA256 = "", ""
	execution.LeaseExpiresAt, execution.UpdatedAt = time.Time{}, now
	if err := pinRuntimeRetention(&execution, now); err != nil {
		return RuntimeExecution{}, err
	}
	if err := tx.UpdateRuntimeExecution(ctx, execution, expectedVersion, expectedFence); err != nil {
		return RuntimeExecution{}, err
	}
	if err := service.revokeExecutionClaims(ctx, tx, principal, process.ID, turn.ID,
		"owner_gate_wait", now); err != nil {
		return RuntimeExecution{}, err
	}
	for _, record := range []struct {
		action string
		value  entity.Resource
	}{{"runtime_requires_owner_gate", gate}, {"runtime_wait_owner_turn", waitingTurn},
		{"runtime_wait_owner_process", waitingProcess}} {
		if err := service.appendMutationRecords(ctx, tx, principal, record.action, record.value); err != nil {
			return RuntimeExecution{}, err
		}
	}
	if err := service.appendLifecycleAudit(ctx, tx, principal, "complete_runtime_requires_owner",
		execution.ID, "RUNTIME_EXECUTION", execution.Version, now); err != nil {
		return RuntimeExecution{}, err
	}
	return execution, nil
}

func (service *Service) materializeScheduledRuntimeResult(
	ctx context.Context,
	tx domainrepo.Transaction,
	principal value.Principal,
	execution RuntimeExecution,
	result RuntimeOutput,
	now time.Time,
) (entity.Resource, error) {
	evidence := sha256.Sum256([]byte(execution.ID + "\x00" + execution.TurnID + "\x00" +
		result.ArtifactSHA256 + "\x00" + execution.ImmutableInputSHA256))
	artifact, err := entity.New(result.ArtifactID, principal.OrganizationID, principal.ProjectID,
		execution.SessionID, principal.ActorID, enum.KindArtifact, result.ArtifactName,
		entity.ArtifactSpec{ArtifactKind: "runtime-result", Direction: "OUTPUT",
			StorageRef: "control-plane-inline:" + result.ArtifactID, SizeBytes: uint64(len(result.ArtifactPayload)),
			MediaType: result.ArtifactMediaType, SHA256: result.ArtifactSHA256, ScanStatus: "CLEAN",
			RetentionPolicyRef: "policy://runtime-result", ScanPolicyRevision: 1,
			ScanEvidenceSHA256: hex.EncodeToString(evidence[:]), ScannerWorkloadID: "agent-runner",
			ScannedAt: now}, now)
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	if err := tx.Insert(ctx, artifact); err != nil {
		return entity.Resource{}, err
	}
	if err := service.appendMutationRecords(ctx, tx, principal, "materialize_runtime_result", artifact); err != nil {
		return entity.Resource{}, err
	}
	return artifact, nil
}
