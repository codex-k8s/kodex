package websockettransport

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"
)

const (
	// platformSnapshotDiagnosticKindKey names the bounded resource-kind attribute; caller-supplied unknown kinds are normalized before logging.
	// platformSnapshotDiagnosticKindKey задаёт имя ограниченного атрибута вида ресурса; неизвестные входные виды нормализуются до записи лога.
	platformSnapshotDiagnosticKindKey = "kind"
	// platformSnapshotDiagnosticStage identifies the aggregate platform-snapshot read boundary separately from its individual dependency stage.
	// platformSnapshotDiagnosticStage обозначает общую границу чтения платформенного снимка отдельно от этапа конкретной зависимости.
	platformSnapshotDiagnosticStage = "PLATFORM_SNAPSHOT_READ"
	// platformSnapshotDiagnosticUnknown replaces unrecognized kinds or kind-incompatible stages without logging their raw input values.
	// platformSnapshotDiagnosticUnknown заменяет неизвестные виды либо несовместимые с видом этапы без записи их исходных значений.
	platformSnapshotDiagnosticUnknown = "UNKNOWN"
	// platformSnapshotReadStageKey names the closed dependency-stage attribute, not a caller-supplied operation or resource identifier.
	// platformSnapshotReadStageKey задаёт имя атрибута закрытого этапа зависимости, а не переданных вызывающим операции или идентификатора ресурса.
	platformSnapshotReadStageKey = "read_stage"
	// platformSnapshotAttemptKey names the bounded assistant-page adaptation attempt; unrelated or invalid attempt contexts become zero.
	// platformSnapshotAttemptKey задаёт имя ограниченной попытки подбора страницы помощника; посторонний либо недопустимый контекст попытки превращается в ноль.
	platformSnapshotAttemptKey = "read_attempt"
	// platformSnapshotAssistantPageKey records only a permitted assistant page size from the fixed halving ladder, never payload length or content.
	// platformSnapshotAssistantPageKey записывает только разрешённый размер страницы помощника из фиксированной лестницы деления, а не длину или содержимое данных.
	platformSnapshotAssistantPageKey = "assistant_page_size"
)

// platformSnapshotReadStage labels a known read/projection stage for diagnostic attribution; it does not authorize or select an RPC.
// platformSnapshotReadStage обозначает известный этап чтения или проекции для диагностики, не разрешая и не выбирая RPC.
type platformSnapshotReadStage string

const (
	// platformSnapshotAssistantGetStage attributes a failed system-assistant owner read within a SYSTEM_ASSISTANT snapshot.
	// platformSnapshotAssistantGetStage относит отказ к чтению системного помощника у владельца данных внутри снимка SYSTEM_ASSISTANT.
	platformSnapshotAssistantGetStage platformSnapshotReadStage = "SYSTEM_ASSISTANT_GET"
	// platformSnapshotConversationListStage attributes failure to the fresh owner conversation-page read, not cached conversation data.
	// platformSnapshotConversationListStage относит отказ к свежему чтению страницы диалогов у владельца, а не к закэшированным данным.
	platformSnapshotConversationListStage platformSnapshotReadStage = "ASSISTANT_CONVERSATIONS_LIST"
	// platformSnapshotBootstrapGetStage attributes failure to the bootstrap-state owner read in an assistant snapshot.
	// platformSnapshotBootstrapGetStage относит отказ к чтению состояния первоначальной настройки у владельца в снимке помощника.
	platformSnapshotBootstrapGetStage platformSnapshotReadStage = "BOOTSTRAP_STATE_GET"
	// platformSnapshotAssistantProjectStage identifies rejected assistant/bootstrap projection after dependency reads rather than a new owner mutation.
	// platformSnapshotAssistantProjectStage обозначает отклонённую проекцию помощника или первоначальной настройки после чтений, а не новое изменение у владельца.
	platformSnapshotAssistantProjectStage platformSnapshotReadStage = "SYSTEM_ASSISTANT_PROJECTION"
	// platformSnapshotRunsListStage identifies failure while reading the owner run catalog for a RUN snapshot.
	// platformSnapshotRunsListStage обозначает отказ при чтении каталога запусков у владельца для снимка RUN.
	platformSnapshotRunsListStage platformSnapshotReadStage = "RUNS_LIST"
	// platformSnapshotOwnerGatesListStage identifies the owner-gate listing dependency in a RUN snapshot without exposing individual gate references.
	// platformSnapshotOwnerGatesListStage обозначает зависимость списка подтверждений владельца в снимке RUN без раскрытия ссылок отдельных подтверждений.
	platformSnapshotOwnerGatesListStage platformSnapshotReadStage = "OWNER_GATES_LIST"
	// platformSnapshotOverviewGetStage identifies the overview dependency within the complete RUN snapshot read sequence.
	// platformSnapshotOverviewGetStage обозначает зависимость обзора в полной последовательности чтения снимка RUN.
	platformSnapshotOverviewGetStage platformSnapshotReadStage = "OVERVIEW_GET"
	// platformSnapshotRunProjectStage labels contract/projection rejection for a RUN bundle without serializing malformed fields.
	// platformSnapshotRunProjectStage обозначает отклонение контракта или проекции набора RUN без сериализации некорректных полей.
	platformSnapshotRunProjectStage platformSnapshotReadStage = "RUN_PROJECTION"
	// platformSnapshotStageFailureMessage is the fixed wrapper Error text; the original dependency error remains available only through Unwrap.
	// platformSnapshotStageFailureMessage — фиксированный текст Error обёртки; исходная ошибка зависимости доступна отдельно через Unwrap.
	platformSnapshotStageFailureMessage = "platform snapshot stage failed"
)

// platformSnapshotStageError carries only a stage label and wrapped cause. Its public error text is fixed, preserving errors.Is/status mapping without rendering the cause.
// platformSnapshotStageError хранит только обозначение этапа и исходную ошибку. Текст Error фиксирован, что сохраняет errors.Is и status-отображение без вывода причины.
type platformSnapshotStageError struct {
	stage platformSnapshotReadStage
	err   error
}

// Error returns the fixed stage-failure message without including the wrapped error, stage input or resource data.
// Error возвращает фиксированное сообщение отказа этапа без вложенной ошибки, входного этапа и данных ресурса.
func (err *platformSnapshotStageError) Error() string { return platformSnapshotStageFailureMessage }

// Unwrap exposes the original cause for existing error/status classification; diagnostic logging must still use closed fields instead of the cause text.
// Unwrap предоставляет исходную причину для прежней классификации ошибок и статуса; диагностика по-прежнему должна использовать закрытые поля вместо её текста.
func (err *platformSnapshotStageError) Unwrap() error { return err.err }

// withPlatformSnapshotReadStage leaves success nil and otherwise wraps the same error with attribution, without retrying or changing its underlying status.
// withPlatformSnapshotReadStage оставляет успех nil, а ошибку оборачивает обозначением этапа без повторов и изменения исходного статуса.
func withPlatformSnapshotReadStage(stage platformSnapshotReadStage, err error) error {
	if err == nil {
		return nil
	}
	return &platformSnapshotStageError{stage: stage, err: err}
}

// platformSnapshotErrorStage unwraps attribution and accepts only the exact stages permitted for SYSTEM_ASSISTANT or RUN; unknown kinds, stages or unstaged errors become UNKNOWN.
// platformSnapshotErrorStage извлекает обозначение и принимает только точные этапы, разрешённые для SYSTEM_ASSISTANT либо RUN; неизвестные виды, этапы и ошибки без этапа становятся UNKNOWN.
func platformSnapshotErrorStage(kind string, err error) string {
	var staged *platformSnapshotStageError
	if !errors.As(err, &staged) {
		return platformSnapshotDiagnosticUnknown
	}
	switch kind {
	case "SYSTEM_ASSISTANT":
		switch staged.stage {
		case platformSnapshotAssistantGetStage, platformSnapshotConversationListStage,
			platformSnapshotBootstrapGetStage, platformSnapshotAssistantProjectStage:
			return string(staged.stage)
		}
	case "RUN":
		switch staged.stage {
		case platformSnapshotRunsListStage, platformSnapshotOwnerGatesListStage,
			platformSnapshotOverviewGetStage, platformSnapshotRunProjectStage:
			return string(staged.stage)
		}
	}
	return platformSnapshotDiagnosticUnknown
}

// observePlatformSnapshotReadFailure logs one nonnil failure with bounded kind/stage/gRPC code/page context and elapsed/parent-budget values. It never renders error text, payloads, references or metadata, and does not alter the returned failure.
// observePlatformSnapshotReadFailure записывает один ненулевой отказ с ограниченными видом, этапом, gRPC-кодом, контекстом страницы и временем с бюджетом родителя. Текст ошибки, данные, ссылки и metadata не выводятся; возвращаемый отказ не меняется.
func observePlatformSnapshotReadFailure(ctx context.Context, started time.Time, parentBudget int64, kind string, err error, attempt int, pageSize int32) {
	if err == nil {
		return
	}
	if !slices.Contains(platformBootstrapKinds, kind) {
		kind = platformSnapshotDiagnosticUnknown
	}
	code := snapshotDiagnosticGRPCCode(err)
	attempt, pageSize = platformSnapshotDiagnosticPage(kind, attempt, pageSize)
	slog.ErrorContext(ctx, platformSnapshotReadFailure,
		platformSnapshotDiagnosticKindKey, kind,
		platformSnapshotReadStageKey, platformSnapshotErrorStage(kind, err),
		platformSnapshotAttemptKey, attempt,
		platformSnapshotAssistantPageKey, pageSize,
		runSnapshotDiagnosticErrorClassKey, "dependency",
		runSnapshotDiagnosticStageKey, platformSnapshotDiagnosticStage,
		runSnapshotDiagnosticGRPCCodeKey, code.String(),
		runSnapshotDiagnosticElapsedKey, time.Since(started).Milliseconds(),
		runSnapshotDiagnosticParentBudgetKey, parentBudget,
		runSnapshotDiagnosticParentRemainingKey, runSnapshotParentBudget(ctx, time.Now()),
		runSnapshotDiagnosticParentStateKey, runSnapshotContextState(ctx))
}

// platformSnapshotDiagnosticPage accepts only SYSTEM_ASSISTANT sizes 50,25,12,6,3,1 with an attempt from one through that size's ladder position, allowing a smaller initial hint. Other combinations become zero/zero.
// platformSnapshotDiagnosticPage допускает только размеры SYSTEM_ASSISTANT 50,25,12,6,3,1 с попыткой от единицы до позиции размера в лестнице, включая меньшую стартовую подсказку. Остальные сочетания становятся нулём и нулём.
func platformSnapshotDiagnosticPage(kind string, attempt int, pageSize int32) (int, int32) {
	if kind == "SYSTEM_ASSISTANT" {
		for number, size := 1, int32(platformSnapshotPageSize); ; number, size = number+1, max(1, size/2) {
			if attempt >= 1 && attempt <= number && size == pageSize {
				return attempt, size
			}
			if size == 1 {
				break
			}
		}
	}
	return 0, 0
}
