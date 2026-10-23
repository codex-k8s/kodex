package websockettransport

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	// runSnapshotDiagnosticMessage is the fixed public log message for run snapshot read/projection failures, excluding upstream error text.
	// runSnapshotDiagnosticMessage — фиксированное сообщение лога об отказе чтения или проекции снимка запуска без внешнего текста ошибки.
	runSnapshotDiagnosticMessage = "realtime run snapshot unavailable"
	// runSnapshotReadStage labels the dependency-read boundary of an individual run snapshot, distinct from projection failure.
	// runSnapshotReadStage обозначает границу чтения зависимостей отдельного снимка запуска, отличая её от отказа проекции.
	runSnapshotReadStage = "RUN_SNAPSHOT_READ"
	// runSnapshotProjectStage labels a rejected run snapshot contract/projection without identifying the affected run or malformed value.
	// runSnapshotProjectStage обозначает отклонённый контракт или проекцию снимка запуска без идентификатора запуска и некорректного значения.
	runSnapshotProjectStage = "RUN_SNAPSHOT_PROJECT"
	// noRunSnapshotParentBudget distinguishes an unbounded parent context from an exhausted zero-millisecond budget.
	// noRunSnapshotParentBudget отличает родительский контекст без срока от исчерпанного бюджета в ноль миллисекунд.
	noRunSnapshotParentBudget = -1
	// runSnapshotDiagnosticStageKey names the closed read/projection stage attribute shared by snapshot diagnostics.
	// runSnapshotDiagnosticStageKey задаёт имя атрибута закрытого этапа чтения или проекции, общего для диагностики снимков.
	runSnapshotDiagnosticStageKey = "stage"
	// runSnapshotDiagnosticGRPCCodeKey names the normalized canonical gRPC status string, never arbitrary upstream status text.
	// runSnapshotDiagnosticGRPCCodeKey задаёт имя нормализованной канонической строки gRPC-статуса, а не произвольного внешнего текста.
	runSnapshotDiagnosticGRPCCodeKey = "grpc_code"
	// runSnapshotDiagnosticElapsedKey records elapsed milliseconds since the supplied operation start, not a new timeout or scheduling budget.
	// runSnapshotDiagnosticElapsedKey записывает прошедшие миллисекунды от переданного начала операции, а не новый timeout или бюджет планирования.
	runSnapshotDiagnosticElapsedKey = "elapsed_ms"
	// runSnapshotDiagnosticParentBudgetKey names the caller-supplied parent budget captured at the start of the read.
	// runSnapshotDiagnosticParentBudgetKey задаёт имя переданного начального бюджета родителя, зафиксированного при начале чтения.
	runSnapshotDiagnosticParentBudgetKey = "parent_budget_start_ms"
	// runSnapshotDiagnosticParentRemainingKey names the current remaining parent milliseconds, clamped at zero or marked unbounded.
	// runSnapshotDiagnosticParentRemainingKey задаёт имя текущего остатка миллисекунд родителя с нижней границей ноль либо признаком отсутствия срока.
	runSnapshotDiagnosticParentRemainingKey = "parent_budget_remaining_ms"
	// runSnapshotDiagnosticParentStateKey records the closed ACTIVE/CANCELED/DEADLINE_EXCEEDED state of the parent context.
	// runSnapshotDiagnosticParentStateKey записывает закрытое состояние ACTIVE/CANCELED/DEADLINE_EXCEEDED родительского контекста.
	runSnapshotDiagnosticParentStateKey = "parent_state"
	// runSnapshotDiagnosticReadStateKey distinguishes the individual read context's closed cancellation state from its parent's state.
	// runSnapshotDiagnosticReadStateKey отделяет закрытое состояние отмены отдельного контекста чтения от состояния родителя.
	runSnapshotDiagnosticReadStateKey = "read_state"
	// runSnapshotDiagnosticErrorClassKey names the fixed dependency/contract failure class selected by the relevant snapshot boundary.
	// runSnapshotDiagnosticErrorClassKey задаёт имя фиксированного класса dependency/contract, выбранного соответствующей границей снимка.
	runSnapshotDiagnosticErrorClassKey = "error_class"
)

// observeRunSnapshotReadFailure emits one warning for a nonnil read error with normalized status and parent/read context states. It does not log error text, resource references or request metadata, retry a call or change cancellation.
// observeRunSnapshotReadFailure выдаёт одно предупреждение ненулевой ошибки чтения с нормализованным статусом и состояниями контекстов родителя и чтения. Текст ошибки, ссылки и metadata не выводятся; вызов не повторяется и отмена не меняется.
func observeRunSnapshotReadFailure(parent, request context.Context, started time.Time, parentBudget int64, err error) {
	if err == nil {
		return
	}
	code := snapshotDiagnosticGRPCCode(err)
	slog.WarnContext(parent, runSnapshotDiagnosticMessage,
		runSnapshotDiagnosticStageKey, runSnapshotReadStage, runSnapshotDiagnosticGRPCCodeKey, code.String(),
		runSnapshotDiagnosticElapsedKey, time.Since(started).Milliseconds(),
		runSnapshotDiagnosticParentBudgetKey, parentBudget,
		runSnapshotDiagnosticParentRemainingKey, runSnapshotParentBudget(parent, time.Now()),
		runSnapshotDiagnosticParentStateKey, runSnapshotContextState(parent),
		runSnapshotDiagnosticReadStateKey, runSnapshotContextState(request))
}

// observeRunSnapshotProjectionFailure emits only the fixed projection stage, contract class and elapsed time for a nonnil error; success is silent and the error itself is not serialized.
// observeRunSnapshotProjectionFailure при ненулевой ошибке выдаёт только фиксированный этап проекции, класс contract и прошедшее время; успех не записывается и сама ошибка не сериализуется.
func observeRunSnapshotProjectionFailure(started time.Time, err error) {
	if err != nil {
		slog.Warn(runSnapshotDiagnosticMessage, runSnapshotDiagnosticStageKey, runSnapshotProjectStage,
			runSnapshotDiagnosticErrorClassKey, "contract", runSnapshotDiagnosticElapsedKey, time.Since(started).Milliseconds())
	}
}

// runSnapshotParentBudget reports -1 without a deadline or nonnegative remaining milliseconds relative to the supplied clock; it does not create or extend a context.
// runSnapshotParentBudget возвращает -1 без срока либо неотрицательный остаток миллисекунд относительно переданных часов; контекст не создаётся и не продлевается.
func runSnapshotParentBudget(ctx context.Context, now time.Time) int64 {
	deadline, bounded := ctx.Deadline()
	if !bounded {
		return noRunSnapshotParentBudget
	}
	return max(0, deadline.Sub(now).Milliseconds())
}

// runSnapshotContextState classifies only deadline exceeded, cancellation or active state using errors.Is on ctx.Err, without exposing an error message.
// runSnapshotContextState различает только истечение срока, отмену и активное состояние через errors.Is для ctx.Err без раскрытия текста ошибки.
func runSnapshotContextState(ctx context.Context) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "DEADLINE_EXCEEDED"
	case errors.Is(ctx.Err(), context.Canceled):
		return "CANCELED"
	default:
		return "ACTIVE"
	}
}
