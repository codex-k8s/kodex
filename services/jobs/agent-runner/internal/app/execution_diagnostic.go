package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/codex"
)

const (
	// runtimeExecutionFailureMessage fixes the pre-completion warning text so failures cannot inject provider frames, raw errors or context causes into its message.
	// runtimeExecutionFailureMessage закрепляет текст предупреждения до завершения, не позволяя подставить provider frames, сырые ошибки или context causes в сообщение.
	runtimeExecutionFailureMessage = "runtime execution failed before completion"
	// runtimeExecutionCategoryKey names the structured field whose value comes from the broker's closed failure classifier, not err.Error.
	// runtimeExecutionCategoryKey задаёт поле структурированного лога со значением из закрытого классификатора broker, а не err.Error.
	runtimeExecutionCategoryKey = "category"
	// runtimeExecutionContextStateKey separates actual caller context state from the independent execution failure category.
	// runtimeExecutionContextStateKey отделяет фактическое состояние контекста вызывающего от независимой категории отказа исполнения.
	runtimeExecutionContextStateKey = "context_state"
	// runtimeExecutionActivityFailedKey records the caller's activity-failure flag without retaining the underlying activity error or payload.
	// runtimeExecutionActivityFailedKey записывает признак отказа activity от вызывающего без сохранения исходной ошибки или пакета.
	runtimeExecutionActivityFailedKey = "activity_failed"
)

// logRuntimeExecutionFailure emits one warning through the supplied runtime logger, using only closed broker/context categories and an activity flag. Nil loggers are ignored; raw errors, input/provider frames and context causes are never passed to the logger.
// logRuntimeExecutionFailure пишет одно предупреждение через переданный runtime logger лишь с закрытыми категориями broker/context и признаком activity. Nil logger пропускается; сырые ошибки, input/provider frames и context causes в logger не передаются.
func logRuntimeExecutionFailure(ctx context.Context, logger *slog.Logger, err error, activityFailed bool) {
	if logger == nil {
		return
	}
	contextState := "ACTIVE"
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		contextState = "DEADLINE_EXCEEDED"
	case errors.Is(ctx.Err(), context.Canceled):
		contextState = "CANCELED"
	case ctx.Err() != nil:
		contextState = "UNKNOWN"
	}
	logger.WarnContext(ctx, runtimeExecutionFailureMessage,
		runtimeExecutionCategoryKey, codex.BrokerResponseFailureCategory(err),
		runtimeExecutionContextStateKey, contextState,
		runtimeExecutionActivityFailedKey, activityFailed)
}
