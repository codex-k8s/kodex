package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesGateConsumerLock takes the Gate lock after its original source and invocation.
// queryKubernetesGateConsumerLock блокирует Gate после original source и invocation.
//
//go:embed sql/kubernetes_child_gate__lock.sql
var queryKubernetesGateConsumerLock string

// queryKubernetesGateConsumerLiveClaim reads persisted original lease pins without exposing its fence.
// queryKubernetesGateConsumerLiveClaim читает persisted original lease pins без раскрытия fence.
//
//go:embed sql/kubernetes_child_gate__live_claim.sql
var queryKubernetesGateConsumerLiveClaim string

// queryKubernetesGateConsumerTerminal closes the current invocation and invokes its canonical dependency trigger.
// queryKubernetesGateConsumerTerminal закрывает current invocation через canonical dependency trigger.
//
//go:embed sql/kubernetes_child_gate__terminal_invocation.sql
var queryKubernetesGateConsumerTerminal string

// queryKubernetesGateConsumerResolve binds the final decision write to the still-live original claim.
// queryKubernetesGateConsumerResolve связывает последнюю decision write с ещё live original claim.
//
//go:embed sql/kubernetes_child_gate__resolve.sql
var queryKubernetesGateConsumerResolve string

// resolveKubernetesChildGate handles only server-linked child Gates before the generic parent writer.
// Approval preserves the original running claim. Rejection/cancel closes its dependency graph and uses
// the exact chosen Gate for ordinary continuation; no arbitrary sibling or caller binding is adopted.
// resolveKubernetesChildGate обрабатывает server-linked child Gate до generic parent writer.
// Approval сохраняет original running claim. Rejection/cancel закрывает dependency graph и связывает
// ordinary continuation с exact выбранным Gate, без случайного sibling либо caller binding.
func (repository *Repository) resolveKubernetesChildGate(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, bool, error) {
	var empty commandOutcome
	payload, ok := input.Payload.(command.GateResolutionInput)
	if !ok {
		return empty, false, errs.ErrInvalid
	}
	invocationRef, handled, err := kubernetesChildGateOrigin(ctx, tx, current.organizationID, payload.GateRef)
	if err != nil || !handled {
		return empty, handled, err
	}
	if payload.IntegrationApproval != nil || input.Mutation.ExpectedVersion == nil || !contains([]string{"APPROVE", "REJECT", "CANCEL"}, payload.Decision) {
		return empty, true, errs.ErrInvalid
	}
	owner, leaseRef, fenceDigest, generation, err := repository.readKubernetesChildGateOwnerTx(ctx, tx, current, invocationRef)
	defer clear(owner.normalized)
	defer clear(owner.input.Body)
	defer clear(owner.input.Options)
	defer clear(owner.candidate.boundedInput)
	if err != nil {
		return empty, true, err
	}
	var lockedID string
	if err := tx.QueryRow(ctx, queryKubernetesGateConsumerLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "gate_ref": payload.GateRef}).Scan(&lockedID); err != nil {
		return empty, true, errs.ErrUnavailable
	}
	record, err := readKubernetesChildGateRecord(ctx, tx, current.organizationID, payload.GateRef)
	if err != nil {
		return empty, true, err
	}
	if record.gateID != lockedID || record.invocationID != owner.candidate.id || record.invocationRef != invocationRef || record.state != "OPEN" {
		return empty, true, errs.ErrAlreadyResolved
	}
	if record.version != *input.Mutation.ExpectedVersion {
		return empty, true, errs.ErrVersionMismatch
	}
	if !contains(record.allowed, payload.Decision) {
		return empty, true, errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: payload.GateRef}); err != nil {
		return empty, true, err
	}
	visible, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId, current.organizationID, payload.GateRef, current.role, current.actorID), true)
	if err != nil {
		return empty, true, err
	}
	if !contains(visible.NextActions, "RESOLVE_GATE") {
		return empty, true, errs.ErrForbidden
	}
	if err := repository.projectOwnerGateScope(ctx, tx, current, &visible, true); err != nil {
		return empty, true, err
	}
	snapshot, err := kubernetesOperationSnapshotDigestFromClaim(owner, fenceDigest, generation)
	if err != nil || snapshot != record.snapshotDigest || !record.deadline.After(record.now) || !owner.lease.ExpiresAt.After(record.now) {
		return empty, true, errs.ErrForbidden
	}
	var childCurrent bool
	if err := tx.QueryRow(ctx, queryKubernetesChildCurrent, pgx.StrictNamedArgs{"child_id": record.childID, "invocation_id": record.invocationID}).Scan(&childCurrent); err != nil {
		return empty, true, errs.ErrUnavailable
	}
	if !childCurrent {
		return empty, true, errs.ErrForbidden
	}
	rootState, lockErr := lockOwnerGateRootTx(ctx, tx, record.rootID)
	if lockErr != nil {
		return empty, true, lockErr
	}
	if rootState != "RUNNING" && rootState != "WAITING_HUMAN" {
		return empty, true, errs.ErrForbidden
	}
	attachment, err := repository.resolveFinalizedAttachmentSet(ctx, tx, current, record.projectID, payload.AttachmentSetRef, "OWNER_GATE_MESSAGE", false)
	if err != nil {
		return empty, true, err
	}
	next, nodeNext, safeError := "APPROVED", "SUCCEEDED", ""
	switch payload.Decision {
	case "REJECT":
		next, nodeNext, safeError = "REJECTED", "FAILED", "INTEGRATION_REJECTED_BY_OWNER"
	case "CANCEL":
		next, nodeNext, safeError = "CANCELLED", "CANCELLED", "INTEGRATION_CANCELLED_BY_OWNER"
	}
	// Selected Gate closes first: the invocation trigger may cancel other still-open child Gates.
	// Выбранный Gate закрывается первым: invocation trigger может отменить остальные OPEN child Gates.
	resolved, err := tx.Exec(ctx, queryKubernetesGateConsumerResolve, pgx.StrictNamedArgs{"organization_id": current.organizationID, "gate_id": record.gateID, "expected_version": record.version, "state": next, "decision": payload.Decision, "comment": truncate(payload.Comment, 2000), "actor_id": current.actorID, "lease_ref": leaseRef, "generation": generation, "fence_digest": fenceDigest})
	if err != nil {
		return empty, true, errs.ErrUnavailable
	}
	if resolved.RowsAffected() != 1 {
		return empty, true, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion, record.nodeID, nodeNext); err != nil {
		return empty, true, errs.ErrUnavailable
	}
	if payload.Decision != "APPROVE" {
		tag, err := tx.Exec(ctx, queryKubernetesGateConsumerTerminal, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": record.invocationID, "state": next, "safe_error_code": safeError, "lease_ref": leaseRef, "generation": generation, "fence_digest": fenceDigest})
		if err != nil {
			return empty, true, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return empty, true, errs.ErrConflict
		}
		if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, record.invocationID, record.projectID, payload.GateRef); err != nil {
			return empty, true, err
		}
	}
	if attachment.ID != "" {
		tag, err := tx.Exec(ctx, queryAttachmentSetsBindGateResolution, pgx.StrictNamedArgs{"attachment_set_id": attachment.ID, "gate_id": record.gateID})
		if err != nil || tag.RowsAffected() != 1 {
			return empty, true, errs.ErrConflict
		}
		if err := repository.bindAttachmentSet(ctx, tx, current, record.projectID, attachment, "OWNER_GATE_MESSAGE", record.gateID); err != nil {
			return empty, true, err
		}
	}
	if _, err := tx.Exec(ctx, queryInteractionCancelPendingGateDeliveries, pgx.StrictNamedArgs{"gate_id": record.gateID}); err != nil {
		return empty, true, errs.ErrUnavailable
	}
	runNext := "RUNNING"
	if payload.Decision == "APPROVE" {
		var active int
		if err := tx.QueryRow(ctx, queryCommandsResolvegateSelectActiveAgentNodes, record.rootID).Scan(&active); err != nil {
			return empty, true, errs.ErrUnavailable
		}
		if active == 0 {
			// Match the parent Gate lifecycle: retain the waiting root until the approved external result settles.
			// Сохраняем parent Gate lifecycle: root ждёт до завершения согласованного внешнего результата.
			runNext = "WAITING_HUMAN"
		}
	}
	var open int64
	if tx.QueryRow(ctx, queryCommandsResolvegateCountOpenGates, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": record.rootID}).Scan(&open) != nil {
		return empty, true, errs.ErrUnavailable
	}
	if open > 0 {
		runNext = "WAITING_HUMAN"
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunsStateVersionUpdatedAt, record.rootID, runNext); err != nil {
		return empty, true, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, current, record.projectID, record.rootID, payload.GateRef, "OWNER_GATE_RESOLVED", record.nodeRef, "", payload.GateRef, "", "i18n:OWNER_GATE_RESOLVED", runNext, nodeNext)
	if err != nil {
		return empty, true, err
	}
	runRef, err := mustRunRef(ctx, tx, record.rootID)
	if err != nil {
		return empty, true, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, current, runRef)
	if err != nil {
		return empty, true, err
	}
	gate, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId, current.organizationID, payload.GateRef, current.role, current.actorID), true)
	if err != nil {
		return empty, true, err
	}
	if err := repository.projectGateIntent(ctx, tx, current, &gate, true, kubernetesGatePreviewSummary); err != nil {
		return empty, true, err
	}
	return commandOutcome{result: command.Result{Gate: &gate, Run: &run, Graph: &graph, Event: &event}, projectID: record.projectID, projectRef: record.projectRef, resourceKind: "OWNER_GATE", resourceRef: payload.GateRef, summary: "i18n:OWNER_GATE_RESOLVED"}, true, nil
}

// readKubernetesChildGateOwnerTx reuses original-actor source and canonical running-invocation eligibility.
// Only the database supplies the current fence digest/generation; no raw fence or dispatch binding exists here.
// readKubernetesChildGateOwnerTx повторяет original-actor source и canonical running-invocation eligibility.
// Только database задаёт current fence digest/generation; raw fence/dispatch binding здесь не создаётся.
func (repository *Repository) readKubernetesChildGateOwnerTx(ctx context.Context, tx pgx.Tx, current scope, invocationRef string) (kubernetesOperationOwner, string, string, int64, error) {
	owner := kubernetesOperationOwner{current: current}
	if _, err := scanKubernetesOperator(tx.QueryRow(ctx, queryKubernetesSetupSourceShare)); err != nil {
		return owner, "", "", 0, err
	}
	_, selected, err := repository.currentKubernetesInvocationSourceTx(ctx, tx, current.organizationID, invocationRef)
	if err != nil {
		return owner, "", "", 0, err
	}
	owner.selected = selected
	err = scanIntegrationInvocationCandidate(tx.QueryRow(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState, current.organizationID, 1, "integration-gateway", "MANAGED_MCP", false, invocationRef, true, false, false), &owner.candidate)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, "", "", 0, errs.ErrForbidden
	}
	if err != nil {
		return owner, "", "", 0, errs.ErrUnavailable
	}
	actor := current
	actor.actorRef = owner.candidate.initiatorRef
	actor.actorID = ""
	accessUntil, err := repository.requireAccessUntil(ctx, tx, actor, "integration.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: owner.candidate.connectionRef})
	if err != nil {
		return owner, "", "", 0, err
	}
	owner.accessUntil = accessUntil
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, owner.candidate.connectionRef, owner.candidate.definitionKey, owner.candidate.definitionVersion, owner.candidate.definitionDigest)
	if err != nil {
		return owner, "", "", 0, err
	}
	capability, known := definition.Capability(owner.candidate.capabilityKey)
	if !known || owner.candidate.definitionKey != integrationpackage.KubernetesDefinitionKey || owner.candidate.resourceKind != "KUBERNETES_TARGET" || capability.Operation != owner.candidate.operation || capability.Risk != owner.candidate.risk || definition.ValidateInvocationApprovalPolicy(capability, owner.candidate.approvalPolicy, owner.candidate.boundedInput) != nil {
		return owner, "", "", 0, errs.ErrForbidden
	}
	owner.input, owner.normalized, err = managedkubernetes.DecodeOperationInput(owner.candidate.operation, owner.candidate.boundedInput)
	if err != nil {
		return owner, "", "", 0, errs.ErrForbidden
	}
	var resourceScope map[string]string
	if json.Unmarshal(owner.candidate.resourceScope, &resourceScope) != nil || len(resourceScope) != 2 || resourceScope["target_ref"] != selected.target.Ref || owner.input.ValidateGrantScope(owner.candidate.operation, resourceScope["namespace"]) != nil {
		return owner, "", "", 0, errs.ErrForbidden
	}
	var leaseRef, fenceDigest string
	var generation int64
	if err := tx.QueryRow(ctx, queryKubernetesGateConsumerLiveClaim, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": invocationRef}).Scan(&leaseRef, &generation, &fenceDigest, &owner.lease.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return owner, "", "", 0, errs.ErrForbidden
		}
		return owner, "", "", 0, errs.ErrUnavailable
	}
	if !managedkubernetes.ValidLocator(leaseRef) || !managedkubernetes.ValidDigest(fenceDigest) || generation < 1 {
		return owner, "", "", 0, errs.ErrUnavailable
	}
	deadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, owner.candidate.runID)
	if err != nil {
		return owner, "", "", 0, err
	}
	if expired {
		return owner, "", "", 0, errs.ErrForbidden
	}
	if deadline != nil && deadline.EffectiveDeadlineAt.Before(owner.lease.ExpiresAt) {
		owner.lease.ExpiresAt = deadline.EffectiveDeadlineAt
	}
	if !owner.accessUntil.IsZero() && owner.accessUntil.Before(owner.lease.ExpiresAt) {
		owner.lease.ExpiresAt = owner.accessUntil
	}
	if err := owner.creditError(ctx, owner.lease.ExpiresAt); err != nil {
		return owner, "", "", 0, err
	}
	return owner, leaseRef, fenceDigest, generation, nil
}
