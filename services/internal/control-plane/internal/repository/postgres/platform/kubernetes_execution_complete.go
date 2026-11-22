package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesCompletionRecord locks the original effect independently of fresh output eligibility.
// queryKubernetesCompletionRecord блокирует original effect отдельно от fresh output eligibility.
//
//go:embed sql/kubernetes_execution__completion_record.sql
var queryKubernetesCompletionRecord string

// queryKubernetesCompletionInsert stores one immutable accepted or explicitly discarded receipt.
// queryKubernetesCompletionInsert хранит immutable accepted либо explicitly discarded receipt.
//
//go:embed sql/kubernetes_execution__completion_insert.sql
var queryKubernetesCompletionInsert string

// queryKubernetesEffectComplete closes the exact single attempt without scheduling another request.
// queryKubernetesEffectComplete закрывает exact single attempt без scheduling нового request.
//
//go:embed sql/kubernetes_execution__effect_complete.sql
var queryKubernetesEffectComplete string

// queryKubernetesExecutionReadiness checks the actual owner tables/function under the runtime identity.
// queryKubernetesExecutionReadiness проверяет actual owner tables/function через runtime identity.
//
//go:embed sql/kubernetes_execution__readiness.sql
var queryKubernetesExecutionReadiness string

// kubernetesCompletionRecord retains stored facts independently of a caller's current authority.
// kubernetesCompletionRecord хранит stored facts отдельно от current caller authority.
type kubernetesCompletionRecord struct {
	effectID, state, operation, childID, snapshot               string
	deadline, now                                               time.Time
	childCurrent                                                bool
	streamRef, streamState                                      string
	terminalJSON                                                []byte
	resultJSON, resultDigest, submissionDigest, outcome, reason string
	applied                                                     bool
}

// CompleteManagedKubernetesEffect records a bounded original attempt receipt once. Fresh owner and
// child eligibility admit output; authority loss discards bytes while retaining the submission hash.
// Infrastructure failures roll back all writes, and historical receipts never rewrite accepted truth.
// CompleteManagedKubernetesEffect однократно фиксирует bounded original attempt receipt. Fresh owner
// и child eligibility допускают output; authority loss удаляет bytes, сохраняя submission hash.
// Infrastructure failure откатывает writes; historical receipts не переписывают accepted truth.
func (repository *Repository) CompleteManagedKubernetesEffect(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCompleteEffect) (result entity.ManagedKubernetesEffectReceipt, resultErr error) {
	if err := kubernetesOperationBrokerCaller(principal, binding, "platform.runtime.kubernetes.effect.complete"); err != nil {
		return result, err
	}
	if managedkubernetes.ValidateEffectResult(input.ResultJSON, input.ResultDigest, string(input.Outcome), input.ReasonCode) != nil {
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
	owner, authorityErr := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if authorityErr != nil && (accessInfrastructureFailure(authorityErr) || !errors.Is(authorityErr, errs.ErrForbidden) && !errors.Is(authorityErr, errs.ErrNotFound)) {
		return result, authorityErr
	}
	bindingDigest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return result, errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": binding.InvocationRef, "invocation_input_digest": binding.InputDigest, "binding_digest": bindingDigest, "effect_ref": input.EffectRef, "effect_generation": input.EffectGeneration, "child_input_digest": input.ChildInputDigest}
	var record kubernetesCompletionRecord
	err = tx.QueryRow(ctx, queryKubernetesCompletionRecord, args).Scan(&record.effectID, &record.state, &record.operation, &record.childID, &record.snapshot, &record.deadline, &record.childCurrent, &record.streamRef, &record.streamState, &record.terminalJSON, &record.resultJSON, &record.resultDigest, &record.submissionDigest, &record.outcome, &record.reason, &record.applied, &record.now)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrForbidden
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if input.StreamRef != record.streamRef {
		return result, errs.ErrConflict
	}
	if record.streamRef != "" {
		var receipt managedkubernetes.TerminalReceipt
		if len(record.terminalJSON) == 0 || internalrpcauth.DecodeStrictJSON(record.terminalJSON, &receipt) != nil || managedkubernetes.ValidateStreamEffectResult(receipt, input.ResultJSON, input.ResultDigest, string(input.Outcome), input.ReasonCode) != nil {
			return result, errs.ErrConflict
		}
		if record.streamState == "ACTIVE" || record.streamState == "PREPARED" {
			return result, errs.ErrConflict
		}
	}
	authorized := authorityErr == nil && record.childCurrent && record.deadline.After(record.now)
	if authorized {
		snapshot, snapshotErr := kubernetesOperationSnapshotDigest(owner, binding)
		if snapshotErr != nil {
			return result, snapshotErr
		}
		if snapshot != record.snapshot {
			authorized = false
		}
	}
	until := owner.lease.ExpiresAt
	if limit := record.now.Add(30 * time.Second); limit.Before(until) {
		until = limit
	}
	if record.deadline.Before(until) {
		until = record.deadline
	}
	authorized = authorized && until.After(record.now)
	if record.submissionDigest != "" {
		if record.submissionDigest != input.ResultDigest || record.outcome != string(input.Outcome) || record.reason != input.ReasonCode {
			return result, errs.ErrConflict
		}
		if record.applied {
			if !authorized {
				return result, errs.ErrForbidden
			}
			if record.resultDigest != input.ResultDigest || record.resultJSON != string(input.ResultJSON) || record.state != record.outcome {
				return result, errs.ErrUnavailable
			}
			result = entity.ManagedKubernetesEffectReceipt{State: record.state, ResultDigest: record.resultDigest, SubmissionDigest: record.submissionDigest, Applied: true, AuthorizedUntil: until}
		} else {
			result = entity.ManagedKubernetesEffectReceipt{State: "CANCELLED", ResultDigest: record.resultDigest, SubmissionDigest: record.submissionDigest}
		}
		if tx.Commit(ctx) != nil {
			return entity.ManagedKubernetesEffectReceipt{}, errs.ErrUnavailable
		}
		if result.Applied {
			if creditErr := owner.creditError(ctx, result.AuthorizedUntil); creditErr != nil {
				return entity.ManagedKubernetesEffectReceipt{}, creditErr
			}
		}
		return result, nil
	}
	if record.state != "ATTEMPTING" {
		authorized = false
	}
	storedJSON, storedDigest, state := string(input.ResultJSON), input.ResultDigest, string(input.Outcome)
	if !authorized {
		storedJSON = "{}"
		digest := sha256.Sum256([]byte(storedJSON))
		storedDigest = hex.EncodeToString(digest[:])
		state = "CANCELLED"
	}
	tag, err := tx.Exec(ctx, queryKubernetesCompletionInsert, pgx.StrictNamedArgs{"effect_id": record.effectID, "organization_id": current.organizationID, "outcome": string(input.Outcome), "result_json": storedJSON, "result_digest": storedDigest, "submission_digest": input.ResultDigest, "reason_code": input.ReasonCode, "applied": authorized})
	if err != nil {
		return result, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	tag, err = tx.Exec(ctx, queryKubernetesEffectComplete, pgx.StrictNamedArgs{"organization_id": current.organizationID, "effect_id": record.effectID, "state": state})
	if err != nil {
		return result, mapWriteError(err)
	}
	if tag.RowsAffected() != 1 {
		return result, errs.ErrConflict
	}
	result = entity.ManagedKubernetesEffectReceipt{State: state, ResultDigest: storedDigest, SubmissionDigest: input.ResultDigest, Applied: authorized}
	if authorized {
		result.AuthorizedUntil = until
	}
	if tx.Commit(ctx) != nil {
		return entity.ManagedKubernetesEffectReceipt{}, errs.ErrUnavailable
	}
	if result.Applied {
		if creditErr := owner.creditError(ctx, result.AuthorizedUntil); creditErr != nil {
			return entity.ManagedKubernetesEffectReceipt{}, creditErr
		}
	}
	return result, nil
}

// CheckManagedKubernetesOwnerReadiness verifies the registered caller and actual owner storage path.
// It proves neither target credentials/RBAC nor broker handler readiness; those are separate probes.
// CheckManagedKubernetesOwnerReadiness проверяет registered caller и actual owner storage path.
// Не доказывает target credentials/RBAC либо broker handler readiness; это отдельные probes.
func (repository *Repository) CheckManagedKubernetesOwnerReadiness(ctx context.Context, principal value.Principal) (bool, error) {
	if principal.CallerWorkload != "secret-broker" || principal.Permission != "platform.runtime.kubernetes.readiness" || principal.ProjectRef != "" {
		return false, errs.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := repository.resolveScope(ctx, principal); err != nil {
		return false, err
	}
	var ready bool
	if repository.pool.QueryRow(ctx, queryKubernetesExecutionReadiness).Scan(&ready) != nil {
		return false, errs.ErrUnavailable
	}
	return ready, nil
}
