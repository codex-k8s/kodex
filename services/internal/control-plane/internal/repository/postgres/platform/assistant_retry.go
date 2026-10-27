package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryAssistantRetryAuthority embeds tenant/run lookup and actor-owned active assistant
// conversation linkage with authority-project constraints. Nonassistant runs are identified for
// another retry path; assistant targets require the exact associated agent.
// queryAssistantRetryAuthority встраивает поиск запуска организации и связи с активным разговором
// ассистента владельца с ограничением проекта полномочий. Обычные запуски определяются для другого
// пути повтора; ассистент требует точного связанного агента.
//
//go:embed sql/assistant_retry__authority.sql
var queryAssistantRetryAuthority string

// queryAssistantRetrySource embeds the original first USER turn, attachment set and context of an
// actor-initiated FAILED/CANCELLED root assistant run. It reads the owner source used for a new
// turn, not a later model message.
// queryAssistantRetrySource встраивает чтение исходного первого USER turn, набора вложений и
// контекста FAILED/CANCELLED корневого запуска ассистента, начатого субъектом. Читает исходные
// данные владельца для нового turn, а не позднее сообщение модели.
//
//go:embed sql/assistant_retry__source.sql
var queryAssistantRetrySource string

// queryAssistantRetryRootNode embeds the first root AGENT_EXECUTION node of the exact
// organization/root assistant run with matching conversation agent. The caller uses this locator
// inside the specialized retry lifecycle.
// queryAssistantRetryRootNode встраивает поиск первого корневого AGENT_EXECUTION узла точной
// организации и корневого запуска ассистента с совпадающим агентом разговора. Вызывающий код
// использует ссылку в специализированном жизненном цикле повтора.
//
//go:embed sql/assistant_retry__root_node.sql
var queryAssistantRetryRootNode string

// authorizeAssistantRetry classifies whether a typed run belongs to the assistant retry path,
// requiring the active actor-owned conversation and current launch permission for assistant
// targets. Invalid/missing/denied cases return handled=true with an error; nonassistant targets
// return false,nil and do not gain retry authority here.
// authorizeAssistantRetry определяет принадлежность типизированного запуска пути повтора
// ассистента, требуя активный разговор владельца и текущее разрешение запуска для ассистента.
// Неверный вход, отсутствие или отказ дают handled=true с ошибкой; обычные цели возвращают
// false,nil без выдачи здесь права повтора.
func (repository *Repository) authorizeAssistantRetry(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (bool, error) {
	payload, ok := input.Payload.(command.RunCommandInput)
	if !ok || payload.RunRef == "" {
		return true, errs.ErrInvalid
	}
	var targetType, conversationRef string
	err := tx.QueryRow(ctx, queryAssistantRetryAuthority, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"run_ref": payload.RunRef, "authority_project": current.authorityProjectID,
	}).Scan(&targetType, &conversationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, errs.ErrNotFound
	}
	if err != nil {
		return true, errs.ErrUnavailable
	}
	if targetType != "SYSTEM_ASSISTANT" {
		return false, nil
	}
	if conversationRef == "" {
		return true, errs.ErrNotFound
	}
	permission, target, err := repository.assistantCommandTarget(ctx, tx, current, conversationRef, "", "agent.launch")
	if err != nil {
		return true, err
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return true, errs.ErrNotFound
	}
	return true, nil
}

// retryAssistantRun reloads exact source content/attachments/context and queues one new assistant
// turn through the existing attachment policy, clearing the nested expected version. It requires
// exactly one returned turn, reads its run graph and returns a RUN outcome without committing or
// replaying an external provider effect.
// retryAssistantRun заново читает точные исходные содержимое, вложения и контекст и ставит один
// новый turn ассистента в очередь через прежнюю политику вложений, очищая вложенную ожидаемую
// версию. Требует ровно один возвращённый turn, читает его граф и возвращает RUN результат без
// Commit и повтора внешнего эффекта провайдера.
func (repository *Repository) retryAssistantRun(ctx context.Context, tx pgx.Tx, current scope, input command.Command, runID string) (commandOutcome, error) {
	var conversationRef, content, attachmentSetRef string
	var descriptor entity.AssistantContextDescriptor
	err := tx.QueryRow(ctx, queryAssistantRetrySource, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "run_id": runID,
	}).Scan(&conversationRef, &content, &attachmentSetRef, &descriptor.Route, &descriptor.EntityKind, &descriptor.EntityRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, fmt.Errorf("read assistant retry source: %w", errs.ErrUnavailable)
	}
	nested := input
	nested.Kind = command.AddAssistantTurn
	nested.Mutation.ExpectedVersion = nil
	nested.Payload = command.AssistantTurnInput{ConversationRef: conversationRef, Content: content,
		AttachmentSetRef: attachmentSetRef, DeliveryMode: "QUEUE", Context: &descriptor}
	outcome, err := repository.addAssistantTurnWithAttachmentPolicy(ctx, tx, current, nested, true)
	if err != nil {
		return commandOutcome{}, err
	}
	if outcome.result.Conversation == nil || len(outcome.result.Conversation.Turns) != 1 {
		return commandOutcome{}, errs.ErrUnavailable
	}
	runRef := outcome.result.Conversation.Turns[0].RunRef
	run, graph, err := repository.readRunGraphTx(ctx, tx, current, runRef)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.result.Run, outcome.result.Graph = &run, &graph
	outcome.resourceKind, outcome.resourceRef = "RUN", runRef
	return outcome, nil
}
