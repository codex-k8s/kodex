package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// modelMeasurementRecordedSummary is the safe localized receipt description, never raw provider content.
// modelMeasurementRecordedSummary задаёт безопасное локализуемое описание квитанции без исходного provider content.
const modelMeasurementRecordedSummary = "i18n:MODEL_MEASUREMENT_RECORDED"

// reportModelMeasurement appends one exact observation receipt after the caller resolves the active owner lease.
// The outer semantic receipt handles replay before this append, avoiding a lifetime event scan for every step.
// The observation never changes phase or terminal state.
// reportModelMeasurement добавляет точную квитанцию после разрешения caller действующей owner lease.
// Внешняя semantic receipt обрабатывает replay до append, исключая сканирование всей истории на каждом шаге.
// Измерение не меняет фазу или terminal state.
func (repository *Repository) reportModelMeasurement(ctx context.Context, tx pgx.Tx, scope scope, payload command.LeaseInput, lease map[string]any) (commandOutcome, error) {
	measurement := payload.ModelMeasurement
	if measurement == nil || measurement.Validate() != nil || payload.Financial != nil || payload.Message != nil || payload.Progress != "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	event, err := repository.emitRunEventWithBinding(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), stringMap(lease, "nodeRef"), "TURN_PROGRESS", stringMap(lease, "nodeRef"), "", "", "", nil, nil, modelMeasurementRecordedSummary, "RUNNING", "RUNNING", "", measurement)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: modelMeasurementRecordedSummary}, nil
}

// bindModelMeasurementReceipt resolves current execution authority before an immutable receipt can be replayed.
// The server derives the key from the lease generation and observation ref, never transport correlation or caller identity.
// bindModelMeasurementReceipt разрешает текущие полномочия execution до повтора immutable квитанции.
// Сервер выводит ключ из поколения lease и ref измерения, а не transport correlation или caller identity.
func (repository *Repository) bindModelMeasurementReceipt(ctx context.Context, tx pgx.Tx, current scope, input *command.Command) error {
	if input.Kind != command.ReportExecutionProgress {
		return nil
	}
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok || payload.ModelMeasurement == nil {
		return nil
	}
	if payload.ModelMeasurement.Validate() != nil || payload.Message != nil || payload.Financial != nil || payload.Progress != "" {
		return errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, current, payload, true)
	if err != nil {
		return err
	}
	var rootID, rootState string
	if tx.QueryRow(ctx, queryRuntimeCompleteexecutionLockRootRun, lease["rootRunID"]).Scan(&rootID, &rootState) != nil {
		return errs.ErrUnavailable
	}
	if rootID != stringMap(lease, "rootRunID") || contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, rootState) {
		return errs.ErrForbidden
	}
	if _, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, stringMap(lease, "runID")); err != nil {
		return err
	} else if expired {
		return errs.ErrForbidden
	}
	input.Mutation.IdempotencyKey = digestBytes(asJSON([]any{"model-measurement-callback-v1", stringMap(lease, "leaseID"), payload.Generation, payload.ModelMeasurement.Ref}))
	input.Mutation.IntentDigest = digestBytes(asJSON(payload.ModelMeasurement))
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, current.organizationID, current.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// ConfigureModelMeasurementObserver installs a synchronous non-I/O post-commit hook before any workers start.
// ConfigureModelMeasurementObserver задаёт синхронный post-commit hook без I/O до запуска workers.
func (repository *Repository) ConfigureModelMeasurementObserver(observe func(runtimecontract.ModelMeasurement)) {
	repository.modelMeasurementObserver = observe
}
