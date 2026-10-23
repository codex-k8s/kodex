package app

import (
	"context"
	"log/slog"
	"regexp"

	"github.com/codex-k8s/kodex/services/jobs/session-archive/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// archiveRPCObservationMessage names the safe record of a task's owner RPC result.
	// RU: archiveRPCObservationMessage задаёт безопасную запись результата owner RPC задания.
	archiveRPCObservationMessage = "session archive task RPC observation"
	// archiveRPCStageAttribute names the closed owner-command stage log field.
	// RU: archiveRPCStageAttribute задаёт поле журнала закрытого этапа owner-команды.
	archiveRPCStageAttribute = "stage"
	// archiveTaskRefAttribute names the bounded task-reference log field.
	// RU: archiveTaskRefAttribute задаёт поле журнала ограниченной ссылки задания.
	archiveTaskRefAttribute = "task_ref"
	// archiveSessionRefAttribute names the bounded session-reference log field.
	// RU: archiveSessionRefAttribute задаёт поле журнала ограниченной ссылки сессии.
	archiveSessionRefAttribute = "session_ref"
	// archiveTaskKindAttribute names the allowlisted task-kind log field.
	// RU: archiveTaskKindAttribute задаёт поле журнала вида задания из разрешённого списка.
	archiveTaskKindAttribute = "task_kind"
	// archiveContentGenerationAttribute names the nonnegative content-generation log field.
	// RU: archiveContentGenerationAttribute задаёт поле журнала неотрицательного поколения содержимого.
	archiveContentGenerationAttribute = "content_generation"
	// archiveAttemptAttribute names the bounded attempt-number log field.
	// RU: archiveAttemptAttribute задаёт поле журнала ограниченного номера попытки.
	archiveAttemptAttribute = "attempt"
	// archiveDiagnosticUnknown is the single safe fallback for invalid diagnostic input.
	// RU: archiveDiagnosticUnknown задаёт единственное безопасное запасное значение неверного диагностического ввода.
	archiveDiagnosticUnknown = "UNKNOWN"
)

// archiveRPCStage identifies the specialized owner command whose result is being observed.
// RU: archiveRPCStage определяет специализированную owner-команду, результат которой наблюдается.
type archiveRPCStage string

const (
	// archiveRPCRenew marks the owner lease-renewal command.
	// RU: archiveRPCRenew обозначает owner-команду продления lease.
	archiveRPCRenew archiveRPCStage = "RENEW"
	// archiveRPCFail marks the specialized terminal failure command.
	// RU: archiveRPCFail обозначает специализированную команду конечного отказа.
	archiveRPCFail archiveRPCStage = "FAIL"
	// archiveRPCCompleteSnapshot marks completion of a verified snapshot receipt.
	// RU: archiveRPCCompleteSnapshot обозначает завершение с проверенной квитанцией снимка.
	archiveRPCCompleteSnapshot archiveRPCStage = "COMPLETE_SNAPSHOT"
	// archiveRPCCompleteRestore marks completion of restored-source verification.
	// RU: archiveRPCCompleteRestore обозначает завершение проверки восстановленного источника.
	archiveRPCCompleteRestore archiveRPCStage = "COMPLETE_RESTORE"
	// archiveRPCCompletePVCDeletion marks completion of an observed PVC deletion.
	// RU: archiveRPCCompletePVCDeletion обозначает завершение наблюдавшегося удаления PVC.
	archiveRPCCompletePVCDeletion archiveRPCStage = "COMPLETE_PVC_DELETION"
	// archiveRPCCompleteObjectDeletion marks completion of an observed archive-object deletion.
	// RU: archiveRPCCompleteObjectDeletion обозначает завершение наблюдавшегося удаления объекта архива.
	archiveRPCCompleteObjectDeletion archiveRPCStage = "COMPLETE_OBJECT_DELETION"
)

// archiveDiagnosticRef limits publicly logged identifiers to a bounded safe character set.
// RU: archiveDiagnosticRef ограничивает выводимые идентификаторы безопасным набором символов и длиной.
var archiveDiagnosticRef = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// observeArchiveRPC records the owner RPC outcome separately from worker output, excluding error text, lease, fence and receipts.
// RU: observeArchiveRPC фиксирует исход owner RPC отдельно от результата worker, исключая текст ошибки, lease, fence и квитанции.
func observeArchiveRPC(ctx context.Context, logger *slog.Logger, task model.Task, stage archiveRPCStage, err error) {
	if logger == nil {
		return
	}
	logger.InfoContext(ctx, archiveRPCObservationMessage,
		archiveTaskRefAttribute, archivePublicRef(task.TaskRef),
		archiveSessionRefAttribute, archivePublicRef(task.SessionRef),
		archiveTaskKindAttribute, archiveTaskKind(task.Kind),
		archiveContentGenerationAttribute, max(task.ContentGeneration, 0),
		archiveAttemptAttribute, archiveDiagnosticAttempt(task.Attempt),
		archiveRPCStageAttribute, archiveDiagnosticStage(stage),
		archiveRPCCodeAttribute, archiveDiagnosticRPCCode(err))
}

// archivePublicRef returns only a safe bounded reference or the shared diagnostic fallback.
// RU: archivePublicRef возвращает лишь безопасную ограниченную ссылку либо общее запасное значение диагностики.
func archivePublicRef(value string) string {
	if archiveDiagnosticRef.MatchString(value) {
		return value
	}
	return archiveDiagnosticUnknown
}

// archiveTaskKind exposes only the four supported archive task kinds in logs.
// RU: archiveTaskKind раскрывает в журнале лишь четыре поддерживаемых вида задания архива.
func archiveTaskKind(value string) string {
	switch value {
	case "SNAPSHOT", "RESTORE", "DELETE_PVC", "DELETE_OBJECT":
		return value
	default:
		return archiveDiagnosticUnknown
	}
}

// archiveDiagnosticAttempt maps attempts outside the supported one-through-five range to zero.
// RU: archiveDiagnosticAttempt отображает попытки вне поддерживаемого диапазона от одной до пяти в ноль.
func archiveDiagnosticAttempt(value int32) int32 {
	if value < 1 || value > 5 {
		return 0
	}
	return value
}

// archiveDiagnosticStage accepts only specialized lease and terminal-command stages.
// RU: archiveDiagnosticStage принимает лишь специализированные этапы lease и конечных команд.
func archiveDiagnosticStage(value archiveRPCStage) string {
	switch value {
	case archiveRPCRenew, archiveRPCFail, archiveRPCCompleteSnapshot, archiveRPCCompleteRestore,
		archiveRPCCompletePVCDeletion, archiveRPCCompleteObjectDeletion:
		return string(value)
	default:
		return archiveDiagnosticUnknown
	}
}

// archiveDiagnosticRPCCode preserves canonical gRPC codes and rejects out-of-range values.
// RU: archiveDiagnosticRPCCode сохраняет канонические gRPC-коды и отклоняет значения вне диапазона.
func archiveDiagnosticRPCCode(err error) string {
	code := status.Code(err)
	if code > codes.Unauthenticated {
		return archiveDiagnosticUnknown
	}
	return code.String()
}
