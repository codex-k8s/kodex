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

// queryRuntimeDeadlineStart starts the execution clock once for exact organization run/root IDs
// with a pinned workflow, including only eligible workflow steps for non-root runs. It does not
// itself claim work or validate authority; caller transaction owns the execution transition.
// queryRuntimeDeadlineStart однократно начинает часы точных run и root организации с закреплённым
// workflow, для дочернего run требуя соответствующий шаг. Не захватывает работу и не проверяет
// полномочия; переход исполнения принадлежит транзакции вызывающего.
//
//go:embed sql/runtime_deadline__start.sql
var queryRuntimeDeadlineStart string

// queryRuntimeDeadlineRead walks same-organization/project parent lineage up to depth 128,
// includes its roots and reads started pinned workflow clocks in run-ref order with database-time
// expiry. It does not lock or authorize the graph; the caller validates the resulting deadline
// contract.
// queryRuntimeDeadlineRead обходит родителей той же организации и проекта до глубины 128,
// добавляет корни и читает начатые часы закреплённых workflow по ссылке run с истечением по БД.
// Граф не блокирует и не авторизует; итоговый контракт проверяет вызывающий.
//
//go:embed sql/runtime_deadline__read.sql
var queryRuntimeDeadlineRead string

// queryRuntimeDeadlineExpired locks a bounded ordered page of expired run rows with active roots
// and an active run or open gate, skipping locked rows. It returns one representative execution
// node per run; the caller terminalizes the complete root graph, not just this candidate row.
// queryRuntimeDeadlineExpired блокирует ограниченную страницу просроченных run с активным корнем и
// активным run либо открытым gate, пропуская заблокированные строки. Возвращает один execution
// node на run; вызывающий завершает весь корневой граф, не только кандидата.
//
//go:embed sql/runtime_deadline__expired.sql
var queryRuntimeDeadlineExpired string

// queryRuntimeDeadlineOwner resolves a representative execution node and root/project identity for
// the exact expired organization/run ref with an active root. It selects no session/turn identity
// and does not itself lock, fail or authorize the graph.
// queryRuntimeDeadlineOwner разрешает representative execution node, корень и проект точного
// просроченного run организации с активным корнем. Не выбирает session и turn, сам не блокирует,
// не завершает и не авторизует граф.
//
//go:embed sql/runtime_deadline__owner.sql
var queryRuntimeDeadlineOwner string

// queryRuntimeDeadlineGate resolves the run behind an exact organization/gate ref without a
// gate-state or actor predicate. The preceding command authority/replay path and later deadline
// read establish eligibility.
// queryRuntimeDeadlineGate разрешает run точных организации и gate без условий состояния gate и
// субъекта. Допустимость устанавливают предшествующие authority и replay команды и последующее
// чтение deadline.
//
//go:embed sql/runtime_deadline__gate.sql
var queryRuntimeDeadlineGate string

// runtimeTimeoutSummary names the existing localized timeout summary used for graph failure and
// its command receipt. It contains no diagnostic payload and does not by itself prove terminal
// commit.
// runtimeTimeoutSummary задаёт прежнее локализованное описание тайм-аута для завершения графа и
// квитанции команды. Не содержит диагностических данных и само не подтверждает итоговый commit.
const runtimeTimeoutSummary = "i18n:RUNTIME_TIMEOUT"

// readRuntimeExecutionDeadline collects started lineage clocks with UTC timestamps, computes the
// minimum deadline and ORs database expiry flags. Query/scan/iteration failure discards all data
// as Unavailable, no clocks returns nil/false, and invalid contract is Conflict. It does not
// decide authority, mutate state or commit.
// readRuntimeExecutionDeadline собирает начатые часы происхождения со временем UTC, вычисляет
// минимальный срок и объединяет флаги истечения БД. Ошибки чтения дают пустой результат и
// Unavailable, отсутствие часов — nil/false, неверный контракт — Conflict. Полномочия, состояние и
// commit не меняет.
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

// reconcileExecutionDeadlines buffers and closes a bounded locked candidate page before failing
// each distinct root graph once through the canonical timeout path. Errors discard the returned
// list while earlier changes remain inside the caller transaction pending rollback; no commit or
// external cancellation is performed here.
// reconcileExecutionDeadlines буферизует и закрывает ограниченную страницу заблокированных
// кандидатов, затем один раз завершает каждый отдельный корневой граф штатным timeout путём. При
// ошибке список отбрасывается, прежние изменения остаются в транзакции до rollback; commit и
// внешняя отмена здесь отсутствуют.
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

// executionDeadlineCommand runs after fresh command authority/replay lookup and before its effect,
// extracting exact lease pins from supported nominal payloads or a gate-owned run. Expired
// completion first preserves validated usage/archive metadata, then the earliest deadline owner is
// failed through the canonical full-graph path and a handled expiry receipt is returned. No expiry
// or unsupported payload is unhandled; caller must commit expiry state even when receipt mapping
// rejects the late effect.
// executionDeadlineCommand работает после fresh authority и replay команды, до её эффекта:
// извлекает точные lease pins поддерживаемых типов либо run из gate. Поздний completion сначала
// сохраняет проверенные usage и archive metadata, затем владелец самого раннего deadline
// завершается общим full-graph путём и возвращается обработанная expiry квитанция. Без истечения
// либо для неподдерживаемого payload путь не обработан; вызывающий фиксирует expiry даже при
// отказе позднему эффекту.
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

// executionDeadlineReceiptError maps a committed deadline-expired result marker to Forbidden and
// all other results to nil. It changes neither the stored receipt nor graph state and must not
// cause the enclosing committed expiry to be treated as rolled back.
// executionDeadlineReceiptError отображает маркер истёкшего deadline в результате в Forbidden,
// остальные результаты — в nil. Не меняет квитанцию и граф; не позволяет считать зафиксированное
// истечение откатившимся.
func executionDeadlineReceiptError(result command.Result) error {
	if result.ExecutionDeadlineExpired {
		return errs.ErrForbidden
	}
	return nil
}

// validateRuntimeCompletion checks usage and success/error-code consistency, then requires
// coherent archive identity, a 64-character digest and bounded positive source size if any archive
// field is present. Digest alphabet is not validated here; it performs no storage read, artifact
// publication or authority check.
// validateRuntimeCompletion проверяет usage и согласованность успеха с safe error code, затем при
// любом archive поле требует корректную archive identity, хеш длины 64 и ограниченный
// положительный размер. Алфавит хеша здесь не проверяется; чтения storage, публикации и
// авторизации нет.
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

// recordExpiredRuntimeCompletion validates a late completion and records its exact turn usage plus
// root totals before optional session-storage/archive pins with 30-day retention and exactly one
// affected row. Missing turn is Conflict; SQL/storage failures are Unavailable. It does not
// publish artifacts or change the timeout verdict; atomic commit and current lease ownership
// belong to the caller.
// recordExpiredRuntimeCompletion проверяет поздний completion и записывает usage точного turn и
// суммы корня, затем необязательные session-storage и archive pins с 30-дневным retention и ровно
// одной строкой. Отсутствие turn — Conflict, ошибки SQL и storage — Unavailable. Артефакты не
// публикует и timeout verdict не меняет; atomic commit и владение lease принадлежат вызывающему.
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
