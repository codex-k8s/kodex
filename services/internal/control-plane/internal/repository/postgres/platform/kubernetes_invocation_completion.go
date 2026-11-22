package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesInvocationCompletionRead reads only exact original claim metadata, including terminal ACKs.
// queryKubernetesInvocationCompletionRead читает лишь metadata exact original claim, включая terminal ACK.
//
//go:embed sql/kubernetes_invocation__completion_read.sql
var queryKubernetesInvocationCompletionRead string

// queryKubernetesInvocationCompletionChildren reads immutable child receipts in their owner-assigned order.
// queryKubernetesInvocationCompletionChildren читает immutable child receipts в owner-assigned порядке.
//
//go:embed sql/kubernetes_invocation__completion_children.sql
var queryKubernetesInvocationCompletionChildren string

// queryKubernetesInvocationCompletionLock serializes finalization against child completion on the locked invocation.
// queryKubernetesInvocationCompletionLock сериализует finalization и child completion при locked invocation.
//
//go:embed sql/kubernetes_invocation__completion_lock.sql
var queryKubernetesInvocationCompletionLock string

// queryKubernetesInvocationContinuationGate selects a server-owned approved child decision for the exact completed invocation.
// queryKubernetesInvocationContinuationGate выбирает серверное решение по дочернему действию точного завершённого вызова.
//
//go:embed sql/kubernetes_invocation__continuation_gate.sql
var queryKubernetesInvocationContinuationGate string

// kubernetesInvocationCompletionRecord retains private lifecycle references separately from its safe receipt.
// kubernetesInvocationCompletionRecord хранит private lifecycle refs отдельно от safe receipt.
type kubernetesInvocationCompletionRecord struct {
	receipt                                                                   entity.ManagedKubernetesInvocationReceipt
	id, summary, receiptSummary, runID, rootID, projectID, nodeRef, effectKey string
	expires                                                                   *time.Time
	now                                                                       time.Time
	openGate                                                                  bool
}

// kubernetesInvocationChildResult contains stored effect truth, never a caller's proposed aggregate outcome.
// kubernetesInvocationChildResult содержит stored effect truth, не caller proposed aggregate outcome.
type kubernetesInvocationChildResult struct {
	ordinal                                           int
	operation, state, outcome, result, digest, reason string
	applied, current, openGate, hasOutput             bool
	streamRef                                         string
	capturePartial                                    bool
	captureReady, capturePending                      bool
	capturedArtifacts                                 []entity.ManagedKubernetesCapturedArtifact
	streamState                                       enum.ManagedKubernetesStreamState
	terminal                                          *managedkubernetes.TerminalReceipt
}

// kubernetesInvocationGatewayCaller validates the protected method and original invocation binding variant.
// kubernetesInvocationGatewayCaller проверяет protected method и original invocation binding variant.
func kubernetesInvocationGatewayCaller(p value.Principal, binding value.ManagedKubernetesBinding, permission string) error {
	if p.CallerWorkload != "integration-gateway" || p.Permission != permission || p.ProjectRef != "" || binding.Validate() != nil || binding.InvocationRef == "" || binding.ConnectionTestRef != "" {
		return errs.ErrForbidden
	}
	return nil
}

// kubernetesInvocationCompletionArgs hashes the raw fence locally and never exposes it in metadata readback.
// kubernetesInvocationCompletionArgs хеширует raw fence локально, не раскрывая его в metadata readback.
func kubernetesInvocationCompletionArgs(org string, binding value.ManagedKubernetesBinding) (pgx.StrictNamedArgs, error) {
	digest, err := managedkubernetes.BindingDigest(binding)
	if err != nil {
		return nil, errs.ErrInvalid
	}
	return pgx.StrictNamedArgs{"organization_id": org, "invocation_ref": binding.InvocationRef, "input_digest": binding.InputDigest, "binding_digest": digest, "generation": binding.Generation, "lease_ref": binding.LeaseRef, "fence_digest": kubernetesHealthFenceDigest(binding.Fence), "instance": binding.WorkloadInstance}, nil
}

// readKubernetesInvocationCompletion checks exact durable lineage and stored receipt integrity without result delivery.
// A rejected invocation is represented as cancelled; neither variant claims a recorded business completion.
// readKubernetesInvocationCompletion проверяет exact durable lineage и stored receipt integrity без result delivery.
// Rejected invocation представлен cancelled; ни один вариант не утверждает recorded business completion.
func readKubernetesInvocationCompletion(row pgx.Row, binding value.ManagedKubernetesBinding) (record kubernetesInvocationCompletionRecord, err error) {
	r := &record.receipt
	var state string
	err = row.Scan(&record.id, &state, &record.summary, &r.PlanRef, &r.PlanDigest, &r.SnapshotDigest, &r.ReceiptRef, &r.ResultDigest, &record.receiptSummary, &record.runID, &record.rootID, &record.projectID, &record.nodeRef, &record.effectKey, &record.expires, &record.now, &record.openGate)
	if errors.Is(err, pgx.ErrNoRows) {
		return record, errs.ErrForbidden
	}
	if err != nil {
		return record, errs.ErrUnavailable
	}
	if state == "REJECTED" {
		state = "CANCELLED"
	}
	r.InvocationRef, r.Generation, r.InputDigest = binding.InvocationRef, binding.Generation, binding.InputDigest
	r.BindingDigest, err = managedkubernetes.BindingDigest(binding)
	if err != nil {
		return record, errs.ErrInvalid
	}
	r.State = enum.ManagedKubernetesInvocationState(state)
	r.CompletionPendingReason = enum.KubernetesCompletionPendingNone
	if !r.State.Known() {
		return record, errs.ErrUnavailable
	}
	if r.PlanRef != "" {
		r.PlanRevision = 1
	}
	r.CompletionRecorded = r.ReceiptRef != ""
	if r.CompletionRecorded {
		digest := sha256.Sum256([]byte(record.summary))
		if state == "RUNNING" || record.summary != record.receiptSummary || hex.EncodeToString(digest[:]) != r.ResultDigest {
			return record, errs.ErrUnavailable
		}
	} else if r.ResultDigest != "" || state == "SUCCEEDED" {
		return record, errs.ErrUnavailable
	}
	return record, nil
}

// GetManagedKubernetesInvocation returns exact original-attempt metadata after closure without issuing any credit.
// It does not read credentials, replay a provider request, or expose stored result bytes through an organization locator.
// GetManagedKubernetesInvocation выдаёт metadata exact original attempt после closure без выдачи credit.
// Не читает credentials, не повторяет provider request, не раскрывает result bytes через organization locator.
func (repository *Repository) GetManagedKubernetesInvocation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding) (entity.ManagedKubernetesInvocationReceipt, error) {
	if err := kubernetesInvocationGatewayCaller(p, binding, "platform.runtime.kubernetes.read"); err != nil {
		return entity.ManagedKubernetesInvocationReceipt{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return entity.ManagedKubernetesInvocationReceipt{}, err
	}
	args, err := kubernetesInvocationCompletionArgs(current.organizationID, binding)
	if err != nil {
		return entity.ManagedKubernetesInvocationReceipt{}, err
	}
	record, err := readKubernetesInvocationCompletion(repository.pool.QueryRow(ctx, queryKubernetesInvocationCompletionRead, args), binding)
	if err != nil {
		return record.receipt, err
	}
	if record.receipt.State == enum.KubernetesInvocationRunning && record.receipt.PlanRef != "" {
		captures, readErr := readKubernetesInvocationCaptures(ctx, repository.pool, current.organizationID, binding)
		if readErr != nil {
			return entity.ManagedKubernetesInvocationReceipt{}, readErr
		}
		for _, capture := range captures {
			if capture.pending {
				record.receipt.CompletionPendingReason = enum.KubernetesCompletionCapturePending
				break
			}
		}
	}
	return record.receipt, nil
}

// aggregateKubernetesInvocationResults derives the useful bounded summary solely from accepted child facts.
// Pending means only an unattempted suffix after settled successes; ambiguity never grants permission to advance.
// aggregateKubernetesInvocationResults выводит useful bounded summary лишь из accepted child facts.
// Pending означает только unattempted suffix после settled successes; ambiguity не разрешает advance.
func aggregateKubernetesInvocationResults(children []kubernetesInvocationChildResult) (state enum.ManagedKubernetesInvocationState, summary string, err error) {
	state = enum.KubernetesInvocationSucceeded
	pending := false
	captureWaiting := false
	results := make([]struct {
		Operation string          `json:"operation"`
		Result    json.RawMessage `json:"result"`
	}, 0, len(children))
	for index, child := range children {
		if child.ordinal != index || child.openGate || !child.current {
			return "", "", errs.ErrForbidden
		}
		if child.state == "" {
			pending = true
			continue
		}
		if pending {
			return "", "", errs.ErrConflict
		}
		if child.result == "" || !child.applied {
			state = enum.KubernetesInvocationUnknown
			continue
		}
		if managedkubernetes.ValidateEffectResult([]byte(child.result), child.digest, child.outcome, child.reason) != nil || child.state != child.outcome {
			return "", "", errs.ErrUnavailable
		}
		streamComplete := true
		if descriptor, known := integrationpackage.KubernetesOperationByKey(child.operation); known && descriptor.Execution == integrationpackage.KubernetesStream {
			if child.streamRef == "" || child.terminal == nil || managedkubernetes.ValidateStreamEffectResult(*child.terminal, []byte(child.result), child.digest, child.outcome, child.reason) != nil {
				return "", "", errs.ErrUnavailable
			}
			streamComplete = !child.capturePartial && child.streamState == enum.KubernetesStreamCompleted && child.terminal.LocalState == "COMPLETED" && child.terminal.TransportJoined
			if !streamComplete {
				state = enum.KubernetesInvocationUnknown
			}
		}
		// A useful capture is required for complete success. Incomplete local delivery preserves
		// UNKNOWN even with a known remote exit; it never promotes a terminal stream from exit0 alone.
		// Useful capture обязателен для complete success. Incomplete local delivery сохраняет
		// UNKNOWN даже при known remote exit; один exit0 не повышает terminal stream до success.
		if child.hasOutput && child.outcome == "SUCCEEDED" && streamComplete && !child.captureReady {
			if child.streamRef == "" || !child.capturePending {
				return "", "", errs.ErrUnavailable
			}
			captureWaiting = true
		}
		switch child.outcome {
		case "SUCCEEDED":
		case "FAILED":
			if state != enum.KubernetesInvocationUnknown {
				state = enum.KubernetesInvocationFailed
			}
		case "UNKNOWN":
			state = enum.KubernetesInvocationUnknown
		default:
			return "", "", errs.ErrUnavailable
		}
		resultJSON := json.RawMessage(child.result)
		if child.captureReady {
			if child.streamRef == "" || len(child.capturedArtifacts) == 0 {
				return "", "", errs.ErrUnavailable
			}
			raw, marshalErr := json.Marshal(struct {
				Result    json.RawMessage                            `json:"result"`
				Artifacts []entity.ManagedKubernetesCapturedArtifact `json:"artifacts"`
			}{resultJSON, child.capturedArtifacts})
			if marshalErr != nil || len(raw) > 64<<10 {
				return "", "", errs.ErrUnavailable
			}
			resultJSON = raw
		}
		results = append(results, struct {
			Operation string          `json:"operation"`
			Result    json.RawMessage `json:"result"`
		}{child.operation, resultJSON})
	}
	if (pending || captureWaiting) && state == enum.KubernetesInvocationSucceeded {
		return enum.KubernetesInvocationRunning, "", nil
	}
	if len(children) == 0 {
		return enum.KubernetesInvocationFailed, "{}", nil
	}
	if len(children) == 1 && len(results) == 1 {
		return state, string(results[0].Result), nil
	}
	raw, marshalErr := json.Marshal(struct {
		Children any `json:"children"`
	}{results})
	if marshalErr != nil || len(raw) > 64<<10 {
		return "", "", errs.ErrUnavailable
	}
	var safe map[string]json.RawMessage
	if internalrpcauth.DecodeStrictJSON(raw, &safe) != nil {
		return "", "", errs.ErrUnavailable
	}
	return state, string(raw), nil
}

// CompleteManagedKubernetesInvocation settles the existing owner graph without caller-supplied outcome or bytes.
// Lost ACKs replay stored metadata. New completion requires a fresh original owner and current child grants;
// all writes, dependency closure, continuation and event commit together, preserving ambiguous remote facts.
// CompleteManagedKubernetesInvocation завершает existing owner graph без caller outcome либо bytes.
// Lost ACK повторяет stored metadata. Новый completion требует fresh original owner и current child grants;
// writes, dependency closure, continuation и event фиксируются вместе, сохраняя remote uncertainty.
func (repository *Repository) CompleteManagedKubernetesInvocation(ctx context.Context, p value.Principal, binding value.ManagedKubernetesBinding, input command.ManagedKubernetesCompleteInvocation) (result entity.ManagedKubernetesInvocationReceipt, resultErr error) {
	if err := kubernetesInvocationGatewayCaller(p, binding, "platform.runtime.kubernetes.complete"); err != nil {
		return result, err
	}
	if !input.ValidPlanPins() {
		return result, errs.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	args, err := kubernetesInvocationCompletionArgs(current.organizationID, binding)
	if err != nil {
		return result, err
	}
	// The first read permits exact terminal ACK recovery even after revocation, with no result authority.
	// Первый read допускает exact terminal ACK recovery даже после revoke, без result authority.
	before, err := readKubernetesInvocationCompletion(repository.pool.QueryRow(ctx, queryKubernetesInvocationCompletionRead, args), binding)
	if err != nil {
		return result, err
	}
	if before.receipt.PlanRef != input.PlanRef || before.receipt.PlanRevision != input.PlanRevision || before.receipt.PlanDigest != input.PlanDigest {
		return result, errs.ErrConflict
	}
	if before.receipt.State != enum.KubernetesInvocationRunning {
		return before.receipt, nil
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	owner, err := repository.readKubernetesOperationOwnerTx(ctx, tx, current, binding)
	if err != nil {
		if !accessInfrastructureFailure(err) && (errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrNotFound)) {
			closed, readErr := readKubernetesInvocationCompletion(repository.pool.QueryRow(ctx, queryKubernetesInvocationCompletionRead, args), binding)
			if readErr == nil && closed.receipt.State != enum.KubernetesInvocationRunning && closed.receipt.PlanRef == input.PlanRef && closed.receipt.PlanRevision == input.PlanRevision && closed.receipt.PlanDigest == input.PlanDigest {
				return closed.receipt, nil
			}
			if readErr != nil && !errors.Is(readErr, errs.ErrForbidden) {
				return result, readErr
			}
		}
		return result, err
	}
	record, err := readKubernetesInvocationCompletion(tx.QueryRow(ctx, queryKubernetesInvocationCompletionRead, args), binding)
	if err != nil {
		return result, err
	}
	if record.receipt.PlanRef != input.PlanRef || record.receipt.PlanRevision != input.PlanRevision || record.receipt.PlanDigest != input.PlanDigest {
		return result, errs.ErrConflict
	}
	if record.receipt.State != enum.KubernetesInvocationRunning {
		return record.receipt, nil
	}
	if input.PlanRef == "" && record.openGate {
		return result, errs.ErrConflict
	}
	snapshot, err := kubernetesOperationSnapshotDigest(owner, binding)
	if err != nil {
		return result, err
	}
	if input.PlanRef != "" && snapshot != record.receipt.SnapshotDigest {
		return result, errs.ErrForbidden
	}
	var originalChildren []entity.ManagedKubernetesChildEffect
	if input.PlanRef != "" {
		planArgs := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": record.id, "binding_digest": record.receipt.BindingDigest, "snapshot_digest": snapshot}
		var planID, ref, digest, proposal string
		var count int
		var deadline time.Time
		if err = tx.QueryRow(ctx, queryKubernetesPlanDetail, planArgs).Scan(&planID, &ref, &digest, &proposal, &count, &deadline); err != nil {
			return result, errs.ErrUnavailable
		}
		original, readErr := readKubernetesPlanChildren(ctx, tx, current.organizationID, planID, count, owner.selected.source.registry.Nodes)
		if readErr != nil {
			return result, readErr
		}
		originalChildren = original
		expected, hashErr := kubernetesPlanDigest(record.receipt.BindingDigest, snapshot, proposal, original)
		if hashErr != nil || ref != input.PlanRef || digest != input.PlanDigest || expected != digest {
			return result, errs.ErrUnavailable
		}
	}
	childArgs := pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": record.id, "binding_digest": record.receipt.BindingDigest}
	if _, err := tx.Exec(ctx, queryKubernetesInvocationCompletionLock, childArgs); err != nil {
		return result, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryKubernetesInvocationCompletionChildren, childArgs)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var children []kubernetesInvocationChildResult
	for rows.Next() {
		var child kubernetesInvocationChildResult
		if err = rows.Scan(&child.ordinal, &child.operation, &child.state, &child.outcome, &child.applied, &child.result, &child.digest, &child.reason, &child.current, &child.openGate, &child.hasOutput, &child.streamRef, &child.capturePartial); err != nil {
			rows.Close()
			return result, errs.ErrUnavailable
		}
		children = append(children, child)
	}
	rows.Close()
	if rows.Err() != nil {
		return result, errs.ErrUnavailable
	}
	for index := range children {
		child := &children[index]
		if child.streamRef == "" || child.result == "" || !child.applied {
			continue
		}
		stored, readErr := readKubernetesStreamRecordTx(ctx, tx, current, binding, child.streamRef)
		if readErr != nil {
			return result, readErr
		}
		history, readErr := stored.history(binding, child.streamRef)
		if readErr != nil {
			return result, readErr
		}
		child.streamState, child.terminal = stored.state, history.Terminal
	}
	captures, captureErr := readKubernetesInvocationCaptures(ctx, tx, current.organizationID, binding)
	if captureErr != nil {
		return result, captureErr
	}
	for index := range children {
		child := &children[index]
		if capture, exists := captures[child.streamRef]; exists {
			child.captureReady, child.capturePending = capture.ready, capture.pending
			child.capturePartial = child.capturePartial || capture.partial
			child.capturedArtifacts = capture.record.receipt.Artifacts
		}
	}
	state, summary, err := aggregateKubernetesInvocationResults(children)
	preflightReason := ""
	if input.PreflightRejection != "" {
		if !validKubernetesPortForwardPreflight(owner.candidate.operation, originalChildren, children) {
			return result, errs.ErrConflict
		}
		safe, marshalErr := json.Marshal(struct {
			Operation  string `json:"operation"`
			ReasonCode string `json:"reasonCode"`
		}{"kubernetes.port-forward", managedkubernetes.ReasonPortForwardProfileUnsupported})
		if marshalErr != nil {
			return result, errs.ErrUnavailable
		}
		state, summary, err = enum.KubernetesInvocationFailed, string(safe), nil
		preflightReason = managedkubernetes.ReasonPortForwardProfileUnsupported
	}
	if err != nil {
		return result, err
	}
	if state == enum.KubernetesInvocationRunning {
		for _, child := range children {
			if child.capturePending {
				record.receipt.CompletionPendingReason = enum.KubernetesCompletionCapturePending
				break
			}
		}
		if tx.Commit(ctx) != nil {
			return result, errs.ErrUnavailable
		}
		return record.receipt, nil
	}
	digest := sha256.Sum256([]byte(summary))
	resultDigest := hex.EncodeToString(digest[:])
	receiptRef, err := newRef("erc")
	if err != nil {
		return result, errs.ErrUnavailable
	}
	providerRef := input.PlanRef
	if providerRef == "" {
		providerRef = binding.InvocationRef
	}
	var receiptID, storedReceiptRef string
	err = tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocationInsertEffectReceipt, receiptRef, current.organizationID, record.id, record.effectKey, binding.InputDigest, providerRef, resultDigest, summary).Scan(&receiptID, &storedReceiptRef)
	if err != nil {
		return result, mapWriteError(err)
	}
	reason := ""
	if state == enum.KubernetesInvocationFailed {
		reason = "INTEGRATION_REQUEST_REJECTED"
		if preflightReason != "" {
			reason = preflightReason
		}
	}
	if state == enum.KubernetesInvocationUnknown {
		reason = "INTEGRATION_OUTCOME_UNKNOWN"
	}
	if _, err = tx.Exec(ctx, queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode, record.id, string(state), summary, reason, receiptID); err != nil {
		return result, mapWriteError(err)
	}
	if state == enum.KubernetesInvocationSucceeded || state == enum.KubernetesInvocationFailed {
		var decisionGateRef string
		if err = tx.QueryRow(ctx, queryKubernetesInvocationContinuationGate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": record.id}).Scan(&decisionGateRef); err != nil {
			return result, errs.ErrUnavailable
		}
		var decisionGateRefs []string
		if decisionGateRef != "" {
			decisionGateRefs = []string{decisionGateRef}
		}
		if _, err = repository.scheduleIntegrationContinuation(ctx, tx, current, record.id, record.projectID, decisionGateRefs...); err != nil {
			return result, err
		}
	}
	if _, err = repository.emitIntegrationActionEvent(ctx, tx, current, record.projectID, record.rootID, binding.InvocationRef, record.nodeRef, string(state)); err != nil {
		return result, err
	}
	result = record.receipt
	result.State = state
	result.CompletionRecorded = true
	result.ReceiptRef = storedReceiptRef
	result.ResultDigest = resultDigest
	if tx.Commit(ctx) != nil {
		return entity.ManagedKubernetesInvocationReceipt{}, errs.ErrUnavailable
	}
	return result, nil
}
