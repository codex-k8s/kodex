package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryAssistantContextProjection embeds the authoritative actor/project-bound assistant context
// projection with current entity name, version and allowed operations. It returns metadata, not a
// reusable command grant.
// queryAssistantContextProjection встраивает авторитетную проекцию контекста ассистента с
// привязкой к субъекту и проекту, текущими именем, версией и допустимыми операциями. Возвращает
// метаданные, а не повторно используемое разрешение команды.
//
//go:embed sql/assistant_context_projection.sql
var queryAssistantContextProjection string

// queryAssistantConversationContext embeds actor-owned conversation or plan context lookup with
// fresh projection, active-project visibility and specialized assistant operation eligibility. An
// authority-project pin limits the lookup; stored plan or context fields do not assign authority.
// queryAssistantConversationContext встраивает поиск контекста разговора либо плана владельца с
// актуальной проекцией, видимостью активного проекта и допустимостью специализированных операций
// ассистента. Проект полномочий ограничивает поиск; сохранённые поля плана и контекста не
// назначают права.
//
//go:embed sql/assistant_conversation_context.sql
var queryAssistantConversationContext string

// queryAssistantConfigurationPlanOwner embeds the tenant/ref lookup of a plan conversation and
// stored operations. Caller context authorization precedes this lookup, and each privileged
// operation is reauthorized separately.
// queryAssistantConfigurationPlanOwner встраивает поиск разговора и сохранённых операций плана по
// организации и ссылке. Доступ к контексту проверяется до поиска, каждая привилегированная
// операция авторизуется отдельно.
//
//go:embed sql/assistant_configuration__plan_owner.sql
var queryAssistantConfigurationPlanOwner string

// assistantConversationContext reads current authorized conversation context and project using
// either conversation or plan ref. NoRows becomes NotFound and other scan failures Unavailable; it
// neither executes operations nor commits.
// assistantConversationContext читает текущий разрешённый контекст разговора и проект по ссылке
// разговора либо плана. NoRows преобразуется в NotFound, прочие ошибки чтения — в Unavailable; не
// исполняет операции и не завершает транзакцию.
func (repository *Repository) assistantConversationContext(ctx context.Context, tx pgx.Tx, current scope, conversationRef, planRef string) (entity.AssistantContextDescriptor, string, error) {
	var descriptor entity.AssistantContextDescriptor
	var projectRef string
	err := tx.QueryRow(ctx, queryAssistantConversationContext, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "authority_project": current.authorityProjectID,
		"conversation_ref": conversationRef, "plan_ref": planRef,
	}).Scan(&projectRef, &descriptor.Route, &descriptor.EntityKind, &descriptor.EntityRef, &descriptor.EntityName, &descriptor.EntityVersion, &descriptor.AllowedOperations)
	if errors.Is(err, pgx.ErrNoRows) {
		return descriptor, "", errs.ErrNotFound
	}
	if err != nil {
		return descriptor, "", errs.ErrUnavailable
	}
	return descriptor, projectRef, nil
}

// authorizeAssistantContextCommand routes supported typed payloads through conversation creation
// or existing context admission. Plan update/validate/apply reload stored operations, require the
// isolated connection-preparation case, constrain plan scope and reauthorize specialized
// configuration/file/runtime commands. Other payload types are left to outer dispatch; this guard
// performs no mutation or commit.
// authorizeAssistantContextCommand проводит поддерживаемые типизированные payload через допуск
// создания разговора либо существующего контекста. Для update/validate/apply плана загружает
// сохранённые операции, требует отдельность подготовки подключения, ограничивает область плана и
// заново авторизует специализированные команды конфигурации, файлов и runtime. Другие типы
// оставляет внешней диспетчеризации; сама проверка не меняет состояние и не завершает транзакцию.
func (repository *Repository) authorizeAssistantContextCommand(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	var conversationRef, planRef string
	switch payload := input.Payload.(type) {
	case command.AssistantConversationInput:
		if _, err := repository.assistantForConversationCreation(ctx, tx, current, payload); err != nil {
			return err
		}
		_, err := repository.resolveAssistantContext(ctx, tx, current, payload.Context, payload.ProjectRef)
		return err
	case command.AssistantConversationTitleInput:
		conversationRef = payload.ConversationRef
	case command.AssistantConversationArchiveInput:
		conversationRef = payload.ConversationRef
	case command.AssistantTurnInput:
		conversationRef = payload.ConversationRef
	case command.AssistantPlanInput:
		planRef = payload.PlanRef
	case command.AssistantPlanDraftInput:
		planRef = payload.PlanRef
	default:
		return nil
	}
	if conversationRef == "" && planRef == "" {
		return errs.ErrInvalid
	}
	_, _, err := repository.assistantConversationContext(ctx, tx, current, conversationRef, planRef)
	if err != nil {
		return err
	}
	if planRef != "" && (input.Kind == command.UpdateAssistantPlan || input.Kind == command.ValidateAssistantPlan || input.Kind == command.ApplyAssistantPlan) {
		var raw []byte
		if err := tx.QueryRow(ctx, queryAssistantConfigurationPlanOwner, pgx.StrictNamedArgs{"organization_id": current.organizationID, "plan_ref": planRef}).Scan(&conversationRef, &raw); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
		var operations []entity.AssistantPlanOperation
		if json.Unmarshal(raw, &operations) != nil {
			return errs.ErrUnavailable
		}
		for _, operation := range operations {
			if operation.Type != prepareProjectAssistantConnection {
				continue
			}
			if len(operations) != 1 {
				return errs.ErrInvalid
			}
			normalized, err := normalizeAssistantOperation(operation)
			if err != nil {
				return err
			}
			planned, err := projectAssistantConnectionCommand(normalized)
			if err != nil {
				return err
			}
			payload, valid := planned.Payload.(command.ProjectAssistantConnectionInput)
			if !valid {
				return errs.ErrInvalid
			}
			if err := repository.authorizeProjectAssistantConnection(ctx, tx, current, payload, true); err != nil {
				return err
			}
		}
		if err := repository.constrainAssistantPlanScope(ctx, tx, &current, conversationRef, operations); err != nil {
			return err
		}
		for _, operation := range operations {
			if assistantProjectConfigurationOperation(operation) {
				if _, err := repository.authorizeProjectAssistantConfiguration(ctx, tx, current, operation, true); err != nil {
					return err
				}
			}
			if !assistantFileOperation(operation.Type) && !assistantProjectConfigurationOperation(operation) && operation.Type != "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" && operation.Type != "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" && operation.Type != "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
				continue
			}
			operation, err := normalizeAssistantOperation(operation)
			if err != nil {
				return err
			}
			planned, err := assistantOperationCommand(operation)
			if err != nil {
				return err
			}
			if err := repository.authorizeCommand(ctx, tx, current, planned); err != nil {
				return err
			}
		}
	}
	return nil
}
