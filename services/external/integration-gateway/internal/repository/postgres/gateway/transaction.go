package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (transaction *transaction) StoreDefinition(ctx context.Context, definition entity.Definition) error {
	if definition.CreatedAt.IsZero() {
		definition.CreatedAt = time.Now().UTC()
	}
	payload, err := marshal(definition)
	if err != nil {
		return err
	}
	arguments := pgx.StrictNamedArgs{
		"definition_id": definition.ID, "definition_version": definition.Version,
		"canonical_digest": definition.Digest, "source": definition.Source,
		"payload": payload, "created_at": definition.CreatedAt,
	}
	var identifier, storedDigest string
	err = transaction.tx.QueryRow(ctx, sqlDefinitionInsert, arguments).Scan(
		&identifier,
		&storedDigest,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = transaction.tx.QueryRow(ctx, sqlDefinitionGet, pgx.StrictNamedArgs{
			"definition_id":      definition.ID,
			"definition_version": definition.Version,
		}).Scan(&identifier, &storedDigest)
	}
	if err != nil {
		return err
	}
	if storedDigest != definition.Digest {
		return errs.ErrConflict
	}
	return transaction.appendAudit(ctx, entity.AuditEvent{
		ID: uuid.NewString(), TenantID: transaction.tenantID, ProjectID: transaction.projectID,
		ActorID: "system:definition-loader", Action: "definition.store", ResourceKind: "INTEGRATION_DEFINITION",
		ResourceID: definition.ID, RequestHash: definition.Digest, Outcome: "STORED", OccurredAt: definition.CreatedAt,
	})
}

func (transaction *transaction) AdmitSession(ctx context.Context, admission domainrepo.SessionAdmission) error {
	if admission.Session.TenantID != transaction.tenantID || admission.Session.ProjectID != transaction.projectID {
		return errs.ErrForbidden
	}
	for _, connection := range admission.Connections {
		if connection.TenantID != transaction.tenantID || connection.ProjectID != transaction.projectID || connection.Generation == 0 {
			return errs.ErrForbidden
		}
		payload, err := marshal(connection)
		if err != nil {
			return err
		}
		var identifier string
		if err := transaction.tx.QueryRow(ctx, sqlConnectionUpsert, pgx.StrictNamedArgs{
			"connection_id": connection.ID, "tenant_id": connection.TenantID, "project_id": connection.ProjectID,
			"integration_id": connection.IntegrationID, "revision": connection.Revision, "generation": connection.Generation,
			"status": connection.Status, "definition_id": connection.DefinitionID,
			"definition_version": connection.DefinitionVersion, "payload": payload,
		}).Scan(&identifier); err != nil {
			return err
		}
		var revoked int64
		if err := transaction.tx.QueryRow(ctx, sqlAuthorityRevokeStale, pgx.StrictNamedArgs{
			"tenant_id": connection.TenantID, "project_id": connection.ProjectID,
			"connection_id": connection.ID, "generation": connection.Generation,
		}).Scan(&revoked); err != nil {
			return err
		}
		if err := transaction.appendAudit(ctx, entity.AuditEvent{
			ID: uuid.NewString(), TenantID: connection.TenantID,
			ProjectID: connection.ProjectID, ActorID: admission.Audit.ActorID, Action: "connection.resolve",
			ResourceKind: "INTEGRATION_CONNECTION", ResourceID: connection.ID, Outcome: string(connection.Status),
			OccurredAt: admission.Audit.OccurredAt,
		}); err != nil {
			return err
		}
	}
	for _, grant := range admission.Grants {
		if grant.TenantID != transaction.tenantID || grant.ProjectID != transaction.projectID ||
			grant.ProcessID != admission.Session.ProcessID ||
			grant.SessionID != admission.Session.AgentSessionID || grant.TurnID != admission.Session.TurnID ||
			grant.SessionVersion != admission.Session.AgentSessionVersion || grant.ThreadID != admission.Session.ThreadID ||
			grant.TurnVersion != admission.Session.TurnVersion ||
			grant.Attempt != admission.Session.Attempt || grant.InputDigest != admission.Session.InputDigest ||
			grant.RuntimeRevisionID != admission.Session.RuntimeRevisionID ||
			grant.RuntimeRevisionVersion != admission.Session.RuntimeRevisionVersion ||
			grant.RuntimeRevisionDigest != admission.Session.RuntimeRevisionDigest ||
			grant.RuntimeManifestDigest != admission.Session.RuntimeManifestDigest ||
			grant.RoleID != admission.Session.RoleID || grant.RoleVersion != admission.Session.RoleVersion ||
			grant.Generation != admission.Session.GrantGeneration ||
			grant.ExpiresAt.After(admission.Session.ExpiresAt) {
			return errs.ErrForbidden
		}
		payload, err := marshal(grant)
		if err != nil {
			return err
		}
		var identifier string
		if err := transaction.tx.QueryRow(ctx, sqlGrantUpsert, pgx.StrictNamedArgs{
			"grant_id": grant.ID, "tenant_id": grant.TenantID, "project_id": grant.ProjectID,
			"session_id": grant.SessionID, "turn_id": grant.TurnID, "attempt": grant.Attempt,
			"input_digest": grant.InputDigest, "runtime_revision_id": grant.RuntimeRevisionID,
			"connection_id": grant.ConnectionID, "generation": grant.Generation, "status": grant.Status,
			"expires_at": grant.ExpiresAt, "payload": payload,
		}).Scan(&identifier); err != nil {
			return err
		}
		if err := transaction.appendAudit(ctx, entity.AuditEvent{
			ID: uuid.NewString(), TenantID: grant.TenantID,
			ProjectID: grant.ProjectID, ActorID: admission.Audit.ActorID, Action: "grant.resolve",
			ResourceKind: "INTEGRATION_GRANT", ResourceID: grant.ID, Outcome: string(grant.Status),
			OccurredAt: admission.Audit.OccurredAt,
		}); err != nil {
			return err
		}
	}
	sessionPayload, err := marshal(admission.Session)
	if err != nil {
		return err
	}
	var identifier string
	if err := transaction.tx.QueryRow(ctx, sqlSessionInsert, pgx.StrictNamedArgs{
		"transport_session_id": admission.Session.ID, "tenant_id": admission.Session.TenantID,
		"project_id": admission.Session.ProjectID, "agent_session_id": admission.Session.AgentSessionID,
		"turn_id": admission.Session.TurnID, "attempt": admission.Session.Attempt,
		"input_digest": admission.Session.InputDigest, "runtime_revision_id": admission.Session.RuntimeRevisionID,
		"grant_generation": admission.Session.GrantGeneration, "token_digest": admission.Session.TokenDigest,
		"status": admission.Session.Status, "expires_at": admission.Session.ExpiresAt,
		"request_count": admission.Session.RequestCount, "concurrent_requests": admission.Session.ConcurrentRequests,
		"last_seen_at": admission.Session.LastSeenAt, "payload": sessionPayload,
	}).Scan(&identifier); err != nil {
		return err
	}
	return transaction.appendAudit(ctx, admission.Audit)
}

func (transaction *transaction) ReserveInvocation(ctx context.Context, reservation domainrepo.InvocationReservation) (entity.Invocation, bool, error) {
	if reservation.Invocation.TenantID != transaction.tenantID || reservation.Invocation.ProjectID != transaction.projectID {
		return entity.Invocation{}, false, errs.ErrForbidden
	}
	if _, err := transaction.tx.Exec(ctx, sqlReceiptLock, pgx.StrictNamedArgs{"key_hash": reservation.ReceiptKeyHash}); err != nil {
		return entity.Invocation{}, false, err
	}
	var storedHash string
	var storedRaw []byte
	err := transaction.tx.QueryRow(ctx, sqlReceiptGet, pgx.StrictNamedArgs{
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID, "key_hash": reservation.ReceiptKeyHash,
	}).Scan(&storedHash, &storedRaw)
	if err == nil {
		var stored entity.Invocation
		if json.Unmarshal(storedRaw, &stored) != nil {
			return entity.Invocation{}, false, errors.New("stored invocation is invalid")
		}
		if storedHash != reservation.RequestHash {
			return entity.Invocation{}, false, errs.ErrConflict
		}
		return stored, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return entity.Invocation{}, false, err
	}
	var authorityCurrent bool
	if err := transaction.tx.QueryRow(ctx, sqlInvocationAuthorityLock, pgx.StrictNamedArgs{
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID,
		"transport_session_id":  reservation.Invocation.TransportSessionID,
		"connection_id":         reservation.Invocation.ConnectionID,
		"connection_generation": reservation.Invocation.ConnectionGeneration,
		"grant_id":              reservation.Invocation.GrantID,
		"grant_generation":      reservation.Invocation.GrantGeneration,
	}).Scan(&authorityCurrent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return entity.Invocation{}, false, errs.ErrForbidden
		}
		return entity.Invocation{}, false, err
	}
	if !authorityCurrent {
		return entity.Invocation{}, false, errs.ErrForbidden
	}
	payload, err := marshal(reservation.Invocation)
	if err != nil {
		return entity.Invocation{}, false, err
	}
	if _, err := transaction.tx.Exec(ctx, sqlInvocationInsert, invocationArgs(reservation.Invocation, payload)); err != nil {
		return entity.Invocation{}, false, err
	}
	if reservation.Continuation.InvocationID != reservation.Invocation.ID ||
		reservation.Continuation.TenantID != transaction.tenantID ||
		reservation.Continuation.ProjectID != transaction.projectID ||
		reservation.Continuation.Action != enum.ContinuationSuspend ||
		reservation.Continuation.ApplicationGrantExpiresAt.IsZero() {
		return entity.Invocation{}, false, errs.ErrInvalid
	}
	continuationPayload, err := marshal(reservation.Continuation)
	if err != nil {
		return entity.Invocation{}, false, err
	}
	if _, err := transaction.tx.Exec(ctx, sqlContinuationInsert, pgx.StrictNamedArgs{
		"invocation_id": reservation.Continuation.InvocationID,
		"tenant_id":     transaction.tenantID, "project_id": transaction.projectID,
		"action": reservation.Continuation.Action, "desired_action": reservation.Continuation.DesiredAction,
		"application_grant_expires_at": reservation.Continuation.ApplicationGrantExpiresAt,
		"available_at":                 reservation.Continuation.AvailableAt, "payload": continuationPayload,
		"updated_at": reservation.Invocation.CreatedAt,
	}); err != nil {
		return entity.Invocation{}, false, err
	}
	if reservation.Approval != nil {
		approvalPayload, err := marshal(*reservation.Approval)
		if err != nil {
			return entity.Invocation{}, false, err
		}
		if _, err := transaction.tx.Exec(ctx, sqlApprovalInsert, pgx.StrictNamedArgs{
			"approval_id": reservation.Approval.ID, "tenant_id": transaction.tenantID,
			"project_id": transaction.projectID, "invocation_id": reservation.Approval.InvocationID,
			"request_hash": reservation.Approval.RequestHash, "status": reservation.Approval.Status,
			"expires_at": reservation.Approval.ExpiresAt, "payload": approvalPayload,
			"created_at": reservation.Invocation.CreatedAt, "decided_at": reservation.Approval.DecidedAt,
		}); err != nil {
			return entity.Invocation{}, false, err
		}
	}
	if _, err := transaction.tx.Exec(ctx, sqlReceiptInsert, pgx.StrictNamedArgs{
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID,
		"key_hash": reservation.ReceiptKeyHash, "request_hash": reservation.RequestHash,
		"invocation_id": reservation.Invocation.ID, "created_at": reservation.Invocation.CreatedAt,
	}); err != nil {
		return entity.Invocation{}, false, err
	}
	if err := transaction.appendAudit(ctx, reservation.Audit); err != nil {
		return entity.Invocation{}, false, err
	}
	return reservation.Invocation, false, nil
}

func (transaction *transaction) DecideApproval(ctx context.Context, decision domainrepo.Decision) (entity.Invocation, bool, error) {
	var approvalRaw, invocationRaw []byte
	var approvalStatus enum.ApprovalStatus
	var approvalVersion uint64
	var invocationStatus enum.InvocationStatus
	var expiresAt, databaseNow time.Time
	if err := transaction.tx.QueryRow(ctx, sqlApprovalLock, pgx.StrictNamedArgs{
		"approval_id": decision.ApprovalID, "tenant_id": transaction.tenantID, "project_id": transaction.projectID,
	}).Scan(&approvalRaw, &approvalVersion, &approvalStatus, &expiresAt, &invocationRaw, &invocationStatus, &databaseNow); err != nil {
		return entity.Invocation{}, false, err
	}
	stored, replay, err := transaction.replayReceipt(ctx, decision.ReceiptKeyHash, decision.RequestHash)
	if err != nil || replay {
		return stored, replay, err
	}
	if decision.ExpectedVersion != 0 && approvalVersion != decision.ExpectedVersion ||
		approvalStatus != enum.ApprovalPending ||
		invocationStatus != enum.InvocationPendingApproval || !expiresAt.After(databaseNow) {
		return entity.Invocation{}, false, errs.ErrConflict
	}
	var approval entity.Approval
	var invocation entity.Invocation
	if json.Unmarshal(approvalRaw, &approval) != nil || json.Unmarshal(invocationRaw, &invocation) != nil {
		return entity.Invocation{}, false, errors.New("stored approval state is invalid")
	}
	if decision.ExpectedRequestHash != "" && approval.RequestHash != decision.ExpectedRequestHash {
		return entity.Invocation{}, false, errs.ErrConflict
	}
	approval.Version = approvalVersion + 1
	approval.DecidedBy = decision.ActorID
	approval.DecisionReasonCode = decision.ReasonCode
	approval.DecidedAt = &databaseNow
	if decision.Approve {
		approval.Status = enum.ApprovalApproved
		invocation.Status = enum.InvocationApproved
	} else {
		approval.Status = enum.ApprovalRejected
		invocation.Status = enum.InvocationRejected
	}
	invocation.UpdatedAt = databaseNow
	approvalPayload, err := marshal(approval)
	if err != nil {
		return entity.Invocation{}, false, err
	}
	invocationPayload, err := marshal(invocation)
	if err != nil {
		return entity.Invocation{}, false, err
	}
	var updatedRaw []byte
	if err := transaction.tx.QueryRow(ctx, sqlApprovalUpdate, pgx.StrictNamedArgs{
		"approval_id": approval.ID, "approval_status": approval.Status, "approval_payload": approvalPayload,
		"decided_at": databaseNow, "request_hash": approval.RequestHash,
		"invocation_status": invocation.Status, "invocation_payload": invocationPayload,
	}).Scan(&updatedRaw); err != nil {
		return entity.Invocation{}, false, err
	}
	if err := transaction.saveReceipt(ctx, decision.ReceiptKeyHash, decision.RequestHash, invocation.ID, databaseNow); err != nil {
		return entity.Invocation{}, false, err
	}
	decision.Audit.RequestHash = approval.RequestHash
	decision.Audit.OccurredAt = databaseNow
	if err := transaction.appendAudit(ctx, decision.Audit); err != nil {
		return entity.Invocation{}, false, err
	}
	action := enum.ContinuationReject
	if decision.Approve {
		action = enum.ContinuationApprove
	}
	if err := transaction.scheduleContinuation(ctx, invocation.ID, action, databaseNow); err != nil {
		return entity.Invocation{}, false, err
	}
	return invocation, false, nil
}

func (transaction *transaction) CancelInvocation(ctx context.Context, cancellation domainrepo.Cancellation) (entity.Invocation, bool, error) {
	var invocationRaw, approvalRaw []byte
	var status enum.InvocationStatus
	var expiresAt, databaseNow time.Time
	if err := transaction.tx.QueryRow(ctx, sqlInvocationCancelLock, pgx.StrictNamedArgs{
		"invocation_id": cancellation.InvocationID, "tenant_id": transaction.tenantID,
		"project_id": transaction.projectID, "transport_session_id": cancellation.ExpectedTransportSessionID,
	}).Scan(&invocationRaw, &status, &expiresAt, &approvalRaw, &databaseNow); err != nil {
		return entity.Invocation{}, false, err
	}
	var invocation entity.Invocation
	var approval entity.Approval
	if json.Unmarshal(invocationRaw, &invocation) != nil || json.Unmarshal(approvalRaw, &approval) != nil {
		return entity.Invocation{}, false, errors.New("stored cancellation state is invalid")
	}
	stored, replay, err := transaction.replayReceipt(ctx, cancellation.ReceiptKeyHash, cancellation.RequestHash)
	if err != nil || replay {
		return stored, replay, err
	}
	if status != enum.InvocationPendingApproval && status != enum.InvocationApproved || !expiresAt.After(databaseNow) {
		return entity.Invocation{}, false, errs.ErrConflict
	}
	cancellation.CancelledAt = databaseNow
	invocation.Status = enum.InvocationCancelled
	invocation.UpdatedAt = cancellation.CancelledAt
	invocationPayload, err := marshal(invocation)
	if err != nil {
		return entity.Invocation{}, false, err
	}
	if approval.ID != "" && (approval.Status == enum.ApprovalPending || approval.Status == enum.ApprovalApproved) {
		approval.Status = enum.ApprovalCancelled
		approval.Version++
		approval.DecidedBy = cancellation.ActorID
		approval.DecisionReasonCode = cancellation.ReasonCode
		approval.DecidedAt = &cancellation.CancelledAt
		approvalPayload, err := marshal(approval)
		if err != nil {
			return entity.Invocation{}, false, err
		}
		var approvalID string
		if err := transaction.tx.QueryRow(ctx, sqlApprovalCancel, pgx.StrictNamedArgs{
			"invocation_id": invocation.ID, "payload": approvalPayload, "cancelled_at": cancellation.CancelledAt,
		}).Scan(&approvalID); err != nil {
			return entity.Invocation{}, false, err
		}
	}
	var invocationID string
	if err := transaction.tx.QueryRow(ctx, sqlInvocationCancel, pgx.StrictNamedArgs{
		"invocation_id": invocation.ID, "payload": invocationPayload, "cancelled_at": cancellation.CancelledAt,
	}).Scan(&invocationID); err != nil {
		return entity.Invocation{}, false, err
	}
	if err := transaction.saveReceipt(ctx, cancellation.ReceiptKeyHash, cancellation.RequestHash, invocation.ID, cancellation.CancelledAt); err != nil {
		return entity.Invocation{}, false, err
	}
	cancellation.Audit.RequestHash = invocation.CanonicalRequestHash
	if err := transaction.appendAudit(ctx, cancellation.Audit); err != nil {
		return entity.Invocation{}, false, err
	}
	if err := transaction.scheduleContinuation(ctx, invocation.ID, enum.ContinuationCancel, cancellation.CancelledAt); err != nil {
		return entity.Invocation{}, false, err
	}
	return invocation, false, nil
}

func (transaction *transaction) replayReceipt(ctx context.Context, keyHash, requestHash string) (entity.Invocation, bool, error) {
	if keyHash == "" || requestHash == "" {
		return entity.Invocation{}, false, errs.ErrInvalid
	}
	if _, err := transaction.tx.Exec(ctx, sqlReceiptLock, pgx.StrictNamedArgs{"key_hash": keyHash}); err != nil {
		return entity.Invocation{}, false, err
	}
	var storedHash string
	var storedRaw []byte
	err := transaction.tx.QueryRow(ctx, sqlReceiptGet, pgx.StrictNamedArgs{
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID, "key_hash": keyHash,
	}).Scan(&storedHash, &storedRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.Invocation{}, false, nil
	}
	if err != nil {
		return entity.Invocation{}, false, err
	}
	if storedHash != requestHash {
		return entity.Invocation{}, false, errs.ErrConflict
	}
	var stored entity.Invocation
	if json.Unmarshal(storedRaw, &stored) != nil {
		return entity.Invocation{}, false, errors.New("stored operation receipt is invalid")
	}
	return stored, true, nil
}

func (transaction *transaction) saveReceipt(ctx context.Context, keyHash, requestHash, invocationID string, createdAt time.Time) error {
	_, err := transaction.tx.Exec(ctx, sqlReceiptInsert, pgx.StrictNamedArgs{
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID,
		"key_hash": keyHash, "request_hash": requestHash, "invocation_id": invocationID, "created_at": createdAt,
	})
	return err
}

func (transaction *transaction) ClaimExecution(ctx context.Context, now time.Time) (domainrepo.ExecutionClaim, bool, error) {
	var invocationRaw, connectionRaw, grantRaw, definitionRaw, attemptRaw []byte
	var invocationStatus enum.InvocationStatus
	var continuationExecutionState string
	err := transaction.tx.QueryRow(ctx, sqlExecutionClaim, pgx.StrictNamedArgs{
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID,
	}).Scan(&invocationRaw, &connectionRaw, &grantRaw, &definitionRaw,
		&invocationStatus, &continuationExecutionState, &attemptRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainrepo.ExecutionClaim{}, false, nil
	}
	if err != nil {
		return domainrepo.ExecutionClaim{}, false, err
	}
	var invocation entity.Invocation
	var connection entity.Connection
	var grant entity.Grant
	var definition entity.Definition
	if json.Unmarshal(invocationRaw, &invocation) != nil || json.Unmarshal(connectionRaw, &connection) != nil ||
		json.Unmarshal(grantRaw, &grant) != nil || json.Unmarshal(definitionRaw, &definition) != nil {
		return domainrepo.ExecutionClaim{}, false, errors.New("stored execution claim is invalid")
	}
	if invocation.PinnedConnection.ID != invocation.ConnectionID ||
		invocation.PinnedConnection.Revision != invocation.ConnectionRevision ||
		invocation.PinnedConnection.Generation != invocation.ConnectionGeneration ||
		entity.ConnectionBindingDigest(invocation.PinnedConnection) != invocation.ConnectionBindingDigest ||
		entity.ConnectionBindingDigest(connection) != invocation.ConnectionBindingDigest ||
		definition.ID != invocation.DefinitionID || definition.Version != invocation.DefinitionVersion ||
		definition.Digest != invocation.DefinitionDigest {
		return domainrepo.ExecutionClaim{}, false, errs.ErrConflict
	}
	for _, binding := range invocation.PinnedConnection.CredentialBindingRefs {
		if binding.ID == "" || binding.Version == 0 || binding.Revision == 0 ||
			(binding.ExpiresAt != nil && !binding.ExpiresAt.After(now)) || binding.ProjectionDigest == "" {
			return domainrepo.ExecutionClaim{}, false, errs.ErrExpired
		}
	}
	var tool entity.Tool
	found := false
	for _, candidate := range definition.Tools {
		if candidate.Name == invocation.ToolName && candidate.Version == invocation.ToolVersion &&
			candidate.Capability == invocation.Capability && candidate.Permission == invocation.Permission {
			tool, found = candidate, true
			break
		}
	}
	if !found || !slices.Contains(grant.Capabilities, tool.Capability) || !slices.Contains(grant.Permissions, tool.Permission) {
		return domainrepo.ExecutionClaim{}, false, errs.ErrForbidden
	}
	if invocation.PinnedTool.Name != tool.Name || invocation.PinnedTool.Version != tool.Version ||
		invocation.PinnedTool.Capability != tool.Capability || invocation.PinnedTool.Permission != tool.Permission {
		return domainrepo.ExecutionClaim{}, false, errs.ErrConflict
	}
	connection = invocation.PinnedConnection
	tool = invocation.PinnedTool
	if invocationStatus == enum.InvocationExecuting {
		var attempt entity.ExecutionAttempt
		if continuationExecutionState != "EXECUTING" || len(attemptRaw) == 0 ||
			json.Unmarshal(attemptRaw, &attempt) != nil || attempt.FinishedAt != nil ||
			attempt.ProviderDispatchedAt != nil {
			return domainrepo.ExecutionClaim{}, false, errs.ErrConflict
		}
		if _, err := transaction.tx.Exec(ctx, sqlExecutionWorkScopeDelete,
			pgx.StrictNamedArgs{"invocation_id": invocation.ID}); err != nil {
			return domainrepo.ExecutionClaim{}, false, err
		}
		return domainrepo.ExecutionClaim{
			Invocation: invocation, Attempt: attempt, Tool: tool,
			Connection: connection, ProviderReady: true,
		}, true, nil
	}
	if invocationStatus != enum.InvocationApproved || continuationExecutionState != "NOT_STARTED" {
		return domainrepo.ExecutionClaim{}, false, errs.ErrConflict
	}
	attempt := entity.ExecutionAttempt{
		ID: uuid.NewString(), InvocationID: invocation.ID, Number: 1, Fence: 1,
		ConnectionGeneration: invocation.ConnectionGeneration, GrantGeneration: invocation.GrantGeneration,
		ProviderIdempotencyKey: invocation.CanonicalRequestHash, StartedAt: now, Outcome: enum.InvocationExecuting,
	}
	attemptPayload, err := marshal(attempt)
	if err != nil {
		return domainrepo.ExecutionClaim{}, false, err
	}
	if _, err := transaction.tx.Exec(ctx, sqlAttemptInsert, pgx.StrictNamedArgs{
		"attempt_id": attempt.ID, "tenant_id": transaction.tenantID, "project_id": transaction.projectID,
		"invocation_id": invocation.ID, "attempt_number": attempt.Number, "fence": attempt.Fence,
		"connection_generation": attempt.ConnectionGeneration, "grant_generation": attempt.GrantGeneration,
		"provider_idempotency_key": attempt.ProviderIdempotencyKey, "outcome": attempt.Outcome,
		"payload": attemptPayload, "started_at": attempt.StartedAt,
	}); err != nil {
		return domainrepo.ExecutionClaim{}, false, err
	}
	invocation.Status = enum.InvocationExecuting
	invocation.UpdatedAt = now
	invocationPayload, err := marshal(invocation)
	if err != nil {
		return domainrepo.ExecutionClaim{}, false, err
	}
	var identifier string
	if err := transaction.tx.QueryRow(ctx, sqlInvocationMarkExecuting, pgx.StrictNamedArgs{
		"invocation_id": invocation.ID, "payload": invocationPayload, "updated_at": now,
	}).Scan(&identifier); err != nil {
		return domainrepo.ExecutionClaim{}, false, err
	}
	if err := transaction.appendAudit(ctx, entity.AuditEvent{
		ID: uuid.NewString(), TenantID: transaction.tenantID,
		ProjectID: transaction.projectID, ActorID: "system:integration-gateway-worker", Action: "tool.claim",
		ResourceKind: "EXECUTION_ATTEMPT", ResourceID: attempt.ID, RequestHash: invocation.CanonicalRequestHash,
		Outcome: "EXECUTING", OccurredAt: now,
	}); err != nil {
		return domainrepo.ExecutionClaim{}, false, err
	}
	if err := transaction.scheduleContinuation(ctx, invocation.ID, enum.ContinuationBegin, now); err != nil {
		return domainrepo.ExecutionClaim{}, false, err
	}
	return domainrepo.ExecutionClaim{Invocation: invocation, Attempt: attempt, Tool: tool, Connection: connection}, true, nil
}

func (transaction *transaction) MarkProviderDispatched(
	ctx context.Context,
	invocationID string,
	attemptID string,
	dispatchedAt time.Time,
) error {
	if invocationID == "" || attemptID == "" || dispatchedAt.IsZero() {
		return errs.ErrInvalid
	}
	var raw []byte
	if err := transaction.tx.QueryRow(ctx, sqlExecutionAttemptLock, pgx.StrictNamedArgs{
		"invocation_id": invocationID, "attempt_id": attemptID,
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID,
	}).Scan(&raw); err != nil {
		return err
	}
	var attempt entity.ExecutionAttempt
	if json.Unmarshal(raw, &attempt) != nil || attempt.FinishedAt != nil || attempt.ProviderDispatchedAt != nil {
		return errs.ErrConflict
	}
	attempt.ProviderDispatchedAt = &dispatchedAt
	payload, err := marshal(attempt)
	if err != nil {
		return err
	}
	var identifier string
	return transaction.tx.QueryRow(ctx, sqlExecutionDispatch, pgx.StrictNamedArgs{
		"attempt_id": attemptID, "invocation_id": invocationID,
		"dispatched_at": dispatchedAt, "payload": payload,
	}).Scan(&identifier)
}

func (transaction *transaction) CompleteExecution(ctx context.Context, completion domainrepo.ExecutionCompletion) error {
	var invocationRaw, attemptRaw []byte
	var connectionStatus enum.ConnectionStatus
	var connectionGeneration uint64
	var grantStatus enum.GrantStatus
	var grantGeneration uint64
	if err := transaction.tx.QueryRow(ctx, sqlExecutionLock, pgx.StrictNamedArgs{
		"invocation_id": completion.InvocationID, "attempt_id": completion.AttemptID,
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID,
	}).Scan(&invocationRaw, &attemptRaw, &connectionStatus, &connectionGeneration, &grantStatus, &grantGeneration); err != nil {
		return err
	}
	var invocation entity.Invocation
	var attempt entity.ExecutionAttempt
	if json.Unmarshal(invocationRaw, &invocation) != nil || json.Unmarshal(attemptRaw, &attempt) != nil {
		return errors.New("stored execution state is invalid")
	}
	if invocation.Status != enum.InvocationExecuting || attempt.Fence != completion.Fence || attempt.FinishedAt != nil {
		return errs.ErrConflict
	}
	if connectionStatus != enum.ConnectionValid || grantStatus != enum.GrantActive ||
		connectionGeneration != completion.ConnectionGeneration || grantGeneration != completion.GrantGeneration {
		completion.Result.Status = enum.InvocationUnknown
		completion.Result.EncryptedPayload = nil
		completion.Result.PayloadDigest = storedResultDigest(
			completion.InvocationID,
			completion.AttemptID,
			enum.InvocationUnknown,
		)
		completion.Result.ProviderReceipt = ""
		completion.Audit.Outcome = string(enum.InvocationUnknown)
		completion.Audit.ReasonCode = "AUTHORITY_GENERATION_CHANGED"
	}
	completion.Result.DeliveryVersion = 1
	completion.Result.DeliveryFence = attempt.Fence
	finishedAt := completion.Result.CompletedAt
	attempt.Outcome = completion.Result.Status
	attempt.FinishedAt = &finishedAt
	invocation.Status = completion.Result.Status
	invocation.UpdatedAt = finishedAt
	attemptPayload, err := marshal(attempt)
	if err != nil {
		return err
	}
	invocationPayload, err := marshal(invocation)
	if err != nil {
		return err
	}
	resultPayload, err := marshal(completion.Result)
	if err != nil {
		return err
	}
	var identifier string
	if err := transaction.tx.QueryRow(ctx, sqlAttemptComplete, pgx.StrictNamedArgs{
		"attempt_id": attempt.ID, "invocation_id": invocation.ID, "fence": attempt.Fence,
		"connection_generation": attempt.ConnectionGeneration, "grant_generation": attempt.GrantGeneration,
		"outcome": attempt.Outcome, "payload": attemptPayload, "finished_at": finishedAt,
	}).Scan(&identifier); err != nil {
		return err
	}
	if err := transaction.tx.QueryRow(ctx, sqlResultInsert, pgx.StrictNamedArgs{
		"invocation_id": invocation.ID, "tenant_id": transaction.tenantID, "project_id": transaction.projectID,
		"attempt_id": attempt.ID, "status": completion.Result.Status, "payload": resultPayload, "completed_at": finishedAt,
	}).Scan(&identifier); err != nil {
		return err
	}
	if err := transaction.tx.QueryRow(ctx, sqlInvocationComplete, pgx.StrictNamedArgs{
		"invocation_id": invocation.ID, "status": invocation.Status, "payload": invocationPayload,
		"updated_at": finishedAt, "connection_generation": completion.ConnectionGeneration,
		"grant_generation": completion.GrantGeneration,
	}).Scan(&identifier); err != nil {
		return err
	}
	if err := transaction.appendAudit(ctx, completion.Audit); err != nil {
		return err
	}
	action := enum.ContinuationFail
	if completion.Result.Status == enum.InvocationSucceeded {
		action = enum.ContinuationSucceed
	}
	return transaction.scheduleContinuation(ctx, invocation.ID, action, finishedAt)
}

func storedResultDigest(invocationID, attemptID string, status enum.InvocationStatus) string {
	reference := "integration-gateway://invocations/" + invocationID + "/results/" + attemptID
	digest := sha256.Sum256([]byte(reference + "\x00" + string(status)))
	return hex.EncodeToString(digest[:])
}

func (transaction *transaction) SetConnectionValidation(ctx context.Context, validation domainrepo.ConnectionValidation) error {
	if validation.Status.TenantID != transaction.tenantID || validation.Status.ProjectID != transaction.projectID ||
		validation.Status.ID != validation.ConnectionID || validation.Status.Generation != validation.ExpectedGeneration {
		return errs.ErrForbidden
	}
	payload, err := marshal(validation.Status)
	if err != nil {
		return err
	}
	var identifier string
	if err := transaction.tx.QueryRow(ctx, sqlConnectionValidate, pgx.StrictNamedArgs{
		"connection_id": validation.ConnectionID, "tenant_id": transaction.tenantID,
		"project_id": transaction.projectID, "expected_generation": validation.ExpectedGeneration,
		"status": validation.Status.Status, "payload": payload, "updated_at": validation.Audit.OccurredAt,
	}).Scan(&identifier); err != nil {
		return err
	}
	return transaction.appendAudit(ctx, validation.Audit)
}

func (transaction *transaction) CloseSession(ctx context.Context, sessionID string, closedAt time.Time, audit entity.AuditEvent) error {
	var identifier string
	if err := transaction.tx.QueryRow(ctx, sqlSessionClose, pgx.StrictNamedArgs{
		"transport_session_id": sessionID, "closed_at": closedAt,
	}).Scan(&identifier); err != nil {
		return err
	}
	return transaction.appendAudit(ctx, audit)
}

func (transaction *transaction) Expire(ctx context.Context, _ time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 1000 {
		return 0, errs.ErrInvalid
	}
	var count int64
	err := transaction.tx.QueryRow(ctx, sqlLifecycleExpire, pgx.StrictNamedArgs{"limit": limit}).Scan(&count)
	return count, err
}

func (transaction *transaction) ClaimContinuation(
	ctx context.Context,
	leaseDuration time.Duration,
) (domainrepo.ContinuationClaim, bool, error) {
	if leaseDuration < time.Second || leaseDuration > 30*time.Second {
		return domainrepo.ContinuationClaim{}, false, errs.ErrInvalid
	}
	leaseID := uuid.NewString()
	var effectRaw, invocationRaw, approvalRaw, attemptRaw, resultRaw []byte
	var effect entity.ContinuationEffect
	var action, desiredAction enum.ContinuationAction
	var continuationID, approvalState, executionState, continuationState string
	var continuationVersion, continuationFence, leaseFence uint64
	var grantExpiresAt, availableAt time.Time
	var attempts uint32
	var leaseExpiresAt *time.Time
	err := transaction.tx.QueryRow(ctx, sqlContinuationClaim, pgx.StrictNamedArgs{
		"tenant_id": transaction.tenantID, "project_id": transaction.projectID,
		"lease_id": leaseID, "lease_duration_milliseconds": leaseDuration.Milliseconds(),
	}).Scan(
		&effectRaw, &action, &desiredAction,
		&continuationID, &continuationVersion, &continuationFence,
		&approvalState, &executionState, &continuationState,
		&grantExpiresAt, &availableAt,
		&effect.LeaseID, &leaseFence, &leaseExpiresAt, &attempts,
		&invocationRaw, &approvalRaw, &attemptRaw, &resultRaw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domainrepo.ContinuationClaim{}, false, nil
	}
	if err != nil {
		return domainrepo.ContinuationClaim{}, false, err
	}
	if json.Unmarshal(effectRaw, &effect) != nil {
		return domainrepo.ContinuationClaim{}, false, errors.New("stored continuation effect is invalid")
	}
	// Dynamic fence/state columns всегда сильнее immutable payload.
	effect.Action = action
	effect.DesiredAction = desiredAction
	effect.ContinuationID = continuationID
	effect.Version = continuationVersion
	effect.Fence = continuationFence
	effect.ApprovalState = approvalState
	effect.ExecutionState = executionState
	effect.ContinuationState = continuationState
	effect.ApplicationGrantExpiresAt = grantExpiresAt
	effect.AvailableAt = availableAt
	effect.LeaseID = leaseID
	effect.LeaseFence = leaseFence
	effect.Attempts = attempts
	if leaseExpiresAt != nil {
		effect.LeaseExpiresAt = *leaseExpiresAt
	}
	var claim domainrepo.ContinuationClaim
	claim.Effect = effect
	if json.Unmarshal(invocationRaw, &claim.Invocation) != nil ||
		json.Unmarshal(approvalRaw, &claim.Approval) != nil {
		return domainrepo.ContinuationClaim{}, false, errors.New("stored continuation binding is invalid")
	}
	if len(attemptRaw) > 0 {
		claim.Attempt = &entity.ExecutionAttempt{}
		if json.Unmarshal(attemptRaw, claim.Attempt) != nil {
			return domainrepo.ContinuationClaim{}, false, errors.New("stored continuation attempt is invalid")
		}
	}
	if len(resultRaw) > 0 {
		claim.Result = &entity.Result{}
		if json.Unmarshal(resultRaw, claim.Result) != nil {
			return domainrepo.ContinuationClaim{}, false, errors.New("stored continuation result is invalid")
		}
	}
	return claim, true, nil
}

func (transaction *transaction) CompleteContinuation(
	ctx context.Context,
	completion domainrepo.ContinuationCompletion,
) error {
	var currentRaw []byte
	if err := transaction.tx.QueryRow(ctx, sqlContinuationLock, pgx.StrictNamedArgs{
		"invocation_id": completion.InvocationID,
	}).Scan(&currentRaw); err != nil {
		return err
	}
	var effect entity.ContinuationEffect
	if json.Unmarshal(currentRaw, &effect) != nil {
		return errors.New("stored continuation effect is invalid")
	}
	effect.EncryptedApplicationGrant = completion.EncryptedTransitionGrant
	effect.ApplicationGrantExpiresAt = completion.TransitionGrantExpiresAt
	effect.ContinuationID = completion.State.ID
	effect.Version = completion.State.Version
	effect.Fence = completion.State.Fence
	effect.ApprovalState = completion.State.ApprovalState
	effect.ExecutionState = completion.State.ExecutionState
	effect.ContinuationState = completion.State.ContinuationState
	payload, err := marshal(effect)
	if err != nil {
		return err
	}
	var identifier string
	return transaction.tx.QueryRow(ctx, sqlContinuationComplete, pgx.StrictNamedArgs{
		"invocation_id": completion.InvocationID, "action": completion.Action,
		"lease_id": completion.LeaseID, "lease_fence": completion.LeaseFence,
		"continuation_id":             completion.State.ID,
		"continuation_version":        completion.State.Version,
		"continuation_fence":          completion.State.Fence,
		"approval_state":              completion.State.ApprovalState,
		"execution_state":             completion.State.ExecutionState,
		"continuation_state":          completion.State.ContinuationState,
		"transition_grant_expires_at": optionalTime(completion.TransitionGrantExpiresAt),
		"continuation_payload":        payload,
	}).Scan(&identifier)
}

func (transaction *transaction) RetryContinuation(
	ctx context.Context,
	retry domainrepo.ContinuationRetry,
) error {
	if retry.Backoff < 250*time.Millisecond || retry.Backoff > 5*time.Second {
		return errs.ErrInvalid
	}
	var identifier string
	return transaction.tx.QueryRow(ctx, sqlContinuationRetry, pgx.StrictNamedArgs{
		"invocation_id": retry.InvocationID, "action": retry.Action,
		"lease_id": retry.LeaseID, "lease_fence": retry.LeaseFence,
		"backoff_milliseconds": retry.Backoff.Milliseconds(),
	}).Scan(&identifier)
}

func (transaction *transaction) appendAudit(ctx context.Context, audit entity.AuditEvent) error {
	if audit.ID == "" {
		audit.ID = uuid.NewString()
	}
	if audit.TenantID == "" {
		audit.TenantID = transaction.tenantID
	}
	if audit.ProjectID == "" {
		audit.ProjectID = transaction.projectID
	}
	if audit.ActorID == "" {
		audit.ActorID = "system:integration-gateway"
	}
	if audit.OccurredAt.IsZero() {
		audit.OccurredAt = time.Now().UTC()
	}
	_, err := transaction.tx.Exec(ctx, sqlAuditInsert, pgx.StrictNamedArgs{
		"audit_id": audit.ID, "tenant_id": audit.TenantID, "project_id": audit.ProjectID,
		"actor_id": audit.ActorID, "action": audit.Action, "resource_kind": audit.ResourceKind,
		"resource_id": audit.ResourceID, "request_hash": audit.RequestHash, "outcome": audit.Outcome,
		"reason_code": audit.ReasonCode, "occurred_at": audit.OccurredAt,
	})
	return err
}

func (transaction *transaction) scheduleContinuation(
	ctx context.Context,
	invocationID string,
	action enum.ContinuationAction,
	now time.Time,
) error {
	var identifier string
	return transaction.tx.QueryRow(ctx, sqlContinuationSchedule, pgx.StrictNamedArgs{
		"invocation_id": invocationID, "tenant_id": transaction.tenantID,
		"project_id": transaction.projectID, "desired_action": action,
		"available_at": now, "updated_at": now,
	}).Scan(&identifier)
}

func invocationArgs(invocation entity.Invocation, payload []byte) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{
		"invocation_id": invocation.ID, "tenant_id": invocation.TenantID, "project_id": invocation.ProjectID,
		"transport_session_id": invocation.TransportSessionID, "agent_session_id": invocation.AgentSessionID,
		"turn_id": invocation.TurnID, "attempt": invocation.Attempt, "connection_id": invocation.ConnectionID,
		"connection_generation": invocation.ConnectionGeneration, "grant_id": invocation.GrantID,
		"grant_generation": invocation.GrantGeneration, "semantic_key": invocation.SemanticKey,
		"canonical_request_hash": invocation.CanonicalRequestHash, "status": invocation.Status,
		"expires_at": invocation.ExpiresAt, "payload": payload, "created_at": invocation.CreatedAt,
		"updated_at": invocation.UpdatedAt,
	}
}

func marshal(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("marshal repository payload")
	}
	return payload, nil
}

func optionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
