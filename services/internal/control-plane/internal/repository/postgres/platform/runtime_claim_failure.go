package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// runtimeCandidateEligibilityDiagnosticMessage fixes the eligibility-rejection log message; variable reason/class fields are separately bounded and no candidate input is interpolated.
// runtimeCandidateEligibilityDiagnosticMessage закрепляет сообщение журнала об отказе доступности кандидата; переменные reason/class отдельно ограничены, вход кандидата в текст не подставляется.
const runtimeCandidateEligibilityDiagnosticMessage = "runtime candidate eligibility rejected"

// runtimeEligibilityReasonAttribute names the structured log field for the allowlisted producer reason, not a raw error string.
// runtimeEligibilityReasonAttribute задаёт имя поля структурированного журнала для разрешённой причины producer, а не сырой ошибки.
const runtimeEligibilityReasonAttribute = "safe_reason"

// runtimeEligibilityUnknownDiagnostic is the shared fallback for unsupported eligibility reason/class projections; it is neither a terminal run state nor a domain error.
// runtimeEligibilityUnknownDiagnostic задаёт общий запасной результат для неподдержанных проекций причины/класса доступности; это не итоговое состояние запуска и не доменная ошибка.
const runtimeEligibilityUnknownDiagnostic = "UNKNOWN"

const (
	// runtimeReasonRuntimeContext marks failure while materializing the candidate runtime context before a claim is accepted.
	// runtimeReasonRuntimeContext обозначает отказ материализации runtime-контекста кандидата до принятия захвата.
	runtimeReasonRuntimeContext = "RUNTIME_CONTEXT"
	// runtimeReasonSessionResume marks failure to prepare the candidate session-resume context independently of continuation rendering.
	// runtimeReasonSessionResume обозначает отказ подготовки контекста возобновления сессии кандидата отдельно от формирования продолжения.
	runtimeReasonSessionResume = "SESSION_RESUME"
	// runtimeReasonContinuationPreviousRead marks failure to read the preceding immutable runtime snapshot; missing initial history is handled separately and is not automatically this error.
	// runtimeReasonContinuationPreviousRead обозначает отказ чтения предыдущего неизменяемого runtime snapshot; отсутствие начальной истории обрабатывается отдельно и само по себе этой ошибкой не является.
	runtimeReasonContinuationPreviousRead = "CONTINUATION_PREVIOUS_READ"
	// runtimeReasonContinuationPreviousJson marks invalid JSON in the preceding stored runtime snapshot without retaining its bytes in diagnostics.
	// runtimeReasonContinuationPreviousJson обозначает неверный JSON предыдущего сохранённого runtime snapshot без сохранения его байтов в диагностике.
	runtimeReasonContinuationPreviousJson = "CONTINUATION_PREVIOUS_JSON"
	// runtimeReasonContinuationPreviousDependencies marks failure to derive comparable dependency components from the preceding runtime context.
	// runtimeReasonContinuationPreviousDependencies обозначает отказ получения сравнимых компонентов зависимостей из предыдущего runtime-контекста.
	runtimeReasonContinuationPreviousDependencies = "CONTINUATION_PREVIOUS_DEPENDENCIES"
	// runtimeReasonContinuationCurrentDependencies marks failure to derive comparable dependency components from the new runtime context.
	// runtimeReasonContinuationCurrentDependencies обозначает отказ получения сравнимых компонентов зависимостей из нового runtime-контекста.
	runtimeReasonContinuationCurrentDependencies = "CONTINUATION_CURRENT_DEPENDENCIES"
	// runtimeReasonContinuationDiff marks rejection while comparing pinned previous/current runtime contexts and their exact session/turn/attempt lineage.
	// runtimeReasonContinuationDiff обозначает отказ сравнения привязанных предыдущего/текущего runtime-контекстов и их точного session/turn/attempt lineage.
	runtimeReasonContinuationDiff = "CONTINUATION_DIFF"
	// runtimeReasonContinuationSnapshot marks failure to JSON-copy the prompt materialization snapshot used for the continuation notice.
	// runtimeReasonContinuationSnapshot обозначает отказ JSON-копирования snapshot материализации промпта для уведомления продолжения.
	runtimeReasonContinuationSnapshot = "CONTINUATION_SNAPSHOT"
	// runtimeReasonContinuationTemplateRead marks infrastructure failure reading an approved continuation template; absence can use the platform default through a separate path.
	// runtimeReasonContinuationTemplateRead обозначает инфраструктурный отказ чтения утверждённого шаблона продолжения; отсутствие допускает платформенный шаблон отдельным путём.
	runtimeReasonContinuationTemplateRead = "CONTINUATION_TEMPLATE_READ"
	// runtimeReasonContinuationTemplateBytes marks a continuation template exceeding its explicit byte ceiling before materialization.
	// runtimeReasonContinuationTemplateBytes обозначает превышение явного лимита байтов шаблона продолжения до материализации.
	runtimeReasonContinuationTemplateBytes = "CONTINUATION_TEMPLATE_BYTES"
	// runtimeReasonContinuationTemplateDigest marks a stored template digest mismatch against the actual selected template bytes.
	// runtimeReasonContinuationTemplateDigest обозначает несовпадение сохранённого digest шаблона с фактическими выбранными байтами.
	runtimeReasonContinuationTemplateDigest = "CONTINUATION_TEMPLATE_DIGEST"
	// runtimeReasonContinuationDiffJson marks failure to serialize the bounded runtime-context diff for the continuation snapshot.
	// runtimeReasonContinuationDiffJson обозначает отказ сериализации ограниченного различия runtime-контекстов для snapshot продолжения.
	runtimeReasonContinuationDiffJson = "CONTINUATION_DIFF_JSON"
	// runtimeReasonContinuationBinding marks failure binding platform instructions or the current protected core projection into the continuation snapshot.
	// runtimeReasonContinuationBinding обозначает отказ привязки платформенных инструкций либо текущей защищённой core-проекции к snapshot продолжения.
	runtimeReasonContinuationBinding = "CONTINUATION_BINDING"
	// runtimeReasonContinuationWorkflowContext marks failure hydrating the authorized workflow execution context for the continuation notice.
	// runtimeReasonContinuationWorkflowContext обозначает отказ наполнения разрешённого контекста исполнения процесса для уведомления продолжения.
	runtimeReasonContinuationWorkflowContext = "CONTINUATION_WORKFLOW_CONTEXT"
	// runtimeReasonContinuationMaterialization marks an error returned while materializing the continuation template, distinct from a successful but incomplete result.
	// runtimeReasonContinuationMaterialization обозначает ошибку материализации шаблона продолжения отдельно от успешного, но неполного результата.
	runtimeReasonContinuationMaterialization = "CONTINUATION_MATERIALIZATION"
	// runtimeReasonContinuationIncomplete marks a materialization result missing required content even when template processing returned no error.
	// runtimeReasonContinuationIncomplete обозначает отсутствие обязательного содержимого в результате материализации даже при завершении обработки шаблона без ошибки.
	runtimeReasonContinuationIncomplete = "CONTINUATION_INCOMPLETE"
	// runtimeReasonContinuationNoticeBytes marks a rendered continuation notice exceeding the runtime contract byte limit.
	// runtimeReasonContinuationNoticeBytes обозначает превышение лимита байтов runtime-контракта готовым уведомлением продолжения.
	runtimeReasonContinuationNoticeBytes = "CONTINUATION_NOTICE_BYTES"
	// runtimeReasonContinuationSessionContext marks failure to append the materialized notice to the exact runtime session context.
	// runtimeReasonContinuationSessionContext обозначает отказ добавления материализованного уведомления в точный runtime-контекст сессии.
	runtimeReasonContinuationSessionContext = "CONTINUATION_SESSION_CONTEXT"
	// runtimeReasonContinuationTaskProjection marks failure binding the exact task projection to the runtime continuation before rendering.
	// runtimeReasonContinuationTaskProjection обозначает отказ привязки точной проекции задачи к runtime-продолжению до формирования текста.
	runtimeReasonContinuationTaskProjection = "CONTINUATION_TASK_PROJECTION"
)

// runtimeEligibilityDiagnosticError retains an internal producer reason and underlying domain/infrastructure error while exposing only a fixed message; reasons are allowlisted when read and the wrapper itself changes no lifecycle.
// runtimeEligibilityDiagnosticError сохраняет внутреннюю причину producer и исходную доменную/инфраструктурную ошибку, наружу выдавая фиксированное сообщение; причины ограничиваются при чтении, сама оболочка жизненный цикл не меняет.
type runtimeEligibilityDiagnosticError struct {
	reason string
	err    error
}

// Error returns a fixed eligibility-validation message without serializing the producer reason or underlying error content.
// Error возвращает фиксированное сообщение проверки доступности без сериализации причины producer и содержимого исходной ошибки.
func (err runtimeEligibilityDiagnosticError) Error() string {
	return "runtime eligibility validation failed"
}

// Unwrap preserves the underlying error for typed infrastructure/domain classification; privacy-safe logging must use the closed reason/class helpers instead of dumping this chain.
// Unwrap сохраняет исходную ошибку для типизированной инфраструктурной/доменной классификации; безопасный журнал использует закрытые reason/class helpers вместо вывода цепочки.
func (err runtimeEligibilityDiagnosticError) Unwrap() error { return err.err }

// runtimeEligibilityDiagnostic wraps an internal producer error with its reason without reclassifying it; runtimeEligibilityReason must close that reason before it reaches structured logs.
// runtimeEligibilityDiagnostic оборачивает внутреннюю ошибку producer с причиной без смены классификации; перед структурированным журналом runtimeEligibilityReason обязан ограничить причину.
func runtimeEligibilityDiagnostic(reason string, err error) error {
	return runtimeEligibilityDiagnosticError{reason: reason, err: err}
}

// runtimeEligibilityReason extracts only this typed producer diagnostic and returns a supported reason or the fixed unknown fallback; arbitrary error text is never promoted to a log reason.
// runtimeEligibilityReason извлекает лишь эту типизированную диагностику producer и возвращает поддержанную причину либо фиксированный unknown; произвольный текст ошибки причиной журнала не становится.
func runtimeEligibilityReason(err error) string {
	var diagnostic runtimeEligibilityDiagnosticError
	if !errors.As(err, &diagnostic) {
		return runtimeEligibilityUnknownDiagnostic
	}
	switch diagnostic.reason {
	case runtimeReasonRuntimeContext, runtimeReasonSessionResume, runtimeReasonContinuationPreviousRead, runtimeReasonContinuationPreviousJson, runtimeReasonContinuationPreviousDependencies, runtimeReasonContinuationCurrentDependencies,
		runtimeReasonContinuationDiff, runtimeReasonContinuationSnapshot, runtimeReasonContinuationTemplateRead, runtimeReasonContinuationTemplateBytes, runtimeReasonContinuationTemplateDigest, runtimeReasonContinuationDiffJson,
		runtimeReasonContinuationBinding, runtimeReasonContinuationWorkflowContext, runtimeReasonContinuationMaterialization, runtimeReasonContinuationIncomplete, runtimeReasonContinuationNoticeBytes, runtimeReasonContinuationSessionContext, runtimeReasonContinuationTaskProjection:
		return diagnostic.reason
	default:
		return runtimeEligibilityUnknownDiagnostic
	}
}

// queryRuntimeClaimFailGraph embeds organization/root-scoped closure of active runs, nodes, claims, turns, schedules, invocations, gates and deliveries, preserving uncertain external writes as UNKNOWN_OUTCOME and returning exact transition rows for owner events.
// queryRuntimeClaimFailGraph содержит закрытие активных запусков, узлов, захватов, ходов, расписаний, вызовов, gates и deliveries по organization/root с сохранением неопределённых внешних записей как UNKNOWN_OUTCOME и возвратом точных переходов для событий владельца.
//
//go:embed sql/runtime_claim__fail_graph.sql
var queryRuntimeClaimFailGraph string

// runtimeClaimEligibilityChanged is the localization key for a graph failed because its candidate no longer meets runtime eligibility; it is not a raw provider diagnostic.
// runtimeClaimEligibilityChanged задаёт ключ локализации отказа графа при потере доступности runtime-кандидата; сырой диагностикой провайдера не является.
const runtimeClaimEligibilityChanged = "i18n:RUNTIME_CLAIM_ELIGIBILITY_CHANGED"

// runtimeLeaseExpiredSummary is the audit summary key for a persistently expired claim lease, including cycles that acquire no new candidate.
// runtimeLeaseExpiredSummary задаёт ключ сводки аудита устойчивого истечения аренды захвата, включая циклы без нового кандидата.
const runtimeLeaseExpiredSummary = "i18n:RUNTIME_LEASE_EXPIRED"

// expireRuntimeClaimLeases expires overdue organization leases and requeues only nodes whose run and root remain active, then audits each returned transition in the caller transaction. Its changed flag reports persisted expiry even when no fresh claim is made; it does not commit itself.
// expireRuntimeClaimLeases завершает просроченные аренды организации и возвращает в очередь лишь узлы активных запуска и корня, затем пишет аудит каждого перехода в транзакции вызывающего. Changed сообщает об устойчивом истечении даже без нового захвата; метод сам не делает commit.
func (repository *Repository) expireRuntimeClaimLeases(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (bool, error) {
	rows, err := tx.Query(ctx, queryRuntimeClaimexecutionExpireStaleLeases, current.organizationID)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	// expiredLease keeps only the expired lease ref and its project/run audit coordinates after rows are fully consumed and closed.
	// expiredLease хранит лишь ref просроченной аренды и project/run координаты аудита после полного чтения и закрытия rows.
	type expiredLease struct{ ref, projectID, runRef string }
	var leases []expiredLease
	for rows.Next() {
		var lease expiredLease
		if err := rows.Scan(&lease.ref, &lease.projectID, &lease.runRef); err != nil {
			rows.Close()
			return false, errs.ErrUnavailable
		}
		leases = append(leases, lease)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, errs.ErrUnavailable
	}
	for _, lease := range leases {
		if err := repository.auditRuntimeClaimTransition(ctx, tx, current, input, lease.projectID, lease.runRef, runtimeLeaseExpiredSummary); err != nil {
			return false, err
		}
	}
	return len(leases) > 0, nil
}

// auditRuntimeClaimTransition inserts one owner claim-transition audit with optional project and exact run/correlation metadata into the supplied transaction; reference or insert failure aborts the caller path.
// auditRuntimeClaimTransition вставляет один аудит перехода захвата владельца с необязательным проектом и точными run/correlation в переданную транзакцию; отказ ссылки или вставки прерывает вызывающий путь.
func (repository *Repository) auditRuntimeClaimTransition(ctx context.Context, tx pgx.Tx, current scope, input command.Command, projectID, runRef, summary string) error {
	ref, err := newRef("aud")
	if err != nil {
		return err
	}
	var project any
	if projectID != "" {
		project = projectID
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, ref, current.organizationID, project, current.actorID, input.Mutation.Operation, "RUN", runRef, summary, input.Principal.CorrelationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// runtimeCandidateEligibilityFailure rejects PostgreSQL errors, cancellation, deadlines and Unavailable before consulting the shared closed domain class set. Only the six known owner eligibility outcomes may trigger graph failure; an infrastructure error joined with a domain sentinel must still remain nonterminal.
// runtimeCandidateEligibilityFailure отклоняет ошибки PostgreSQL, отмену, истечение срока и Unavailable перед общим закрытым классификатором домена. Только шесть известных отказов доступности владельца могут завершить граф; инфраструктурная ошибка вместе с доменной sentinel должна оставаться неитоговой.
func runtimeCandidateEligibilityFailure(err error) bool {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errs.ErrUnavailable) {
		return false
	}
	return runtimeEligibilityErrorClass(err) != runtimeEligibilityUnknownDiagnostic
}

// runtimeEligibilityErrorClass maps the six recognized domain sentinels in fixed precedence order through errors.Is and returns a closed unknown fallback. It classifies metadata only; callers must separately exclude infrastructure errors before deciding a terminal transition.
// runtimeEligibilityErrorClass сопоставляет шесть известных доменных sentinel в фиксированном порядке через errors.Is и возвращает закрытый unknown. Классифицирует лишь метаданные; перед итоговым переходом вызывающий отдельно исключает инфраструктурные ошибки.
func runtimeEligibilityErrorClass(err error) string {
	for _, candidate := range []struct {
		err   error
		class string
	}{
		{errs.ErrConflict, "CONFLICT"},
		{errs.ErrVersionMismatch, "VERSION_MISMATCH"},
		{errs.ErrNotFound, "NOT_FOUND"},
		{errs.ErrForbidden, "FORBIDDEN"},
		{errs.ErrCapabilityRequired, "CAPABILITY_REQUIRED"},
		{errs.ErrInvalid, "INVALID"},
	} {
		if errors.Is(err, candidate.err) {
			return candidate.class
		}
	}
	return runtimeEligibilityUnknownDiagnostic
}

// recordSystemAssistantTerminalTurn allocates the next assistant turn inside the supplied transaction, inserts its exact session/run content/state and advances both session and conversation metadata. Commit and terminal eligibility belong to the enclosing owner command.
// recordSystemAssistantTerminalTurn выделяет следующий ход помощника внутри переданной транзакции, вставляет точные session/run/content/state и продвигает метаданные сессии и диалога. Commit и допустимость итогового состояния принадлежат внешней команде владельца.
func (repository *Repository) recordSystemAssistantTerminalTurn(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	sessionID, runID, content, state string,
) error {
	turnRef, err := newRef("trn")
	if err != nil {
		return err
	}
	var next int64
	if err := tx.QueryRow(ctx, queryRuntimeCompleteexecutionSelectSessionsId, sessionID).Scan(&next); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionInsertSessionTurnsRefSessionIdTurnNumber,
		turnRef, current.organizationID, sessionID, runID, next, content, state); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeCompleteexecutionUpdateAssistantConversationsVersionUpdatedAt, sessionID); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// failRuntimeCandidateGraph fails only the candidate root graph through the specialized input-invalid path; independent roots in the same claim batch keep their own execution state and leases.
// failRuntimeCandidateGraph завершает ошибкой лишь граф корня кандидата специализированным input-invalid путём; независимые корни той же пачки сохраняют собственные состояния и аренды.
func (repository *Repository) failRuntimeCandidateGraph(ctx context.Context, tx pgx.Tx, current scope, input command.Command, candidate claimableExecution) error {
	return repository.failRuntimeGraph(ctx, tx, current, input, candidate, "RUNTIME_INPUT_INVALID", runtimeClaimEligibilityChanged)
}

// failRuntimeGraph accepts only the fixed input-invalid/timeout code-summary pairs, applies the root-scoped SQL closure and emits exact run/node/gate events plus run audits in the caller transaction. System-assistant failures also append a terminal turn before queuing terminal interaction deliveries; the outer owner commits the whole result.
// failRuntimeGraph принимает лишь фиксированные пары code/summary для input-invalid и timeout, применяет SQL-закрытие по корню и пишет точные run/node/gate события и аудит запусков в транзакции вызывающего. Отказ системного помощника также добавляет итоговый ход до постановки terminal deliveries; внешний владелец фиксирует общий результат.
func (repository *Repository) failRuntimeGraph(ctx context.Context, tx pgx.Tx, current scope, input command.Command, candidate claimableExecution, code, summary string) error {
	if (code != "RUNTIME_INPUT_INVALID" || summary != runtimeClaimEligibilityChanged) && (code != "RUNTIME_TIMEOUT" || summary != runtimeTimeoutSummary) {
		return errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryRuntimeClaimFailGraph, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": candidate.rootRunID,
		"failed_node_id": candidate.nodeID, "actor_id": current.actorID,
		"summary": summary, "safe_error_code": code,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	// transition holds the SQL-returned aggregate kind/ref, node ref and state used to select the exact audit/event envelope after graph closure.
	// transition хранит возвращённые SQL вид/ref агрегата, ref узла и состояние для выбора точного audit/event envelope после закрытия графа.
	type transition struct{ kind, ref, nodeRef, state string }
	var transitions []transition
	for rows.Next() {
		var item transition
		if err := rows.Scan(&item.kind, &item.ref, &item.nodeRef, &item.state); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		transitions = append(transitions, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	for _, item := range transitions {
		if item.kind == "RUN" {
			if err := repository.auditRuntimeClaimTransition(ctx, tx, current, input, candidate.projectID, item.ref, summary); err != nil {
				return err
			}
		}
		eventKind, gateRef, nodeState := "RUN_STATE_CHANGED", "", ""
		switch item.kind {
		case "NODE":
			eventKind, nodeState = "NODE_STATE_CHANGED", item.state
		case "GATE":
			eventKind, gateRef, nodeState = "OWNER_GATE_RESOLVED", item.ref, "CANCELLED"
		}
		if _, err := repository.emitRunEvent(ctx, tx, current, candidate.projectID, candidate.rootRunID,
			item.ref, eventKind, item.nodeRef, "", gateRef, "", summary, "FAILED", nodeState); err != nil {
			return err
		}
	}
	if candidate.stableKey == "system-assistant" {
		if err := repository.recordSystemAssistantTerminalTurn(ctx, tx, current,
			candidate.sessionID, candidate.runID, "i18n:RUNTIME_INPUT_INVALID", "FAILED"); err != nil {
			return err
		}
	}
	return repository.enqueueTerminalInteractionDeliveries(ctx, tx, current, candidate.projectID, candidate.rootRunID)
}
