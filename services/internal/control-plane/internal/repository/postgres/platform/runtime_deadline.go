package platform

import (
	"context"
	_ "embed"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_deadline__start.sql
var queryRuntimeDeadlineStart string

//go:embed sql/runtime_deadline__read.sql
var queryRuntimeDeadlineRead string

//go:embed sql/runtime_deadline__expired.sql
var queryRuntimeDeadlineExpired string

//go:embed sql/runtime_deadline__owner.sql
var queryRuntimeDeadlineOwner string

//go:embed sql/runtime_deadline__gate.sql
var queryRuntimeDeadlineGate string

const runtimeTimeoutSummary = "i18n:RUNTIME_TIMEOUT"

func readRuntimeExecutionDeadline(ctx context.Context, tx pgx.Tx, organizationID, runID string) (*runtimecontract.RuntimeExecutionDeadline, bool, error) {
	rows, err := tx.Query(ctx, queryRuntimeDeadlineRead, pgx.StrictNamedArgs{"organization_id": organizationID, "run_id": runID})
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	defer rows.Close()
	deadline := &runtimecontract.RuntimeExecutionDeadline{Policy: runtimecontract.WorkflowExecutionDeadlinePolicy}
	expired := false
	for rows.Next() {
		var clock runtimecontract.RuntimeExecutionClock
		var elapsed bool
		if rows.Scan(&clock.RunRef, &clock.WorkflowVersionRef, &clock.WorkflowVersionDigest, &clock.StepKey,
			&clock.TimeoutSeconds, &clock.StartedAt, &clock.DeadlineAt, &elapsed) != nil {
			return nil, false, errs.ErrUnavailable
		}
		clock.StartedAt, clock.DeadlineAt = clock.StartedAt.UTC(), clock.DeadlineAt.UTC()
		deadline.Clocks = append(deadline.Clocks, clock)
		expired = expired || elapsed
		if deadline.EffectiveDeadlineAt.IsZero() || clock.DeadlineAt.Before(deadline.EffectiveDeadlineAt) {
			deadline.EffectiveDeadlineAt = clock.DeadlineAt
		}
	}
	if rows.Err() != nil {
		return nil, false, errs.ErrUnavailable
	}
	if len(deadline.Clocks) == 0 {
		return nil, false, nil
	}
	if deadline.Validate() != nil {
		return nil, false, errs.ErrConflict
	}
	return deadline, expired, nil
}

func (repository *Repository) reconcileExecutionDeadlines(ctx context.Context, tx pgx.Tx, current scope, input command.Command, limit int32) ([]claimableExecution, error) {
	rows, err := tx.Query(ctx, queryRuntimeDeadlineExpired, pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var candidates []claimableExecution
	for rows.Next() {
		var item claimableExecution
		if rows.Scan(&item.nodeID, &item.nodeRef, &item.runID, &item.runRef, &item.rootRunID, &item.projectID, &item.projectRef, &item.sessionID, &item.sessionRef, &item.stableKey) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	seen := make(map[string]bool)
	var failed []claimableExecution
	for _, item := range candidates {
		if seen[item.rootRunID] {
			continue
		}
		if err := repository.failRuntimeGraph(ctx, tx, current, input, item, "RUNTIME_TIMEOUT", runtimeTimeoutSummary); err != nil {
			return nil, err
		}
		seen[item.rootRunID] = true
		failed = append(failed, item)
	}
	return failed, nil
}

// Этот путь запускается после fresh authority и replay lookup, но до effect.
// Expiry должен быть committed, даже когда поздний effect закрыто отклонён.
func (repository *Repository) executionDeadlineCommand(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, bool, error) {
	var pin command.LeaseInput
	runID := ""
	var actualLease map[string]any
	switch payload := input.Payload.(type) {
	case command.LeaseInput:
		pin = payload
	case command.CompleteExecutionInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.DelegateInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.LaunchWorkflowInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.RunToolCallInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.ProviderCredentialRefreshInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.ProposeAssistantPlanInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.ProposeAssistantMetadataInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.ProposeRunMetadataInput:
		pin = command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}
	case command.GateResolutionInput:
		if tx.QueryRow(ctx, queryRuntimeDeadlineGate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "gate_ref": payload.GateRef}).Scan(&runID) != nil {
			return commandOutcome{}, false, errs.ErrUnavailable
		}
	default:
		return commandOutcome{}, false, nil
	}
	if pin.LeaseRef == "" && runID == "" {
		return commandOutcome{}, false, nil
	}
	if runID == "" {
		lease, err := repository.lease(ctx, tx, current, pin, true)
		if err != nil {
			return commandOutcome{}, false, err
		}
		actualLease = lease
		runID = stringMap(lease, "runID")
	}
	deadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, runID)
	if err != nil || !expired {
		return commandOutcome{}, false, err
	}
	if payload, ok := input.Payload.(command.CompleteExecutionInput); ok {
		if err := recordExpiredRuntimeCompletion(ctx, tx, current, actualLease, payload); err != nil {
			return commandOutcome{}, false, err
		}
	}
	var candidate claimableExecution
	clockRef := ""
	for _, clock := range deadline.Clocks {
		if clock.DeadlineAt.Equal(deadline.EffectiveDeadlineAt) {
			clockRef = clock.RunRef
			break
		}
	}
	if tx.QueryRow(ctx, queryRuntimeDeadlineOwner, pgx.StrictNamedArgs{"organization_id": current.organizationID, "run_ref": clockRef}).Scan(&candidate.nodeID, &candidate.nodeRef, &candidate.runID, &candidate.runRef, &candidate.rootRunID, &candidate.projectID, &candidate.projectRef) != nil {
		return commandOutcome{}, false, errs.ErrUnavailable
	}
	if err := repository.failRuntimeGraph(ctx, tx, current, input, candidate, "RUNTIME_TIMEOUT", runtimeTimeoutSummary); err != nil {
		return commandOutcome{}, false, err
	}
	return commandOutcome{result: command.Result{ExecutionDeadlineExpired: true}, resourceKind: "RUN", resourceRef: candidate.runRef, projectID: candidate.projectID, projectRef: candidate.projectRef, summary: runtimeTimeoutSummary, runtimeGraphChanged: true}, true, nil
}

func executionDeadlineReceiptError(result command.Result) error {
	if result.ExecutionDeadlineExpired {
		return errs.ErrForbidden
	}
	return nil
}

func validateRuntimeCompletion(payload command.CompleteExecutionInput) error {
	if !payload.Usage.Valid() || payload.Success && payload.SafeErrorCode != "" || !payload.Success && !runtimeSafeErrorCode(payload.SafeErrorCode) {
		return errs.ErrInvalid
	}
	hasArchive := payload.CodexSessionID != "" || payload.ArchiveRelativePath != "" || payload.ArchiveSHA256 != "" || payload.ArchiveSizeBytes != 0
	if hasArchive && (runtimecontract.ValidateCodexArchiveIdentity(payload.CodexSessionID, payload.ArchiveRelativePath) != nil || len(payload.ArchiveSHA256) != 64 || payload.ArchiveSizeBytes < 1 || payload.ArchiveSizeBytes > runtimecontract.MaximumSessionSourceBytes) {
		return errs.ErrInvalid
	}
	return nil
}

// Поздний успех не публикует артефакты и не меняет verdict, но расход уже
// исполненного provider и подтверждённые archive pins нельзя потерять.
func recordExpiredRuntimeCompletion(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, payload command.CompleteExecutionInput) error {
	if err := validateRuntimeCompletion(payload); err != nil {
		return err
	}
	usage, err := json.Marshal(payload.Usage)
	if err != nil {
		return errs.ErrInvalid
	}
	turnRef := stringMap(lease, "turnRef")
	if turnRef == "" {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRunUsage, lease["runID"], turnRef, usage); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateRootUsage, lease["rootRunID"]); err != nil {
		return errs.ErrUnavailable
	}
	if payload.CodexSessionID != "" {
		var sessionID, targetType string
		if tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectRunsId, lease["runID"]).Scan(&sessionID, &targetType) != nil {
			return errs.ErrUnavailable
		}
		stored, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpsertSessionStorage, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "session_id": sessionID, "runtime_revision_id": lease["runtimeRevisionID"],
			"codex_session_id": payload.CodexSessionID, "source_relative_path": payload.ArchiveRelativePath, "source_sha256": payload.ArchiveSHA256,
			"source_size_bytes": payload.ArchiveSizeBytes, "retention_seconds": int64(30 * 24 * 60 * 60),
		})
		if err != nil || stored.RowsAffected() != 1 {
			return errs.ErrUnavailable
		}
	}
	return nil
}
