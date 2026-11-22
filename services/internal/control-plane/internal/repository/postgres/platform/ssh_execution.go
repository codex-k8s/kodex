package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryIntegrationInvocationBrokerKind rejects generic finalization of purpose-owned identity invocations, preserving the existing KUBE boundary while adding SSH.
// queryIntegrationInvocationBrokerKind отвергает generic finalization purpose-owned identity invocations, сохраняя existing KUBE boundary и добавляя SSH.
//
//go:embed sql/integration_invocation__broker_kind.sql
var queryIntegrationInvocationBrokerKind string

// querySSHExecutionLease binds the exact live outer lease to the immutable source and one database clock.
// querySSHExecutionLease связывает exact live outer lease с immutable source и одним database clock.
//
//go:embed sql/ssh_execution__lease.sql
var querySSHExecutionLease string

// querySSHExecutionInsert spends sole dispatch credit before any private material leaves its owner.
// querySSHExecutionInsert расходует sole dispatch credit до выдачи private material owner.
//
//go:embed sql/ssh_execution__insert.sql
var querySSHExecutionInsert string

// querySSHExecutionRead locks the original invocation and reads historical intent/receipt without requiring live effect authority.
// querySSHExecutionRead блокирует original invocation и читает historical intent/receipt без требования live effect authority.
//
//go:embed sql/ssh_execution__read.sql
var querySSHExecutionRead string

// querySSHExecutionObserve appends one immutable classified observation; conflicting completions never overwrite it.
// querySSHExecutionObserve добавляет одно immutable classified observation; conflicting completions не заменяют его.
//
//go:embed sql/ssh_execution__observe.sql
var querySSHExecutionObserve string

// querySSHExecutionFinish releases output only while original live authority remains current at the final state update.
// querySSHExecutionFinish выдаёт output лишь при current original live authority в final state update.
//
//go:embed sql/ssh_execution__finish.sql
var querySSHExecutionFinish string

// querySSHExecutionReceiptSource resolves an original helper recipe only within the same immutable identity and verified target qualification.
// querySSHExecutionReceiptSource разрешает original helper recipe лишь в той же immutable identity и verified target qualification.
//
//go:embed sql/ssh_execution__receipt_source.sql
var querySSHExecutionReceiptSource string

// sshExecutionRow retains original fence hashes and private immutable snapshots, never a newly issued admission or reusable token.
// sshExecutionRow сохраняет original fence hashes и private immutable snapshots без новой admission либо reusable token.
type sshExecutionRow struct {
	id, invocationID, nodeRef, rootID, projectID string
	instance, lease, fenceDigest, inputDigest    string
	generation                                   int64
	outerState                                   string
	created, expires                             time.Time
	snapshot                                     []byte
	receipt                                      entity.SSHExecutionReceipt
}

// sshExecutionArguments supplies only previously validated original claim coordinates to fixed SQL; hashes remain integrity checks rather than authority.
// sshExecutionArguments передаёт fixed SQL лишь previously validated original claim coordinates; hashes служат integrity checks, не authority.
func sshExecutionArguments(organization string, binding managedssh.Binding) pgx.StrictNamedArgs {
	return pgx.StrictNamedArgs{"organization_id": organization, "invocation_ref": binding.InvocationRef, "instance": binding.WorkloadInstance,
		"lease_ref": binding.LeaseRef, "fence_digest": sshHealthHash([]byte(binding.Fence)), "generation": binding.Generation, "input_digest": binding.InputDigest}
}

// beginSSHExecutionOwner establishes the protected broker identity and bounded Serializable transaction without reminting permissions or committing a partial transition.
// beginSSHExecutionOwner устанавливает protected broker identity и bounded Serializable transaction без remint permissions либо partial transition commit.
func (repository *Repository) beginSSHExecutionOwner(ctx context.Context, p value.Principal, binding managedssh.Binding, permission string) (scope, pgx.Tx, error) {
	if p.CallerWorkload != "secret-broker" || p.Permission != permission || p.ProjectRef != "" || binding.Validate() != nil || binding.InvocationRef == "" {
		return scope{}, nil, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return current, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return current, nil, errs.ErrUnavailable
	}
	return current, tx, nil
}

// currentSSHExecutionCandidateTx reuses canonical RuntimeRevision, grant, Gate and operation checks before validating the originating actor's current use ACL; it never leases or changes the principal.
// currentSSHExecutionCandidateTx использует canonical RuntimeRevision, grant, Gate и operation checks до originating actor current use ACL; lease и изменение principal отсутствуют.
func (repository *Repository) currentSSHExecutionCandidateTx(ctx context.Context, tx pgx.Tx, current scope, ref string) (sshExecutionCandidate, []byte, error) {
	var candidate sshExecutionCandidate
	err := scanIntegrationInvocationCandidate(tx.QueryRow(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState,
		current.organizationID, 1, "integration-gateway", "MANAGED_MCP", false, ref, false, false, true), &candidate.integrationInvocationCandidate)
	if errors.Is(err, pgx.ErrNoRows) {
		return candidate, nil, errs.ErrForbidden
	}
	if err != nil {
		return candidate, nil, errs.ErrUnavailable
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, candidate.connectionRef, candidate.definitionKey, candidate.definitionVersion, candidate.definitionDigest)
	if err != nil {
		return candidate, nil, err
	}
	if _, err := sshHealthMode(definition); err != nil {
		return candidate, nil, err
	}
	capability, ok := definition.Capability(candidate.capabilityKey)
	if !ok || capability.Operation != candidate.operation || capability.Risk != candidate.risk || definition.ValidateInvocationApprovalPolicy(capability, candidate.approvalPolicy, candidate.boundedInput) != nil {
		return candidate, nil, errs.ErrForbidden
	}
	stored, err := canonicalSSHStoredInput(candidate.boundedInput)
	if err != nil {
		return candidate, nil, err
	}
	defer clear(stored)
	canonical, err := capability.ValidateInput(stored)
	if err != nil || sshHealthHash(canonical) != candidate.inputDigest {
		return candidate, nil, errs.ErrUnavailable
	}
	actor := current
	actor.actorRef = candidate.initiatorRef
	candidate.authorizedUntil, err = repository.requireAccessUntil(ctx, tx, actor, integrationInvocationActorPermission(definition), entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: candidate.connectionRef})
	if err != nil {
		return candidate, nil, err
	}
	if candidate.approvalPolicy == string(integrationpackage.ApprovalHumanScoped) {
		if err := lockIntegrationApprovalCallTx(ctx, tx, current.organizationID, candidate.id); err != nil {
			clear(canonical)
			return candidate, nil, err
		}
	}
	return candidate, canonical, nil
}

// sshExecutionCandidate retains the single canonical invocation selection and its original actor's current finite permission bound; it does not issue a separate claim or choose an identity.
// sshExecutionCandidate сохраняет единственную canonical invocation selection и current finite permission bound original actor; отдельный claim либо выбор identity отсутствуют.
type sshExecutionCandidate struct {
	integrationInvocationCandidate
	authorizedUntil time.Time
}

// BeginSSHExecution durably records the only original dispatch after current owner checks; duplicate or lost Begin acknowledgements cannot release a second admission.
// BeginSSHExecution устойчиво сохраняет единственный original dispatch после current owner checks; duplicate либо lost Begin acknowledgement не выдаёт вторую admission.
func (repository *Repository) BeginSSHExecution(ctx context.Context, p value.Principal, binding managedssh.Binding) (result managedssh.ExecutionAdmission, resultErr error) {
	var admission managedssh.ExecutionAdmission
	defer func() {
		if resultErr != nil {
			clear(admission.CanonicalInput)
			if admission.ReceiptSource != nil {
				clear(admission.ReceiptSource.CanonicalInput)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, err := repository.beginSSHExecutionOwner(ctx, p, binding, "platform.runtime.ssh.execution.begin")
	if err != nil {
		return admission, err
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	source, err := repository.currentSSHInvocationSourceTx(ctx, tx, current.organizationID, binding.InvocationRef)
	if err != nil {
		return admission, err
	}
	candidate, canonical, err := repository.currentSSHExecutionCandidateTx(ctx, tx, current, binding.InvocationRef)
	if err != nil {
		return admission, err
	}
	defer clear(canonical)
	var id, ref, operation, inputDigest, effectKey, node, root, project, actor string
	var raw []byte
	var created, expires time.Time
	leaseStarted := time.Now()
	err = tx.QueryRow(ctx, querySSHExecutionLease, sshExecutionArguments(current.organizationID, binding)).Scan(&id, &ref, &operation, &raw, &inputDigest, &effectKey, &node, &root, &project, &actor, &expires, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return admission, errs.ErrForbidden
	}
	if err != nil {
		return admission, errs.ErrUnavailable
	}
	if candidate.id != id || candidate.operation != operation || inputDigest != binding.InputDigest {
		return admission, errs.ErrForbidden
	}
	creditDeadline := leaseStarted.Add(expires.Sub(created))
	if !candidate.authorizedUntil.IsZero() {
		if candidate.authorizedUntil.Before(creditDeadline) {
			creditDeadline = candidate.authorizedUntil
		}
		if candidate.authorizedUntil.Before(expires) {
			expires = candidate.authorizedUntil.UTC().Truncate(time.Microsecond)
		}
	}
	if !time.Now().Before(creditDeadline) {
		return managedssh.ExecutionAdmission{}, errs.ErrForbidden
	}
	admission = managedssh.ExecutionAdmission{InvocationRef: ref, Operation: operation, InputDigest: inputDigest, SubjectDigest: source.subjectDigest,
		Identity: source.material, Mode: source.mode, CanonicalInput: append([]byte(nil), canonical...), ExpiresAt: expires}
	admission.AttemptRef, err = newRef("sxe")
	if err != nil {
		return managedssh.ExecutionAdmission{}, errs.ErrUnavailable
	}
	decoded, err := managedssh.DecodeOperationInput(operation, canonical)
	if err != nil {
		return managedssh.ExecutionAdmission{}, errs.ErrForbidden
	}
	defer decoded.Clear()
	if decoded.Kind() == managedssh.ReceiptInput {
		original, err := decoded.ReceiptReference()
		if err != nil {
			return managedssh.ExecutionAdmission{}, errs.ErrInvalid
		}
		actor := current
		actor.actorRef = candidate.initiatorRef
		receipt, until, err := repository.readSSHExecutionReceiptSourceTx(ctx, tx, actor, id, source, original)
		if err != nil {
			return managedssh.ExecutionAdmission{}, err
		}
		admission.ReceiptSource = &receipt
		if !until.IsZero() {
			if until.Before(creditDeadline) {
				creditDeadline = until
			}
			if until.Before(expires) {
				expires = until.UTC().Truncate(time.Microsecond)
				admission.ExpiresAt = expires
			}
		}
	}
	snapshot, err := json.Marshal(admission)
	if err != nil {
		return managedssh.ExecutionAdmission{}, errs.ErrUnavailable
	}
	defer clear(snapshot)
	admission.SnapshotDigest = sshHealthHash(snapshot)
	if admission.Validate(created) != nil {
		return managedssh.ExecutionAdmission{}, errs.ErrUnavailable
	}
	args := sshExecutionArguments(current.organizationID, binding)
	args["ref"], args["invocation_id"], args["identity_id"] = admission.AttemptRef, id, source.identityID
	args["snapshot"], args["snapshot_digest"], args["created_at"], args["expires_at"] = string(snapshot), admission.SnapshotDigest, created, expires
	delete(args, "invocation_ref")
	tag, err := tx.Exec(ctx, querySSHExecutionInsert, args)
	if err != nil {
		return managedssh.ExecutionAdmission{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if tag.RowsAffected() != 1 {
		return managedssh.ExecutionAdmission{}, errs.ErrConflict
	}
	if !time.Now().Before(creditDeadline) {
		return managedssh.ExecutionAdmission{}, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return managedssh.ExecutionAdmission{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if !time.Now().Before(creditDeadline) {
		return managedssh.ExecutionAdmission{}, errs.ErrForbidden
	}
	return admission, nil
}

// readSSHExecutionRowTx returns the original tenant-bound receipt only when every opaque lease coordinate matches its immutable intent, including after authority expires.
// readSSHExecutionRowTx возвращает original tenant-bound receipt лишь при совпадении всех opaque lease coordinates с immutable intent, в том числе после expiry authority.
func readSSHExecutionRowTx(ctx context.Context, tx pgx.Tx, organization string, binding managedssh.Binding) (sshExecutionRow, error) {
	var row sshExecutionRow
	var result string
	err := tx.QueryRow(ctx, querySSHExecutionRead, pgx.StrictNamedArgs{"organization_id": organization, "invocation_ref": binding.InvocationRef}).Scan(&row.id, &row.receipt.AttemptRef, &row.receipt.SnapshotDigest, &row.snapshot, &row.outerState,
		&row.instance, &row.lease, &row.fenceDigest, &row.generation, &row.inputDigest, &row.receipt.Recorded, &row.receipt.Observation.Outcome, &result, &row.receipt.Observation.ResultDigest, &row.receipt.Applied, &row.receipt.State,
		&row.invocationID, &row.nodeRef, &row.rootID, &row.projectID, &row.created, &row.expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	if row.instance != binding.WorkloadInstance || row.lease != binding.LeaseRef || row.fenceDigest != sshHealthHash([]byte(binding.Fence)) || row.generation != binding.Generation || row.inputDigest != binding.InputDigest {
		return row, errs.ErrForbidden
	}
	row.receipt.Observation.Result = []byte(result)
	row.receipt.OwnerState = row.outerState
	if !row.receipt.Recorded {
		row.receipt.State = "UNREPORTED"
	}
	return row, nil
}

// GetSSHExecution exposes metadata only, so an unreported intent or historical receipt cannot become a new dispatch grant or output-publication channel.
// GetSSHExecution выдаёт лишь metadata; unreported intent либо historical receipt не превращается в новый dispatch grant или output-publication channel.
func (repository *Repository) GetSSHExecution(ctx context.Context, p value.Principal, binding managedssh.Binding) (result entity.SSHExecutionReceipt, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, err := repository.beginSSHExecutionOwner(ctx, p, binding, "platform.runtime.ssh.execution.get")
	if err != nil {
		return entity.SSHExecutionReceipt{}, err
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	var connection string
	if err := tx.QueryRow(ctx, querySSHInvocationLocator, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef}).Scan(&connection); errors.Is(err, pgx.ErrNoRows) {
		return entity.SSHExecutionReceipt{}, errs.ErrNotFound
	} else if err != nil {
		return entity.SSHExecutionReceipt{}, errs.ErrUnavailable
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, connection); err != nil {
		return entity.SSHExecutionReceipt{}, err
	}
	row, err := readSSHExecutionRowTx(ctx, tx, current.organizationID, binding)
	if err != nil {
		return entity.SSHExecutionReceipt{}, err
	}
	var creditDeadline time.Time
	if !row.receipt.Recorded {
		admission, err := decodeSSHExecutionSnapshot(row)
		if err != nil {
			return entity.SSHExecutionReceipt{}, err
		}
		eligible, deadline, err := repository.sshExecutionEligibleTx(ctx, tx, current, binding, row, admission)
		clear(admission.CanonicalInput)
		if admission.ReceiptSource != nil {
			clear(admission.ReceiptSource.CanonicalInput)
		}
		if err != nil {
			return entity.SSHExecutionReceipt{}, err
		}
		if eligible {
			creditDeadline = deadline
			row.receipt.AuthorizedUntil = row.expires
		}
	}
	clear(row.snapshot)
	clear(row.receipt.Observation.Result)
	row.receipt.Observation.Result = nil
	if !creditDeadline.IsZero() && !time.Now().Before(creditDeadline) {
		row.receipt.AuthorizedUntil = time.Time{}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.SSHExecutionReceipt{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if !creditDeadline.IsZero() && !time.Now().Before(creditDeadline) {
		row.receipt.AuthorizedUntil = time.Time{}
	}
	return row.receipt, nil
}

// sameSSHExecutionObservation compares the complete immutable result before replay; changed bytes cannot borrow an old receipt or publish a new result.
// sameSSHExecutionObservation сравнивает complete immutable result до replay; changed bytes не используют old receipt и не выдают новый result.
func sameSSHExecutionObservation(a, b managedssh.ExecutionObservation) bool {
	return a.Outcome == b.Outcome && a.ResultDigest == b.ResultDigest && bytes.Equal(a.Result, b.Result)
}

// decodeSSHExecutionSnapshot verifies exact owner-produced bytes and historical admission structure against the original database clock; this structural read does not renew present authority.
// decodeSSHExecutionSnapshot проверяет exact owner-produced bytes и historical admission structure по original database clock; structural read не продлевает present authority.
func decodeSSHExecutionSnapshot(row sshExecutionRow) (managedssh.ExecutionAdmission, error) {
	var admission managedssh.ExecutionAdmission
	decoder := json.NewDecoder(bytes.NewReader(row.snapshot))
	decoder.DisallowUnknownFields()
	if len(row.snapshot) > 131072 || sshHealthHash(row.snapshot) != row.receipt.SnapshotDigest || decoder.Decode(&admission) != nil {
		return admission, errs.ErrUnavailable
	}
	canonical, err := json.Marshal(admission)
	defer clear(canonical)
	if err != nil || !bytes.Equal(canonical, row.snapshot) || admission.SnapshotDigest != "" || admission.AttemptRef != row.receipt.AttemptRef || !admission.ExpiresAt.Equal(row.expires) {
		return admission, errs.ErrUnavailable
	}
	admission.SnapshotDigest = row.receipt.SnapshotDigest
	if admission.Validate(row.created) != nil {
		return managedssh.ExecutionAdmission{}, errs.ErrUnavailable
	}
	return admission, nil
}

// CompleteSSHExecution preserves a joined private observation independently of publication, rolls back infrastructure failures, and uses the canonical receipt/event/continuation persistence only for the exact still-current original lease.
// CompleteSSHExecution сохраняет joined private observation отдельно от publication, откатывает infrastructure failures и использует canonical receipt/event/continuation persistence лишь для exact still-current original lease.
func (repository *Repository) CompleteSSHExecution(ctx context.Context, p value.Principal, input command.SSHExecutionComplete) (result entity.SSHExecutionReceipt, resultErr error) {
	var empty entity.SSHExecutionReceipt
	if !managedssh.OwnerReference(input.AttemptRef) || !managedssh.Digest(input.SnapshotDigest) || input.Observation.Validate() != nil {
		return empty, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, tx, err := repository.beginSSHExecutionOwner(ctx, p, input.Binding, "platform.runtime.ssh.execution.complete")
	if err != nil {
		return empty, err
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	var connection string
	if err := tx.QueryRow(ctx, querySSHInvocationLocator, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": input.Binding.InvocationRef}).Scan(&connection); errors.Is(err, pgx.ErrNoRows) {
		return empty, errs.ErrNotFound
	} else if err != nil {
		return empty, errs.ErrUnavailable
	}
	if _, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, connection); err != nil {
		return empty, err
	}
	row, err := readSSHExecutionRowTx(ctx, tx, current.organizationID, input.Binding)
	if err != nil {
		return empty, err
	}
	defer clear(row.snapshot)
	defer clear(row.receipt.Observation.Result)
	if row.receipt.AttemptRef != input.AttemptRef || row.receipt.SnapshotDigest != input.SnapshotDigest {
		return empty, errs.ErrForbidden
	}
	if row.receipt.Recorded {
		if !sameSSHExecutionObservation(row.receipt.Observation, input.Observation) {
			return empty, errs.ErrConflict
		}
		row.receipt.Observation.Result = nil
		if err := tx.Commit(ctx); err != nil {
			return empty, serializableTransactionError(err, errs.ErrUnavailable)
		}
		return row.receipt, nil
	}
	admission, err := decodeSSHExecutionSnapshot(row)
	if err != nil {
		return empty, err
	}
	defer clear(admission.CanonicalInput)
	if admission.ReceiptSource != nil {
		defer clear(admission.ReceiptSource.CanonicalInput)
	}
	if admission.InvocationRef != input.Binding.InvocationRef || admission.InputDigest != input.Binding.InputDigest {
		return empty, errs.ErrForbidden
	}
	eligible, _, err := repository.sshExecutionEligibleTx(ctx, tx, current, input.Binding, row, admission)
	if err != nil {
		return empty, err
	}
	receipt := entity.SSHExecutionReceipt{AttemptRef: input.AttemptRef, SnapshotDigest: input.SnapshotDigest, Recorded: true, State: "DISCARDED", OwnerState: row.outerState,
		Observation: managedssh.ExecutionObservation{Outcome: input.Observation.Outcome, ResultDigest: input.Observation.ResultDigest}}
	if eligible {
		applied, err := repository.publishSSHExecutionTx(ctx, tx, current, input, row)
		if err != nil {
			return empty, err
		}
		receipt.Applied = applied
		if applied {
			switch input.Observation.Outcome {
			case managedssh.ExecutionSucceeded:
				receipt.State = "SUCCEEDED"
			case managedssh.ExecutionFailed:
				receipt.State = "FAILED"
			case managedssh.ExecutionUnknown:
				receipt.State = "UNKNOWN_OUTCOME"
			}
		}
	}
	if receipt.Applied {
		receipt.OwnerState = receipt.State
	}
	args := pgx.StrictNamedArgs{"intent_id": row.id, "organization_id": current.organizationID, "outcome": string(input.Observation.Outcome),
		"result": string(input.Observation.Result), "result_digest": input.Observation.ResultDigest, "applied": receipt.Applied, "state": receipt.State}
	if _, err := tx.Exec(ctx, querySSHExecutionObserve, args); err != nil {
		return empty, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if receipt.Applied {
		receipt.Observation.Result = append([]byte(nil), input.Observation.Result...)
	}
	return receipt, nil
}

// publishSSHExecutionTx atomically persists canonical outer completion and its continuation/event; a late deadline rolls back partial effect receipts, while every dependency or SQL failure aborts the owner transaction.
// publishSSHExecutionTx атомарно сохраняет canonical outer completion и continuation/event; late deadline откатывает partial effect receipts, а dependency либо SQL failure прерывает owner transaction.
func (repository *Repository) publishSSHExecutionTx(ctx context.Context, tx pgx.Tx, current scope, input command.SSHExecutionComplete, row sshExecutionRow) (applied bool, resultErr error) {
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, savepoint, &resultErr)
	state, reason := "SUCCEEDED", ""
	switch input.Observation.Outcome {
	case managedssh.ExecutionFailed:
		state, reason = "FAILED", "INTEGRATION_ACTION_FAILED"
	case managedssh.ExecutionUnknown:
		state, reason = "UNKNOWN_OUTCOME", "INTEGRATION_OUTCOME_UNKNOWN"
	}
	var receiptID string
	if input.Observation.Outcome == managedssh.ExecutionSucceeded {
		var effectKey string
		var id, ref, operation, digest, node, root, project, actor string
		var raw []byte
		var expires, created time.Time
		err := savepoint.QueryRow(ctx, querySSHExecutionLease, sshExecutionArguments(current.organizationID, input.Binding)).Scan(&id, &ref, &operation, &raw, &digest, &effectKey, &node, &root, &project, &actor, &expires, &created)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, errs.ErrUnavailable
		}
		if id != row.invocationID {
			return false, errs.ErrUnavailable
		}
		receiptRef, err := newRef("erc")
		if err != nil {
			return false, errs.ErrUnavailable
		}
		var storedRef string
		if err := savepoint.QueryRow(ctx, queryWorkersCompleteintegrationinvocationInsertEffectReceipt, receiptRef, current.organizationID, row.invocationID, effectKey, input.Binding.InputDigest,
			input.AttemptRef, input.Observation.ResultDigest, string(input.Observation.Result)).Scan(&receiptID, &storedRef); err != nil {
			return false, serializableTransactionError(err, errs.ErrUnavailable)
		}
	}
	args := sshExecutionArguments(current.organizationID, input.Binding)
	delete(args, "invocation_ref")
	args["invocation_id"], args["attempt_ref"], args["state"], args["result"], args["reason"], args["receipt_id"] = row.invocationID, input.AttemptRef, state, string(input.Observation.Result), reason, receiptID
	tag, err := savepoint.Exec(ctx, querySSHExecutionFinish, args)
	if err != nil {
		return false, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if state != "UNKNOWN_OUTCOME" {
		if _, err := repository.scheduleIntegrationContinuation(ctx, savepoint, current, row.invocationID, row.projectID); err != nil {
			return false, err
		}
	}
	if _, err := repository.emitIntegrationActionEvent(ctx, savepoint, current, row.projectID, row.rootID, input.Binding.InvocationRef, row.nodeRef, state); err != nil {
		return false, err
	}
	if err := savepoint.Commit(ctx); err != nil {
		return false, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return true, nil
}

// sshExecutionEligibleTx rechecks the same private source, complete canonical owner predicates and original lease immediately before publication or native pre-dispatch; it never extends the intent deadline.
// sshExecutionEligibleTx повторно проверяет тот же private source, complete canonical owner predicates и original lease перед publication либо native pre-dispatch; intent deadline не продлевается.
func (repository *Repository) sshExecutionEligibleTx(ctx context.Context, tx pgx.Tx, current scope, binding managedssh.Binding, row sshExecutionRow, admission managedssh.ExecutionAdmission) (bool, time.Time, error) {
	source, err := repository.currentSSHInvocationSourceTx(ctx, tx, current.organizationID, binding.InvocationRef)
	if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	candidate, canonical, err := repository.currentSSHExecutionCandidateTx(ctx, tx, current, binding.InvocationRef)
	defer clear(canonical)
	if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}
	if !bytes.Equal(canonical, admission.CanonicalInput) || source.material != admission.Identity || source.subjectDigest != admission.SubjectDigest || source.mode != admission.Mode {
		return false, time.Time{}, nil
	}
	if !candidate.authorizedUntil.IsZero() && candidate.authorizedUntil.Before(row.expires) {
		return false, time.Time{}, nil
	}
	if admission.ReceiptSource != nil {
		actor := current
		actor.actorRef = candidate.initiatorRef
		original, until, err := repository.readSSHExecutionReceiptSourceTx(ctx, tx, actor, row.invocationID, source, admission.ReceiptSource.InvocationRef)
		defer clear(original.CanonicalInput)
		if errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound) {
			return false, time.Time{}, nil
		}
		if err != nil {
			return false, time.Time{}, err
		}
		if original.InvocationRef != admission.ReceiptSource.InvocationRef || original.Operation != admission.ReceiptSource.Operation ||
			!bytes.Equal(original.CanonicalInput, admission.ReceiptSource.CanonicalInput) || !until.IsZero() && until.Before(row.expires) {
			return false, time.Time{}, nil
		}
		if !until.IsZero() && (candidate.authorizedUntil.IsZero() || until.Before(candidate.authorizedUntil)) {
			candidate.authorizedUntil = until
		}
	}
	var id, ref, operation, digest, effect, node, root, project, actor string
	var raw []byte
	var expires, now time.Time
	leaseStarted := time.Now()
	err = tx.QueryRow(ctx, querySSHExecutionLease, sshExecutionArguments(current.organizationID, binding)).Scan(&id, &ref, &operation, &raw, &digest, &effect, &node, &root, &project, &actor, &expires, &now)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, errs.ErrUnavailable
	}
	deadline := leaseStarted.Add(row.expires.Sub(now))
	if !candidate.authorizedUntil.IsZero() && candidate.authorizedUntil.Before(deadline) {
		deadline = candidate.authorizedUntil
	}
	return id == row.invocationID && ref == binding.InvocationRef && operation == admission.Operation && digest == binding.InputDigest && row.expires.After(now) &&
		!row.expires.After(expires) && time.Now().Before(deadline), deadline, nil
}

// readSSHExecutionReceiptSourceTx reuses the exact original-owner/project lookup and bounds its immutable recipe by the original actor's current run.view permission; it cannot redirect a recovery invocation or renew an old deadline.
// readSSHExecutionReceiptSourceTx использует exact original-owner/project lookup и ограничивает immutable recipe текущим run.view original actor; перенаправление recovery invocation либо продление старого deadline отсутствуют.
func (repository *Repository) readSSHExecutionReceiptSourceTx(ctx context.Context, tx pgx.Tx, actor scope, readerID string, source sshInvocationSource, original string) (managedssh.ManagedReceiptSource, time.Time, error) {
	var operation, digest, runRef string
	var raw []byte
	err := tx.QueryRow(ctx, querySSHExecutionReceiptSource, pgx.StrictNamedArgs{"organization_id": actor.organizationID, "invocation_ref": original,
		"reader_invocation_id": readerID, "identity_id": source.identityID, "subject_digest": source.subjectDigest}).Scan(&operation, &raw, &digest, &runRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return managedssh.ManagedReceiptSource{}, time.Time{}, errs.ErrForbidden
	}
	if err != nil {
		return managedssh.ManagedReceiptSource{}, time.Time{}, errs.ErrUnavailable
	}
	defer clear(raw)
	canonical, err := canonicalSSHStoredInput(raw)
	if err != nil || sshHealthHash(canonical) != digest {
		clear(canonical)
		return managedssh.ManagedReceiptSource{}, time.Time{}, errs.ErrUnavailable
	}
	until, err := repository.requireAccessUntil(ctx, tx, actor, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: runRef})
	if err != nil {
		clear(canonical)
		return managedssh.ManagedReceiptSource{}, time.Time{}, err
	}
	return managedssh.ManagedReceiptSource{InvocationRef: original, Operation: operation, CanonicalInput: canonical}, until, nil
}

// canonicalSSHStoredInput decodes only an owner-stored JSONB object and preserves exact scalar numbers while removing PostgreSQL presentation spacing. The public/helper parser remains strict and the caller must recheck the original immutable input digest.
// canonicalSSHStoredInput декодирует лишь owner-stored JSONB object и сохраняет exact scalar numbers при удалении PostgreSQL presentation spacing. Public/helper parser остаётся strict; caller повторяет original immutable input digest.
func canonicalSSHStoredInput(raw []byte) ([]byte, error) {
	if len(raw) < 2 || len(raw) > 128<<10 {
		return nil, errs.ErrUnavailable
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, errs.ErrUnavailable
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	return canonical, nil
}
