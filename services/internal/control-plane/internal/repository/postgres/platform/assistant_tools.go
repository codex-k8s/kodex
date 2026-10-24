package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	roleimageservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// maximumAssistantPlanOperations caps an incoming assistant plan proposal at32 operations before
// hydration; later exact no-op elimination can reduce that count but cannot admit a larger
// proposal.
// maximumAssistantPlanOperations ограничивает входящий план помощника32 операциями до hydration;
// последующее исключение точных no-op может уменьшить число, но не допускает больший исходный
// план.
const maximumAssistantPlanOperations = 32

// assistantPlanProposal carries a transaction-prepared proposal with conversation/project
// identity, server-resolved actor scope, normalized operations and fenced lease metadata. It is
// intermediate state, not a committed plan or standalone authority grant.
// assistantPlanProposal несёт подготовленный в транзакции план с identity беседы/проекта,
// разрешённой сервером областью субъекта, нормализованными операциями и fenced lease metadata. Это
// промежуточное состояние, не зафиксированный план и не самостоятельный grant полномочий.
type assistantPlanProposal struct {
	payload                                                                                       command.ProposeAssistantPlanInput
	conversationID, conversationRef, projectID, projectRef, assistantRef, contextKind, contextRef string
	conversationVersion                                                                           int64
	actorScope                                                                                    scope
	operations                                                                                    []entity.AssistantPlanOperation
	lease                                                                                         map[string]any
}

// prepareAssistantPlanProposalTx validates proposal bounds and an exact fenced lease, locks the
// lease-run conversation context and derives the originating human actor from owner state. It
// applies project/self/context restrictions, hydrates server snapshots, normalizes/binds
// specialized commands and authorizes each before persistence. Capability/grant conflicts are
// skipped; an exact runtime no-op is skipped only after authorization and fresh pin matching.
// Stage errors retain one-based operation position; no commit occurs here.
// prepareAssistantPlanProposalTx проверяет размер предложения и точную fenced lease, блокирует
// контекст беседы lease-run и выводит исходного человека из состояния владельца. Применяет
// project/self/context ограничения, загружает серверные снимки, нормализует и связывает
// специализированные команды, авторизует каждую до сохранения. Capability/grant conflicts
// пропускаются; точный runtime no-op пропускается лишь после authorization и свежей сверки pins.
// Ошибка сохраняет stage и позицию операции от единицы; commit здесь нет.
func (repository *Repository) prepareAssistantPlanProposalTx(ctx context.Context, tx pgx.Tx, machineScope scope, input command.Command) (assistantPlanProposal, error) {
	payload, ok := input.Payload.(command.ProposeAssistantPlanInput)
	if !ok || strings.TrimSpace(payload.Summary) == "" || utf8.RuneCountInString(payload.Summary) > 2000 ||
		len(payload.Operations) == 0 || len(payload.Operations) > maximumAssistantPlanOperations {
		return assistantPlanProposal{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, machineScope, command.LeaseInput{
		LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation,
	}, true)
	if err != nil {
		return assistantPlanProposal{}, err
	}
	var conversationID, conversationRef, projectID, projectRef string
	var assistantRef string
	var contextKind, contextRef string
	var allowedOperations []string
	var conversationVersion int64
	actorScope := scope{correlationRef: machineScope.correlationRef}
	if err := tx.QueryRow(ctx, queryRuntimeProposeassistantplanSelectContext,
		machineScope.organizationID, lease["runID"],
	).Scan(&conversationID, &conversationRef, &conversationVersion, &projectID, &projectRef, &allowedOperations, &contextKind, &contextRef, &assistantRef,
		&actorScope.actorID, &actorScope.actorRef, &actorScope.actorName, &actorScope.role,
		&actorScope.organizationRef); err != nil {
		return assistantPlanProposal{}, errs.ErrForbidden
	}
	actorScope.organizationID = machineScope.organizationID
	assistant, err := repository.conversationAssistantTx(ctx, tx, actorScope, conversationRef)
	if err != nil || assistantRef != assistant.Ref {
		return assistantPlanProposal{}, errs.ErrForbidden
	}
	if assistant.Scope == "PROJECT" {
		actorScope.authorityProjectID = projectID
	}
	seen := make(map[string]struct{}, len(payload.Operations))
	normalizedOperations := make([]entity.AssistantPlanOperation, 0, len(payload.Operations))
	for index, operation := range payload.Operations {
		if operation.Type == changeProjectAssistantIntegrationGrant && (assistant.Scope != "PROJECT" || assistantString(operation.Parameters, "projectAssistantRef") != assistant.Ref) {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		if operation.Type == prepareProjectAssistantConnection && (len(payload.Operations) != 1 || assistant.Scope != "PROJECT" || assistantString(operation.Parameters, "projectAssistantRef") != assistant.Ref) {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		if assistant.Scope == "PROJECT" && !projectAssistantOperation(operation.Type) {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		if assistant.Scope == "PROJECT" && operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" && assistantString(operation.Parameters, "agentRef") != assistant.Ref {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		if assistantProjectConfigurationOperation(operation) && assistant.Scope == "PROJECT" && assistantString(operation.Parameters, "projectAssistantRef") != assistant.Ref {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		if operation.Key == "" || len(operation.Key) > 96 {
			return assistantPlanProposal{}, errs.ErrInvalid
		}
		if _, duplicate := seen[operation.Key]; duplicate {
			return assistantPlanProposal{}, errs.ErrInvalid
		}
		seen[operation.Key] = struct{}{}
		selfConfiguration := assistantSelfConfigurationOperation(assistant.Ref, assistant.Scope, operation)
		if assistantProjectConfigurationOperation(operation) {
			selfConfiguration = assistant.Scope == "SYSTEM" || assistant.Scope == "PROJECT" && assistantString(operation.Parameters, "projectAssistantRef") == assistant.Ref
		}
		if assistant.Scope == "PROJECT" && !assistantProjectConfigurationOperation(operation) && operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" &&
			assistantString(operation.Parameters, "systemAssistantRef") == "" {
			configuration, readErr := repository.getRuntimeConfigurationViewTx(ctx, tx, actorScope, assistant.Ref)
			if readErr != nil {
				return assistantPlanProposal{}, readErr
			}
			selfConfiguration = configuration.Environment.ProjectRef == projectRef &&
				configuration.Environment.Ref == assistantString(operation.Parameters, "environmentRef")
		}
		if !contains(allowedOperations, operation.Type) && !selfConfiguration {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		if operation.Type == createProjectFileRevision && (projectRef == "" || assistant.Scope == "SYSTEM" && contextKind != "PROJECT" && contextKind != "FILE") {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		projectGlobalRevision := operation.Type == createProjectFileRevision && assistant.Scope == "PROJECT"
		if !selfConfiguration && !projectGlobalRevision && !assistantOperationMatchesContext(contextKind, contextRef, operation) {
			return assistantPlanProposal{}, errs.ErrForbidden
		}
		if operation.Type == createProjectFileRevision && assistantString(operation.Parameters, "contentRef") != "" {
			var matches bool
			matches, err = repository.assistantFileRevisionSnapshotMatches(ctx, tx, actorScope, operation)
			if err == nil && !matches {
				err = errs.ErrVersionMismatch
			}
		} else {
			operation, err = repository.hydrateAssistantOperation(ctx, tx, actorScope, projectRef, operation)
		}
		noRuntimeChange := operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" && errors.Is(err, errAssistantRuntimeConfigurationNoChange)
		if err != nil && !noRuntimeChange {
			if (operation.Type == "CHANGE_CAPABILITY" || operation.Type == "CHANGE_INTEGRATION_GRANT") && errors.Is(err, errs.ErrConflict) {
				continue
			}
			return assistantPlanProposal{}, errs.WithAssistantPlanStage(err, errs.AssistantPlanHydrate, index+1)
		}
		operation, err = normalizeAssistantOperation(operation)
		if err != nil {
			return assistantPlanProposal{}, errs.WithAssistantPlanStage(err, errs.AssistantPlanNormalize, index+1)
		}
		operation, err = bindAssistantOperationProject(operation, projectRef)
		if err != nil {
			return assistantPlanProposal{}, errs.WithAssistantPlanStage(err, errs.AssistantPlanBind, index+1)
		}
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return assistantPlanProposal{}, errs.WithAssistantPlanStage(err, errs.AssistantPlanCommand, index+1)
		}
		if err := repository.authorizeAssistantPreparedOperation(ctx, tx, actorScope, operation, planned); err != nil {
			return assistantPlanProposal{}, errs.WithAssistantPlanStage(err, errs.AssistantPlanAuthorize, index+1)
		}
		if noRuntimeChange {
			// Пропускается только точный no-op, после обычных authority/binding
			// проверок и повторной сверки server-owned версии и свежих pins.
			matches, err := repository.assistantRuntimeConfigurationSnapshotMatches(ctx, tx, actorScope, operation)
			if err != nil {
				return assistantPlanProposal{}, errs.WithAssistantPlanStage(err, errs.AssistantPlanAuthorize, index+1)
			}
			if !matches {
				return assistantPlanProposal{}, errs.WithAssistantPlanStage(errs.ErrVersionMismatch, errs.AssistantPlanAuthorize, index+1)
			}
			continue
		}
		normalizedOperations = append(normalizedOperations, operation)
	}
	if len(normalizedOperations) == 0 {
		return assistantPlanProposal{}, errs.WithAssistantPlanStage(errs.ErrConflict, errs.AssistantPlanEmpty, 0)
	}
	return assistantPlanProposal{payload: payload, conversationID: conversationID, conversationRef: conversationRef, conversationVersion: conversationVersion, projectID: projectID, projectRef: projectRef, assistantRef: assistantRef, contextKind: contextKind, contextRef: contextRef, actorScope: actorScope, operations: normalizedOperations, lease: lease}, nil
}

// proposeAssistantPlan persists the prepared normalized plan and its first immutable revision,
// links prepared file-content ledgers and advances the conversation in the caller transaction. It
// records originating-actor audit and platform invalidation, returning DRAFT plan/conversation
// metadata; it neither applies planned operations nor commits the transaction.
// proposeAssistantPlan сохраняет подготовленный нормализованный план и первую immutable revision,
// связывает ledgers подготовленного содержимого файлов и обновляет беседу в транзакции
// вызывающего. Записывает audit исходного субъекта и platform invalidation, возвращает DRAFT
// план/беседу; не применяет запланированные операции и не выполняет commit.
func (repository *Repository) proposeAssistantPlan(ctx context.Context, tx pgx.Tx, machineScope scope, input command.Command) (commandOutcome, error) {
	prepared, err := repository.prepareAssistantPlanProposalTx(ctx, tx, machineScope, input)
	if err != nil {
		return commandOutcome{}, err
	}
	payload, actorScope := prepared.payload, prepared.actorScope
	conversationID, conversationRef, conversationVersion := prepared.conversationID, prepared.conversationRef, prepared.conversationVersion
	projectID, projectRef, assistantRef := prepared.projectID, prepared.projectRef, prepared.assistantRef
	normalizedOperations := prepared.operations
	if err := assistantFilePersistenceReady(normalizedOperations, payload.PreparedContent); err != nil {
		return commandOutcome{}, err
	}
	planRef, err := newRef("pln")
	if err != nil {
		return commandOutcome{}, err
	}
	rawOperations := asJSON(normalizedOperations)
	digest := assistantPlanDigest(strings.TrimSpace(payload.Summary), rawOperations)
	var planID string
	if err := tx.QueryRow(ctx, queryConfigurationAddassistantturncommandInsertAssistantPlansRefConversationRefOperations,
		planRef, machineScope.organizationID, conversationRef, strings.TrimSpace(payload.Summary), rawOperations, digest,
	).Scan(&planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	revisionRef, err := newRef("prv")
	if err != nil {
		return commandOutcome{}, err
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryConfigurationInsertAssistantPlanRevision,
		revisionRef, machineScope.organizationID, planID, int64(1), strings.TrimSpace(payload.Summary),
		rawOperations, digest, "SYSTEM_ASSISTANT", assistantRef,
	).Scan(&createdAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	for _, operation := range normalizedOperations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		ledgerID := payload.PreparedContent[operation.Key]
		if ledgerID == "" || assistantString(operation.Parameters, "contentRef") == "" {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.linkPreparedContentTx(ctx, tx, actorScope, ledgerID, planID, 1, operation.Key); err != nil {
			return commandOutcome{}, err
		}
	}
	if _, err := tx.Exec(ctx, queryConfigurationAddassistantturncommandUpdateAssistantConversationsLatestPlanIdVersionUpdatedAt,
		conversationID, planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	plan := entity.AssistantPlan{Ref: planRef, ConversationRef: conversationRef, ProjectRef: projectRef,
		Summary: strings.TrimSpace(payload.Summary), State: "DRAFT", Version: 1, Revision: 1,
		ContentDigest: digest, Operations: normalizedOperations, CreatedAt: createdAt}
	conversation := entity.AssistantConversation{Ref: conversationRef, ProjectRef: projectRef, State: "ACTIVE",
		Version: conversationVersion + 1, LatestPlan: &plan, UpdatedAt: time.Now().UTC()}
	proposal := commandOutcome{projectID: projectID, projectRef: projectRef, resourceKind: "ASSISTANT_PLAN",
		resourceRef: planRef, summary: "i18n:ASSISTANT_PLAN_PROPOSED"}
	if _, err := repository.auditAssistantOperation(ctx, tx, actorScope, conversationRef, proposal, "PROPOSE_CONFIGURATION_PLAN"); err != nil {
		return commandOutcome{}, err
	}
	if err := repository.emitPlatformEvent(ctx, tx, actorScope, "SYSTEM_ASSISTANT_CHANGED", projectRef, planRef,
		"i18n:ASSISTANT_PLAN_PROPOSED"); err != nil {
		return commandOutcome{}, err
	}
	proposal.result = command.Result{Conversation: &conversation, Plan: &plan}
	return proposal, nil
}

// projectAssistantOperation recognizes the explicit operation allowlist for a project assistant,
// including project-local files/configuration and specialized connection/grant preparation.
// Unknown kinds are rejected; recognition alone does not authorize the target or actor.
// projectAssistantOperation распознаёт явный список операций проектного помощника, включая
// локальные файлы/конфигурацию и специализированную подготовку connection/grant. Неизвестные виды
// отклоняет; распознавание само не авторизует цель и субъекта.
func projectAssistantOperation(operationType string) bool {
	switch operationType {
	case prepareProjectAssistantConnection, changeProjectAssistantIntegrationGrant:
		return true
	case "CREATE_PROJECT_FILE", createProjectFileRevision, "UPDATE_PROJECT", "CREATE_AGENT", "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT",
		"CREATE_WORKFLOW", "UPDATE_WORKFLOW", "CHANGE_CAPABILITY", "CHANGE_INTEGRATION_GRANT", "CREATE_SCHEDULE",
		"UPDATE_SCHEDULE", "LAUNCH_RUN", "ARCHIVE_AGENT", "ARCHIVE_WORKFLOW", "CREATE_RUNTIME_ENVIRONMENT_DRAFT",
		"PREPARE_RUNTIME_ENVIRONMENT_REVISION", "BIND_AGENT_RUNTIME_ENVIRONMENT", "CREATE_ROLE_IMAGE_RECIPE", "UPDATE_ROLE_IMAGE_RECIPE", "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION":
		return true
	default:
		return false
	}
}

// assistantSelfConfigurationOperation classifies specialized self-configuration exceptions using
// assistant scope/reference and operation parameters. Project operations require their own
// assistant where applicable; system exceptions preserve their existing kind-specific rules. This
// classifier is followed by snapshot and command authorization rather than granting authority
// itself.
// assistantSelfConfigurationOperation классифицирует специализированные исключения
// self-configuration по области/ссылке помощника и параметрам. Проектные операции требуют своего
// помощника там, где применимо; системные исключения сохраняют отдельные правила видов. После
// классификации следуют проверки снимка и команды; сам классификатор полномочий не даёт.
func assistantSelfConfigurationOperation(assistantRef, assistantScope string, operation entity.AssistantPlanOperation) bool {
	if operation.Type == changeProjectAssistantIntegrationGrant {
		return assistantScope == "PROJECT" && assistantRef != "" && assistantString(operation.Parameters, "projectAssistantRef") == assistantRef
	}
	if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
		return assistantScope == "SYSTEM" && assistantRef != ""
	}
	if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
		return assistantScope == "SYSTEM" || assistantScope == "PROJECT" && assistantString(operation.Parameters, "agentRef") == assistantRef
	}
	if assistantScope == "PROJECT" {
		return assistantRef != "" && assistantString(operation.Parameters, "agentRef") == assistantRef &&
			(operation.Type == "CREATE_INSTRUCTION_DRAFT" || operation.Type == "BIND_AGENT_RUNTIME_ENVIRONMENT" || operation.Type == "UPDATE_AGENT" || operation.Type == "CHANGE_CAPABILITY")
	}
	requestedRef := assistantString(operation.Parameters, "systemAssistantRef")
	if assistantScope != "SYSTEM" || assistantRef == "" || requestedRef != assistantRef {
		return false
	}
	return operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" ||
		operation.Type == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" || operation.Type == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE"
}

// assistantPlanDigest hashes trimmed summary, one newline and the exact serialized operation bytes
// into lowercase hexadecimal SHA-256. It pins this representation without canonicalizing JSON or
// authenticating the proposer.
// assistantPlanDigest хеширует очищенный summary, один перевод строки и точные сериализованные
// байты операций в lowercase hexadecimal SHA-256. Фиксирует это представление без канонизации JSON
// и удостоверения автора.
func assistantPlanDigest(summary string, rawOperations []byte) string {
	digest := sha256.Sum256(append(append([]byte(strings.TrimSpace(summary)), '\n'), rawOperations...))
	return fmt.Sprintf("%x", digest[:])
}

// assistantOperationType recognizes the closed supported plan-operation registry used by
// normalization; specialized kinds remain separate from command-time authorization and
// persistence.
// assistantOperationType распознаёт закрытый реестр поддерживаемых операций для нормализации;
// специализированные виды остаются отделены от авторизации команды и сохранения.
func assistantOperationType(value string) bool {
	switch value {
	case prepareProjectAssistantConnection, changeProjectAssistantIntegrationGrant:
		return true
	case "CREATE_PROJECT", "CREATE_PROJECT_ASSISTANT", "CREATE_PROJECT_FILE", createProjectFileRevision, "UPDATE_PROJECT", "CREATE_AGENT", "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "CREATE_WORKFLOW", "UPDATE_WORKFLOW", "CHANGE_CAPABILITY",
		"CHANGE_INTEGRATION_GRANT", "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", "CREATE_SCHEDULE", "UPDATE_SCHEDULE", "LAUNCH_RUN",
		"CREATE_INTEGRATION_CONNECTION", "UPDATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION", "PUBLISH_INTEGRATION_DEFINITION", "ARCHIVE_AGENT", "ARCHIVE_WORKFLOW",
		"CREATE_RUNTIME_ENVIRONMENT_DRAFT", "PREPARE_RUNTIME_ENVIRONMENT_REVISION", "BIND_AGENT_RUNTIME_ENVIRONMENT", "CREATE_ROLE_IMAGE_RECIPE", "UPDATE_ROLE_IMAGE_RECIPE", "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION":
		return true
	default:
		return false
	}
}

// assistantOperationMatchesContext checks operation-specific context/ref compatibility, including
// exact agent/workflow/environment/connection/schedule bindings and grant recipient selection.
// Explicit self/global kinds and the default create path retain their existing exceptions; this
// shape check does not replace owner-resolved authority.
// assistantOperationMatchesContext проверяет совместимость контекста/ref для вида операции,
// включая точные agent/workflow/environment/connection/schedule и выбор получателя grant. Явные
// self/global виды и default create path сохраняют прежние исключения; проверка формы не заменяет
// разрешённые владельцем полномочия.
func assistantOperationMatchesContext(contextKind, contextRef string, operation entity.AssistantPlanOperation) bool {
	switch operation.Type {
	case createProjectFileRevision:
		return contextRef != "" && (contextKind == "PROJECT" || contextKind == "FILE")
	case prepareProjectAssistantConnection, changeProjectAssistantIntegrationGrant:
		return true
	case "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION", "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT":
		return true
	case "PUBLISH_INTEGRATION_DEFINITION":
		return contextKind == "" && contextRef == ""
	case "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS":
		return assistantString(operation.Parameters, "systemAssistantRef") != ""
	case "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT":
		return contextKind == "AGENT" && contextRef != "" && assistantString(operation.Parameters, "agentRef") == contextRef
	case "UPDATE_WORKFLOW":
		return contextKind == "WORKFLOW" && contextRef != "" && assistantString(operation.Parameters, "workflowRef") == contextRef
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		return assistantString(operation.Parameters, "systemAssistantRef") != "" ||
			contextKind == "ENVIRONMENT" && contextRef != "" && assistantString(operation.Parameters, "environmentRef") == contextRef
	case "UPDATE_INTEGRATION_CONNECTION":
		return contextKind == "INTEGRATION_CONNECTION" && contextRef != "" && assistantString(operation.Parameters, "connectionRef") == contextRef
	case "CHANGE_INTEGRATION_GRANT":
		switch contextKind {
		case "AGENT":
			return contextRef != "" && assistantString(operation.Parameters, "agentRef") == contextRef && assistantString(operation.Parameters, "workflowRef") == ""
		case "WORKFLOW":
			return contextRef != "" && assistantString(operation.Parameters, "workflowRef") == contextRef && assistantString(operation.Parameters, "agentRef") == ""
		case "INTEGRATION_CONNECTION":
			return contextRef != "" && assistantString(operation.Parameters, "connectionRef") == contextRef
		default:
			return false
		}
	case "UPDATE_SCHEDULE":
		return contextKind == "SCHEDULE" && contextRef != "" && assistantString(operation.Parameters, "scheduleRef") == contextRef
	default:
		return true
	}
}

// hydrateAssistantOperation defaults Parameters from Input, bounds the field count and dispatches
// specialized hydrators for protected file/assistant/configuration/image/grant kinds. Generic
// creates receive server-built preview metadata; supported updates read owner snapshots and
// preserve exact project binding. This preparation uses the caller transaction and does not
// persist or independently complete authorization of every command.
// hydrateAssistantOperation подставляет Input вместо отсутствующих Parameters, ограничивает число
// полей и направляет защищённые file/assistant/configuration/image/grant виды специализированным
// hydrators. Обычные create получают серверные preview metadata; поддерживаемые update читают
// owner snapshots и сохраняют точную связь с проектом. Подготовка использует транзакцию
// вызывающего, ничего не сохраняет и не завершает самостоятельно авторизацию каждой команды.
func (repository *Repository) hydrateAssistantOperation(
	ctx context.Context,
	tx pgx.Tx,
	actorScope scope,
	projectRef string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if operation.Parameters == nil {
		operation.Parameters = operation.Input
	}
	if operation.Parameters == nil || len(operation.Parameters) > 100 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if operation.Type == createProjectFileRevision {
		return repository.hydrateAssistantFileRevision(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == prepareProjectAssistantConnection {
		return repository.hydrateProjectAssistantConnection(ctx, tx, actorScope, operation)
	}
	if operation.Type == changeProjectAssistantIntegrationGrant {
		return repository.hydrateProjectAssistantIntegrationGrant(ctx, tx, actorScope, operation)
	}
	if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
		return repository.hydrateSystemAssistantIntegrationGrant(ctx, tx, actorScope, operation)
	}
	if assistantProjectConfigurationOperation(operation) {
		return repository.hydrateProjectAssistantConfiguration(ctx, tx, actorScope, operation)
	}
	if operation.Type == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
		return repository.hydrateSystemAssistantImage(ctx, tx, actorScope, operation)
	}
	if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
		return repository.hydrateAssistantRuntimeConfiguration(ctx, tx, actorScope, operation)
	}
	if operation.Type == "CREATE_ROLE_IMAGE_RECIPE" {
		return repository.hydrateAssistantRoleImage(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "UPDATE_ROLE_IMAGE_RECIPE" {
		return repository.hydrateAssistantRoleImageUpdate(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "PUBLISH_INTEGRATION_DEFINITION" {
		return repository.hydrateAssistantIntegrationDefinitionPublication(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
		return repository.hydrateAssistantSystemInstructions(ctx, tx, actorScope, operation)
	}
	if operation.Type == "CREATE_PROJECT_ASSISTANT" {
		requestedProjectRef := assistantString(operation.Parameters, "projectRef")
		if projectRef != "" {
			if requestedProjectRef != "" && requestedProjectRef != "current" && requestedProjectRef != projectRef {
				return entity.AssistantPlanOperation{}, errs.ErrForbidden
			}
			requestedProjectRef = projectRef
		}
		if requestedProjectRef == "" || requestedProjectRef == "current" {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		_, target, err := repository.resolveCommandTarget(ctx, tx, actorScope, "project.manage", "PROJECT", requestedProjectRef, requestedProjectRef)
		if err != nil {
			return entity.AssistantPlanOperation{}, err
		}
		if err := repository.requireAccess(ctx, tx, actorScope, "project.manage", target); err != nil {
			return entity.AssistantPlanOperation{}, err
		}
		operation.Parameters = cloneAssistantFields(operation.Parameters)
		operation.Parameters["projectRef"] = requestedProjectRef
	}

	if targetKind, targetName, ok := assistantCreateTarget(operation.Type, operation.Parameters); ok {
		if operation.Type == "CREATE_AGENT" {
			operation.Parameters = withAssistantAgentTemplateContext(operation.Parameters)
		}
		operation.Action = "CREATE"
		operation.Target = entity.AssistantPlanTarget{Kind: targetKind, Name: targetName}
		operation.Before = map[string]any{}
		operation.After = cloneAssistantFields(operation.Parameters)
		operation.Selected = true
		return operation, nil
	}
	if operation.Type == "UPDATE_AGENT" {
		return repository.hydrateAssistantAgentOperation(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "CREATE_INSTRUCTION_DRAFT" {
		return repository.hydrateAssistantInstructionDraft(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "BIND_AGENT_RUNTIME_ENVIRONMENT" {
		return repository.hydrateAssistantAgentEnvironmentBinding(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "CHANGE_CAPABILITY" {
		return repository.hydrateAssistantAgentCapability(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "CHANGE_INTEGRATION_GRANT" {
		return repository.hydrateAssistantIntegrationGrant(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "UPDATE_WORKFLOW" {
		return repository.hydrateAssistantWorkflowOperation(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
		return repository.hydrateAssistantEnvironmentOperation(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type == "UPDATE_INTEGRATION_CONNECTION" {
		return repository.hydrateAssistantConnectionOperation(ctx, tx, actorScope, operation)
	}
	if operation.Type == "UPDATE_SCHEDULE" {
		return repository.hydrateAssistantScheduleOperation(ctx, tx, actorScope, projectRef, operation)
	}
	if operation.Type != "UPDATE_PROJECT" {
		return operation, nil
	}
	if !onlyAssistantFields(operation.Parameters, "projectRef", "name", "purpose", "language") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	requestedProjectRef := assistantString(operation.Parameters, "projectRef")
	if projectRef == "" || requestedProjectRef != "" && requestedProjectRef != "current" && requestedProjectRef != projectRef {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}

	var name, purpose, language string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectProject,
		actorScope.organizationID, projectRef,
	).Scan(&name, &purpose, &language, &version); errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	} else if err != nil {
		return entity.AssistantPlanOperation{}, errs.ErrUnavailable
	}
	return hydrateAssistantProjectOperation(projectRef, name, purpose, language, version, operation)
}

// hydrateAssistantRoleImage requires a project-bound viewable agent, reads its role/version and
// resolves a catalog-approved managed recipe. It preserves an explicitly proposed Dockerfile
// across recommended-environment selection, bounds its size and builds CREATE preview metadata
// with agent pins; provider build/admission and write authorization happen later.
// hydrateAssistantRoleImage требует доступного для просмотра агента заданного проекта, читает его
// роль/версию и разрешает одобренный каталогом managed recipe. Сохраняет предложенный Dockerfile
// при выборе рекомендуемого окружения, ограничивает размер и строит CREATE preview с agent pins;
// build/admission провайдера и write authorization выполняются позднее.
func (repository *Repository) hydrateAssistantRoleImage(
	ctx context.Context, tx pgx.Tx, actorScope scope, projectRef string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "projectRef", "agentRef", "name", "environmentKey", "dockerfile") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	requestedProject := assistantString(operation.Parameters, "projectRef")
	if requestedProject != "" && requestedProject != "current" && requestedProject != projectRef {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	agentRef, name := assistantString(operation.Parameters, "agentRef"), assistantString(operation.Parameters, "name")
	if agentRef == "" || name == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	_, target, err := repository.resolveCommandTarget(ctx, tx, actorScope, "agent.view", "AGENT", agentRef, projectRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if err := repository.requireAccess(ctx, tx, actorScope, "agent.view", target); err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	var roleRef string
	var agentVersion int64
	if err := tx.QueryRow(ctx, queryConfigurationAssistantRoleImageAgent, actorScope.organizationID, projectRef, agentRef).Scan(&roleRef, &agentVersion); errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	} else if err != nil {
		return entity.AssistantPlanOperation{}, errs.ErrUnavailable
	}
	selection := entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Parameters, "environmentKey")}
	if raw, supplied := operation.Parameters["dockerfile"]; supplied {
		value, valid := raw.(string)
		if !valid || len(value) > 64<<10 {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		selection.Dockerfile = value
	}
	if selection.EnvironmentKey == "" {
		if repository.roleImageRecommendedSelection == nil {
			return entity.AssistantPlanOperation{}, errs.ErrUnavailable
		}
		var err error
		proposedDockerfile := selection.Dockerfile
		selection, err = repository.roleImageRecommendedSelection()
		if err != nil {
			return entity.AssistantPlanOperation{}, err
		}
		if proposedDockerfile != "" {
			selection.Dockerfile = proposedDockerfile
		}
	}
	if repository.roleImageCatalogResolver == nil {
		return entity.AssistantPlanOperation{}, errs.ErrUnavailable
	}
	recipe, err := repository.roleImageCatalogResolver(selection)
	if err != nil || roleimageservice.ValidateManagedRecipe(projectRef, roleRef, name, recipe) != nil {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	parameters := map[string]any{"projectRef": projectRef, "agentRef": agentRef, "agentVersion": agentVersion,
		"name": name, "environmentKey": selection.EnvironmentKey, "dockerfile": recipe.Dockerfile}
	operation.Parameters = parameters
	operation.Action = "CREATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Name: name}
	operation.Before = map[string]any{}
	operation.After = cloneAssistantFields(parameters)
	operation.Selected = true
	return operation, nil
}

// hydrateAssistantAgentOperation validates the closed agent-update fields and nonempty
// project/ref, reads the current organization/project agent snapshot and delegates string-change
// hydration. NoRows becomes NotFound and other read failures Unavailable; caller command
// authorization remains separate.
// hydrateAssistantAgentOperation проверяет закрытый набор agent-update полей и непустые
// project/ref, читает текущий снимок агента организации/проекта и делегирует применение строковых
// изменений. NoRows даёт NotFound, прочий отказ чтения — Unavailable; авторизация команды остаётся
// отдельной.
func (repository *Repository) hydrateAssistantAgentOperation(
	ctx context.Context,
	tx pgx.Tx,
	machineScope scope,
	projectRef string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "agentRef", "name", "purpose", "roleDescription") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	agentRef := assistantString(operation.Parameters, "agentRef")
	if agentRef == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	var name, purpose, roleDescription, avatarURL string
	var version int64
	if err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgent,
		machineScope.organizationID, projectRef, agentRef,
	).Scan(&name, &purpose, &roleDescription, &avatarURL, &version); errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	} else if err != nil {
		return entity.AssistantPlanOperation{}, errs.ErrUnavailable
	}
	return hydrateAssistantAgentFields(agentRef, name, purpose, roleDescription, avatarURL, version, operation)
}

// hydrateAssistantAgentFields builds complete agent before/after snapshots, applying only
// name/purpose/roleDescription string changes while preserving avatar and server identity/version.
// Invalid supplied strings fail Invalid and unchanged edits fail Conflict; the selected UPDATE
// envelope copies After while Parameters uses the changed snapshot.
// hydrateAssistantAgentFields строит полные before/after снимки агента, применяя лишь строковые
// name/purpose/roleDescription с сохранением avatar и серверных identity/version. Некорректная
// строка даёт Invalid, отсутствие изменений — Conflict; выбранный UPDATE envelope копирует After,
// Parameters использует изменённый снимок.
func hydrateAssistantAgentFields(
	agentRef, name, purpose, roleDescription, avatarURL string,
	version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	before := map[string]any{"agentRef": agentRef, "name": name, "purpose": purpose,
		"roleDescription": roleDescription, "avatarUrl": avatarURL}
	after, err := assistantChangedStringFields(before, operation.Parameters, "name", "purpose", "roleDescription")
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "AGENT", Ref: agentRef, Name: name, Version: &version}
	operation.Parameters = after
	operation.Before = before
	operation.After = cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

// hydrateAssistantAgentCapability validates project/agent/capability and a typed enabled flag,
// reads current version/capability existence and rejects a missing capability or unchanged toggle.
// It delegates preview construction without updating the agent or treating the snapshot as fresh
// execution authority.
// hydrateAssistantAgentCapability проверяет project/agent/capability и типизированный enabled,
// читает текущую версию/существование capability, отклоняет отсутствие либо неизменённое
// переключение. Делегирует построение preview без изменения агента и признания снимка свежими
// полномочиями исполнения.
func (repository *Repository) hydrateAssistantAgentCapability(
	ctx context.Context,
	tx pgx.Tx,
	actorScope scope,
	projectRef string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "agentRef", "capabilityKey", "enabled") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	agentRef, capabilityKey := assistantString(operation.Parameters, "agentRef"), assistantString(operation.Parameters, "capabilityKey")
	enabled, enabledOK := assistantBoolValue(operation.Parameters, "enabled")
	if agentRef == "" || !enabledOK || !validInitialAgentCapabilities([]string{capabilityKey}) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	var name string
	var version int64
	var capabilities []string
	var capabilityExists bool
	if err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgentCapability,
		actorScope.organizationID, projectRef, agentRef, capabilityKey,
	).Scan(&name, &version, &capabilities, &capabilityExists); errors.Is(err, pgx.ErrNoRows) {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	} else if err != nil {
		return entity.AssistantPlanOperation{}, errs.ErrUnavailable
	}
	if !capabilityExists {
		return entity.AssistantPlanOperation{}, errs.ErrNotFound
	}
	currentlyEnabled := contains(capabilities, capabilityKey)
	if currentlyEnabled == enabled {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	return hydrateAssistantAgentCapabilityFields(agentRef, name, capabilityKey, currentlyEnabled, enabled, version, operation)
}

// hydrateAssistantAgentCapabilityFields requires a valid named/versioned agent and an actual
// capability toggle, then builds UPDATE before/after metadata and minimal command parameters. It
// pins the supplied version but performs neither database reads nor permission evaluation.
// hydrateAssistantAgentCapabilityFields требует корректного именованного/версионного агента и
// реального переключения capability, строит UPDATE before/after и минимальные параметры команды.
// Фиксирует переданную версию, но не читает БД и не проверяет полномочия.
func hydrateAssistantAgentCapabilityFields(agentRef, name, capabilityKey string, currentlyEnabled, enabled bool, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if agentRef == "" || name == "" || version < 1 || !validInitialAgentCapabilities([]string{capabilityKey}) || currentlyEnabled == enabled {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before := map[string]any{"agentRef": agentRef, "name": name, "capabilityKey": capabilityKey, "enabled": currentlyEnabled}
	after := cloneAssistantFields(before)
	after["enabled"] = enabled
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "AGENT", Ref: agentRef, Name: name, Version: &version}
	operation.Parameters = map[string]any{"agentRef": agentRef, "capabilityKey": capabilityKey, "enabled": enabled}
	operation.Before = before
	operation.After = after
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

// assistantIntegrationGrantSnapshot holds connection/recipient/grant versions, current eligibility
// reason and exact definition/approval metadata used to build a grant-change preview. Descriptive
// names and pins do not themselves authorize changing the grant.
// assistantIntegrationGrantSnapshot хранит версии connection/recipient/grant, текущую причину
// eligibility и точные definition/approval metadata для preview изменения grant. Описательные
// имена и pins сами не разрешают изменение.
type assistantIntegrationGrantSnapshot struct {
	connectionName, recipientName, reason              string
	connectionVersion, recipientVersion                int64
	grantRef, approvalPolicy, selectedApprovalPolicy   string
	grantVersion                                       int64
	definitionKey, definitionVersion, definitionDigest string
	defaultApprovalPolicy                              string
	allowedApprovalPolicies                            []string
	enabled                                            bool
	approvalScopePaths                                 []string
}

// hydrateAssistantIntegrationGrant validates exactly one agent/workflow recipient, reads an
// actor/project-scoped grant snapshot and requires READY only when enabling. It validates the
// pinned package approval policy and bounded scope paths, preserves existing paths for unchanged
// enabled policy and rejects exact no-ops before building the versioned preview.
// hydrateAssistantIntegrationGrant проверяет ровно одного agent/workflow получателя, читает grant
// snapshot с областью субъекта/проекта и требует READY лишь при включении. Проверяет approval
// policy pinned package и ограниченные scope paths, сохраняет прежние paths для неизменённой
// включённой policy, отклоняет точный no-op до построения версионного preview.
func (repository *Repository) hydrateAssistantIntegrationGrant(
	ctx context.Context,
	tx pgx.Tx,
	actorScope scope,
	projectRef string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if projectRef == "" || !onlyAssistantFields(operation.Parameters, "connectionRef", "capabilityKey", "agentRef", "workflowRef", "enabled", "approvalScopePaths", "approvalPolicy") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	connectionRef := assistantString(operation.Parameters, "connectionRef")
	capabilityKey := assistantString(operation.Parameters, "capabilityKey")
	agentRef, workflowRef := assistantString(operation.Parameters, "agentRef"), assistantString(operation.Parameters, "workflowRef")
	enabled, enabledOK := assistantBoolValue(operation.Parameters, "enabled")
	selectedPolicy := assistantString(operation.Parameters, "approvalPolicy")
	if connectionRef == "" || !validCapabilityKey(capabilityKey) || !enabledOK || !validIntegrationApprovalPolicy(selectedPolicy) || (agentRef == "") == (workflowRef == "") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	recipientKind, recipientRef := "AGENT", agentRef
	if workflowRef != "" {
		recipientKind, recipientRef = "WORKFLOW", workflowRef
	}
	snapshot, err := repository.readAssistantIntegrationGrantSnapshot(ctx, tx, actorScope, projectRef,
		connectionRef, capabilityKey, recipientKind, recipientRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if enabled && snapshot.reason != "READY" {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	definition, err := repository.integrationPackage(ctx, tx, actorScope.organizationID, connectionRef,
		snapshot.definitionKey, snapshot.definitionVersion, snapshot.definitionDigest)
	capability, found := definition.Capability(capabilityKey)
	if err != nil || !found || !capability.AllowsApprovalPolicy(selectedPolicy) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	snapshot.selectedApprovalPolicy = selectedPolicy
	snapshot.defaultApprovalPolicy = capability.ApprovalPolicy
	snapshot.allowedApprovalPolicies = append([]string{}, capability.AllowedApprovalPolicies...)
	scopePaths := []string{}
	if _, supplied := operation.Parameters["approvalScopePaths"]; supplied {
		var ok bool
		scopePaths, ok = assistantStringsValue(operation.Parameters, "approvalScopePaths")
		if !ok || len(scopePaths) > 16 || !enabled && len(scopePaths) != 0 {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	} else if enabled && snapshot.enabled && selectedPolicy == snapshot.approvalPolicy {
		scopePaths = append(scopePaths, snapshot.approvalScopePaths...)
	}
	if enabled && selectedPolicy == "HUMAN_SCOPED" {
		if capability.ValidateApprovalScopePaths(scopePaths) != nil {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	} else if len(scopePaths) != 0 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if snapshot.enabled == enabled && snapshot.approvalPolicy == selectedPolicy && reflect.DeepEqual(snapshot.approvalScopePaths, scopePaths) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	return hydrateAssistantIntegrationGrantFields(connectionRef, capabilityKey, agentRef, workflowRef,
		snapshot, enabled, scopePaths, operation)
}

// readAssistantIntegrationGrantSnapshot reads the exact
// organization/actor/authority-project/connection/recipient/capability projection, validates
// nonempty names and positive owner versions and normalizes nil approval paths to empty. NoRows
// becomes NotFound and other failures Unavailable; a partial internal snapshot may accompany an
// error and must be discarded by callers.
// readAssistantIntegrationGrantSnapshot читает точную проекцию
// organization/actor/authority-project/connection/recipient/capability, проверяет непустые имена и
// положительные owner versions, заменяет nil approval paths на пустые. NoRows даёт NotFound,
// прочие ошибки — Unavailable; при ошибке может возвращаться частичный внутренний снимок, который
// вызывающий обязан отбросить.
func (repository *Repository) readAssistantIntegrationGrantSnapshot(
	ctx context.Context,
	tx pgx.Tx,
	actorScope scope,
	projectRef, connectionRef, capabilityKey, recipientKind, recipientRef string,
) (assistantIntegrationGrantSnapshot, error) {
	result := assistantIntegrationGrantSnapshot{}
	err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectIntegrationGrant,
		actorScope.organizationID, actorScope.actorID, actorScope.authorityProjectID,
		connectionRef, projectRef, recipientKind, recipientRef, capabilityKey,
	).Scan(&result.connectionName, &result.connectionVersion, &result.recipientName, &result.recipientVersion,
		&result.reason, &result.enabled, &result.approvalScopePaths,
		&result.grantRef, &result.grantVersion, &result.approvalPolicy,
		&result.definitionKey, &result.definitionVersion, &result.definitionDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	if result.connectionName == "" || result.recipientName == "" || result.connectionVersion < 1 || result.recipientVersion < 1 {
		return result, errs.ErrUnavailable
	}
	if result.approvalScopePaths == nil {
		result.approvalScopePaths = []string{}
	}
	return result, nil
}

// hydrateAssistantIntegrationGrantFields validates supplied snapshot/recipient/approval shape and
// builds UPDATE metadata pinned to the connection version. Before/After copy approval paths and
// include recipient/grant/definition policy pins; command parameters contain only the selected
// recipient and requested change. Package policy and current authorization belong to callers.
// hydrateAssistantIntegrationGrantFields проверяет форму snapshot/recipient/approval и строит
// UPDATE metadata с версией connection. Before/After копируют approval paths и содержат
// recipient/grant/definition policy pins; параметры команды включают лишь выбранного получателя и
// изменение. Package policy и текущая authorization принадлежат вызывающим.
func hydrateAssistantIntegrationGrantFields(
	connectionRef, capabilityKey, agentRef, workflowRef string,
	snapshot assistantIntegrationGrantSnapshot,
	enabled bool,
	approvalScopePaths []string,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if connectionRef == "" || !validCapabilityKey(capabilityKey) || snapshot.connectionName == "" || snapshot.recipientName == "" ||
		snapshot.connectionVersion < 1 || snapshot.recipientVersion < 1 || (agentRef == "") == (workflowRef == "") ||
		len(approvalScopePaths) > 16 || !enabled && len(approvalScopePaths) != 0 || !validIntegrationApprovalPolicy(snapshot.selectedApprovalPolicy) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	parameters := map[string]any{"connectionRef": connectionRef, "capabilityKey": capabilityKey,
		"enabled": enabled, "approvalScopePaths": append([]string{}, approvalScopePaths...), "approvalPolicy": snapshot.selectedApprovalPolicy}
	if agentRef != "" {
		parameters["agentRef"] = agentRef
	} else {
		parameters["workflowRef"] = workflowRef
	}
	before := cloneAssistantFields(parameters)
	before["enabled"] = snapshot.enabled
	before["approvalPolicy"] = snapshot.approvalPolicy
	before["approvalScopePaths"] = append([]string{}, snapshot.approvalScopePaths...)
	before["recipientName"] = snapshot.recipientName
	before["recipientVersion"] = snapshot.recipientVersion
	after := cloneAssistantFields(parameters)
	after["recipientName"] = snapshot.recipientName
	after["recipientVersion"] = snapshot.recipientVersion
	for _, fields := range []map[string]any{before, after} {
		fields["grantRef"], fields["grantVersion"] = snapshot.grantRef, snapshot.grantVersion
		fields["definitionKey"], fields["definitionVersion"], fields["definitionDigest"] = snapshot.definitionKey, snapshot.definitionVersion, snapshot.definitionDigest
		fields["defaultApprovalPolicy"] = snapshot.defaultApprovalPolicy
		fields["allowedApprovalPolicies"] = append([]string{}, snapshot.allowedApprovalPolicies...)
	}
	version := snapshot.connectionVersion
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "INTEGRATION_CONNECTION", Ref: connectionRef, Name: snapshot.connectionName, Version: &version}
	operation.Parameters = parameters
	operation.Before = before
	operation.After = after
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

// hydrateAssistantConnectionOperation restricts editable connection fields, resolves exact
// assistant-context eligibility, reads connection/package/grants and builds a changed preview,
// then validates public configuration against the pinned definition. It preserves the existing
// full connection read path and performs no provider test or persistence.
// hydrateAssistantConnectionOperation ограничивает изменяемые поля connection, разрешает точную
// assistant-context eligibility, читает connection/package/grants и строит изменённый preview,
// затем проверяет public configuration по pinned definition. Сохраняет прежний полный путь чтения
// connection, не тестирует провайдера и не сохраняет изменения.
func (repository *Repository) hydrateAssistantConnectionOperation(
	ctx context.Context, tx pgx.Tx, actorScope scope, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters, "connectionRef", "name", "publicConfiguration") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	ref := assistantString(operation.Parameters, "connectionRef")
	if ref == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if _, err := repository.resolveAssistantContext(ctx, tx, actorScope,
		entity.AssistantContextDescriptor{EntityKind: "INTEGRATION_CONNECTION", EntityRef: ref}, ""); err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	connection, err := readConnection(ctx, tx, actorScope, ref)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	hydrated, err := hydrateAssistantConnectionFields(connection, operation)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if err := repository.validateAssistantConnectionConfiguration(ctx, tx, actorScope, connection, hydrated.After); err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrated, nil
}

// validateAssistantConnectionConfiguration loads the connection exact definition package and
// requires a typed string-valued public configuration accepted by its schema. It returns package
// read errors unchanged and malformed/configuration rejection as Invalid without accessing
// credential values.
// validateAssistantConnectionConfiguration загружает точный definition package connection и
// требует типизированную public configuration со строковыми значениями, принятую схемой.
// Возвращает исходные ошибки package read, неверную форму/отказ конфигурации — как Invalid без
// доступа к значениям credentials.
func (repository *Repository) validateAssistantConnectionConfiguration(ctx context.Context, tx pgx.Tx, actorScope scope,
	connection entity.IntegrationConnection, after map[string]any,
) error {
	definition, err := repository.integrationPackage(ctx, tx, actorScope.organizationID, connection.Ref,
		connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
	if err != nil {
		return err
	}
	raw, ok := assistantObjectValue(after, "publicConfiguration")
	configuration, valid := integrationStringConfiguration(raw)
	if !ok || !valid || definition.ValidateConfiguration(configuration) != nil {
		return errs.ErrInvalid
	}
	return nil
}

// hydrateAssistantConnectionFields copies the complete public connection snapshot, validates
// optional nonempty bounded name/string configuration changes and rejects no-op edits. It creates
// a version-pinned UPDATE preview with preserved definition identity; nested maps remain shallow
// references and package schema validation is separate.
// hydrateAssistantConnectionFields копирует полный публичный снимок connection, проверяет
// необязательные непустые ограниченные изменения имени/строковой конфигурации, отклоняет no-op.
// Создаёт UPDATE preview с версией и сохранённым definition identity; вложенные карты остаются
// поверхностными ссылками, package schema проверяется отдельно.
func hydrateAssistantConnectionFields(connection entity.IntegrationConnection, operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	before := map[string]any{"connectionRef": connection.Ref, "definitionKey": connection.DefinitionKey,
		"name": connection.Name, "publicConfiguration": connection.PublicConfiguration}
	after := cloneAssistantFields(before)
	changed := false
	if value, supplied := operation.Parameters["name"]; supplied {
		name, ok := value.(string)
		name = strings.TrimSpace(name)
		if !ok || name == "" || len(name) > 160 {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["name"] = name
		changed = changed || name != connection.Name
	}
	if value, supplied := operation.Parameters["publicConfiguration"]; supplied {
		configuration, ok := value.(map[string]any)
		if !ok || len(configuration) > 100 {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		if _, valid := integrationStringConfiguration(configuration); !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["publicConfiguration"] = configuration
		changed = changed || !reflect.DeepEqual(configuration, connection.PublicConfiguration)
	}
	if !changed {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	version := connection.Version
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "INTEGRATION_CONNECTION", Ref: connection.Ref, Name: connection.Name, Version: &version}
	operation.Parameters = after
	operation.Before = before
	operation.After = cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

// withAssistantAgentTemplateContext prepends missing organization/project/agent template lines and
// an integration loop to instruction text using literal marker checks. It returns the original map
// when no prefix is needed, otherwise a shallow copy; this compatibility enrichment is not
// semantic template validation or an authority source.
// withAssistantAgentTemplateContext добавляет недостающие строки organization/project/agent и
// integration loop перед инструкцией по буквальным marker checks. Возвращает исходную карту без
// нужды в префиксе, иначе поверхностную копию; это дополнение совместимости, не семантическая
// валидация шаблона и не источник полномочий.
func withAssistantAgentTemplateContext(parameters map[string]any) map[string]any {
	instructions, ok := parameters["instructions"].(string)
	if !ok {
		return parameters
	}
	context := []struct{ variable, label string }{
		{"organization.name", "Организация"},
		{"project.name", "Проект"},
		{"agent.name", "Сотрудник"},
	}
	lines := make([]string, 0, len(context))
	for _, item := range context {
		if !strings.Contains(instructions, "{{ ."+item.variable+" }}") {
			lines = append(lines, item.label+": {{ ."+item.variable+" }}")
		}
	}
	if len(lines) == 0 {
		lines = nil
	}
	result := cloneAssistantFields(parameters)
	prefix := ""
	if len(lines) != 0 {
		prefix = strings.Join(lines, "\n") + "\n\n"
	}
	if !strings.Contains(instructions, ".integrations.items") {
		prefix += "Доступные в текущем запуске интеграции:\n" +
			"{{ range .integrations.items }}- {{ .name }}: {{ .description }} (возможность: {{ .capability }})\n{{ else }}- Интеграции не предоставлены.\n{{ end }}\n"
	}
	if prefix == "" {
		return parameters
	}
	result["instructions"] = prefix + "\n" + instructions
	return result
}

// hydrateAssistantProjectOperation builds a complete project snapshot and applies only
// name/purpose/language string edits through the shared change helper. It preserves resolved
// project identity/version and returns Invalid or Conflict for malformed/no-op changes; final
// language policy remains in specialized command validation.
// hydrateAssistantProjectOperation строит полный снимок проекта и применяет лишь строковые
// name/purpose/language через общий helper изменений. Сохраняет разрешённые identity/version
// проекта, возвращает Invalid либо Conflict для неверных/no-op изменений; итоговая политика языка
// остаётся в специализированной валидации команды.
func hydrateAssistantProjectOperation(
	projectRef, name, purpose, language string,
	version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	before := map[string]any{"projectRef": projectRef, "name": name, "purpose": purpose, "language": language}
	after, err := assistantChangedStringFields(before, operation.Parameters, "name", "purpose", "language")
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "PROJECT", Ref: projectRef, Name: name, Version: &version}
	operation.Parameters = after
	operation.Before = before
	operation.After = cloneAssistantFields(after)
	operation.ExpectedVersion = &version
	operation.Selected = true
	return operation, nil
}

// assistantCreateTarget maps supported create-operation kinds to preview resource kinds and a
// trimmed name, using fileName for artifacts. Unknown kinds return an unsupported result; this
// mapping neither creates the resource nor authorizes it.
// assistantCreateTarget сопоставляет поддерживаемые create виды с preview resource kinds и
// очищенным именем, используя fileName для артефакта. Неизвестный вид возвращает отсутствие
// поддержки; сопоставление не создаёт ресурс и не авторизует его.
func assistantCreateTarget(operationType string, parameters map[string]any) (string, string, bool) {
	var kind string
	switch operationType {
	case "CREATE_PROJECT":
		kind = "PROJECT"
	case "CREATE_PROJECT_FILE":
		return "ARTIFACT", assistantString(parameters, "fileName"), true
	case "CREATE_AGENT":
		kind = "AGENT"
	case "CREATE_PROJECT_ASSISTANT":
		kind = "PROJECT_ASSISTANT"
	case "CREATE_WORKFLOW":
		kind = "WORKFLOW"
	case "CREATE_INTEGRATION_CONNECTION":
		kind = "INTEGRATION_CONNECTION"
	case "CREATE_SCHEDULE":
		kind = "SCHEDULE"
	case "CREATE_RUNTIME_ENVIRONMENT_DRAFT":
		kind = "RUNTIME_ENVIRONMENT_DRAFT"
	case "CREATE_ROLE_IMAGE_RECIPE":
		kind = "ROLE_IMAGE_RECIPE"
	case "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE":
		kind = "ROLE_IMAGE_RECIPE"
	default:
		return "", "", false
	}
	return kind, assistantString(parameters, "name"), true
}

// assistantChangedStringFields makes a shallow copy of the owner before snapshot and applies only
// supplied fields from the caller ordered string allowlist, trimming and requiring each supplied
// value to be nonblank. It returns nil/Invalid on malformed input or nil/Conflict when no selected
// value changes, preserving both source maps and all unselected owner fields. Nested values remain
// shallow references; this helper neither validates command policy nor assigns target/version
// authority.
// assistantChangedStringFields создаёт поверхностную копию owner before snapshot и применяет лишь
// переданные поля упорядоченного строкового списка вызывающего, очищая значения и требуя непустую
// строку. Возвращает nil/Invalid при неверном поле либо nil/Conflict без изменения выбранных
// значений, сохраняя обе исходные карты и все невыбранные owner fields. Вложенные значения
// остаются поверхностными ссылками; helper не проверяет политику команды и не назначает
// target/version authority.
func assistantChangedStringFields(before, parameters map[string]any, fields ...string) (map[string]any, error) {
	after := cloneAssistantFields(before)
	changed := false
	for _, field := range fields {
		value, exists := parameters[field]
		if !exists {
			continue
		}
		text, ok := value.(string)
		text = strings.TrimSpace(text)
		if !ok || text == "" {
			return nil, errs.ErrInvalid
		}
		after[field] = text
		changed = changed || text != before[field]
	}
	if !changed {
		return nil, errs.ErrConflict
	}
	return after, nil
}

// cloneAssistantFields makes a nonnil shallow map copy, preserving dynamic values and nested
// reference aliasing; nil input becomes an empty map rather than a deep JSON clone.
// cloneAssistantFields создаёт ненулевую поверхностную копию карты с сохранением динамических
// значений и общих вложенных ссылок; nil input превращается в пустую карту, не в глубокую JSON
// копию.
func cloneAssistantFields(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

// assistantJSONEqual compares JSON-representable values after marshal/decode with UseNumber and a
// second marshal. Object ordering is normalized but numeric spelling and unsupported values retain
// their existing behavior; any encoding/decoding error returns false rather than equality or a
// runtime error.
// assistantJSONEqual сравнивает JSON-представимые значения после marshal/decode с UseNumber и
// повторного marshal. Порядок объектов нормализуется, запись чисел и неподдерживаемые значения
// сохраняют прежнее поведение; любая ошибка кодирования/декодирования даёт false, не равенство и
// не runtime error.
func assistantJSONEqual(left, right any) bool {
	canonical := func(value any) ([]byte, error) {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var normalized any
		if err := decoder.Decode(&normalized); err != nil {
			return nil, err
		}
		return json.Marshal(normalized)
	}
	leftJSON, leftErr := canonical(left)
	rightJSON, rightErr := canonical(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

// normalizeAssistantOperation validates the closed operation kind, bounded title/summary/maps,
// expected action and kind-specific target/version/preview consistency. It strips inline file
// content from create previews, builds legacy Input plus expectedVersion and marks the shape
// Permitted with empty problems. That flag is normalization metadata; actor authorization still
// follows separately.
// normalizeAssistantOperation проверяет закрытый вид операции, ограниченные title/summary/maps,
// ожидаемое действие и соответствие target/version/preview виду. Удаляет inline file content из
// create preview, строит совместимый Input с expectedVersion и помечает форму Permitted с пустыми
// problems. Этот flag — метаданные нормализации; авторизация субъекта следует отдельно.
func normalizeAssistantOperation(operation entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if !assistantOperationType(operation.Type) || strings.TrimSpace(operation.Key) == "" ||
		strings.TrimSpace(operation.Title) == "" || utf8.RuneCountInString(operation.Title) > 200 ||
		utf8.RuneCountInString(operation.Summary) > 1000 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if operation.Parameters == nil {
		operation.Parameters = operation.Input
	}
	if operation.Parameters == nil || operation.Before == nil || operation.After == nil ||
		len(operation.Parameters) > 100 || len(operation.Before) > 100 || len(operation.After) > 100 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	expectedAction := "CREATE"
	switch operation.Type {
	case createProjectFileRevision:
		expectedAction = "UPDATE"
	case "UPDATE_PROJECT", "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "UPDATE_WORKFLOW", "PREPARE_RUNTIME_ENVIRONMENT_REVISION", "BIND_AGENT_RUNTIME_ENVIRONMENT", "UPDATE_INTEGRATION_CONNECTION", "UPDATE_SCHEDULE", "UPDATE_ROLE_IMAGE_RECIPE", "PUBLISH_INTEGRATION_DEFINITION", "CHANGE_CAPABILITY", "CHANGE_INTEGRATION_GRANT", "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", changeProjectAssistantIntegrationGrant, "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS", "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION":
		expectedAction = "UPDATE"
	case "ARCHIVE_AGENT", "ARCHIVE_WORKFLOW":
		expectedAction = "ARCHIVE"
	case "LAUNCH_RUN", "TEST_INTEGRATION_CONNECTION":
		expectedAction = "EXECUTE"
	}
	if operation.Action == "" {
		operation.Action = expectedAction
	}
	if operation.Action != expectedAction {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if expectedAction == "CREATE" && operation.Type != prepareProjectAssistantConnection {
		expectedAfter := cloneAssistantFields(operation.Parameters)
		if operation.Type == "CREATE_PROJECT_FILE" {
			delete(expectedAfter, "content")
		}
		if len(operation.Before) != 0 ||
			(!reflect.DeepEqual(operation.Parameters, operation.After) && !reflect.DeepEqual(expectedAfter, operation.After)) {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		operation.After = expectedAfter
	}
	if expectedAction == "CREATE" && operation.Type != prepareProjectAssistantConnection {
		kind, name, supported := assistantCreateTarget(operation.Type, operation.Parameters)
		if !supported || operation.Target.Kind != kind || operation.Target.Name != name || operation.Target.Ref != "" || operation.Target.Version != nil {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	if expectedAction == "UPDATE" || expectedAction == "ARCHIVE" {
		if operation.ExpectedVersion == nil || *operation.ExpectedVersion < 1 || operation.Target.Ref == "" || len(operation.Before) == 0 || len(operation.After) == 0 {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	if expectedAction == "ARCHIVE" && (len(operation.Parameters) != 0 || assistantString(operation.After, "state") != "ARCHIVED") {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	expectedTargetKind, expectedTargetRef := "", ""
	switch operation.Type {
	case createProjectFileRevision:
		expectedTargetKind, expectedTargetRef = "ARTIFACT", assistantString(operation.Parameters, "artifactRef")
		if operation.Target.Version == nil || operation.ExpectedVersion == nil || *operation.Target.Version != *operation.ExpectedVersion ||
			!assistantJSONEqual(operation.After, assistantRevisionAfter(operation)) || assistantString(operation.Before, "artifactRef") != expectedTargetRef ||
			assistantString(operation.Before, "currentRevisionRef") == "" || mustAssistantInt64(operation.Before, "version") != *operation.ExpectedVersion {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	case prepareProjectAssistantConnection:
		expectedTargetKind, expectedTargetRef = "PROJECT_ASSISTANT", assistantString(operation.Parameters, "projectAssistantRef")
		if operation.ExpectedVersion == nil || operation.Target.Version == nil || *operation.ExpectedVersion != *operation.Target.Version ||
			*operation.ExpectedVersion != mustAssistantInt64(operation.Parameters, "agentVersion") || !assistantJSONEqual(operation.After, operation.Parameters) {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		for _, field := range append([]string{"projectAssistantRef"}, projectConnectionPins...) {
			if !assistantJSONEqual(operation.Parameters[field], operation.Before[field]) {
				return entity.AssistantPlanOperation{}, errs.ErrInvalid
			}
		}
	case "UPDATE_PROJECT":
		expectedTargetKind = "PROJECT"
		expectedTargetRef = assistantString(operation.Parameters, "projectRef")
	case "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT":
		expectedTargetKind = "AGENT"
		expectedTargetRef = assistantString(operation.Parameters, "agentRef")
	case "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION":
		expectedTargetKind = "AGENT"
		expectedTargetRef = assistantString(operation.Parameters, "agentRef")
	case "UPDATE_WORKFLOW":
		expectedTargetKind = "WORKFLOW"
		expectedTargetRef = assistantString(operation.Parameters, "workflowRef")
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		expectedTargetKind = "ENVIRONMENT"
		expectedTargetRef = assistantString(operation.Parameters, "environmentRef")
	case "UPDATE_INTEGRATION_CONNECTION":
		expectedTargetKind = "INTEGRATION_CONNECTION"
		expectedTargetRef = assistantString(operation.Parameters, "connectionRef")
	case "UPDATE_SCHEDULE":
		expectedTargetKind = "SCHEDULE"
		expectedTargetRef = assistantString(operation.Parameters, "scheduleRef")
	case "UPDATE_ROLE_IMAGE_RECIPE":
		expectedTargetKind = "ROLE_IMAGE_RECIPE"
		expectedTargetRef = assistantString(operation.Parameters, "recipeRef")
	case "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE":
		expectedTargetKind = "ROLE_IMAGE_RECIPE"
		expectedTargetRef = assistantString(operation.Parameters, "recipeRef")
	case "PUBLISH_INTEGRATION_DEFINITION":
		expectedTargetKind = "INTEGRATION_DEFINITION"
		expectedTargetRef = assistantString(operation.Parameters, "configurationRef")
	case "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS":
		expectedTargetKind = "SYSTEM_ASSISTANT"
		expectedTargetRef = assistantString(operation.Parameters, "systemAssistantRef")
	case "CHANGE_CAPABILITY", "ARCHIVE_AGENT":
		expectedTargetKind = "AGENT"
		expectedTargetRef = assistantString(operation.Parameters, "agentRef")
	case "CHANGE_INTEGRATION_GRANT", "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", changeProjectAssistantIntegrationGrant, "TEST_INTEGRATION_CONNECTION":
		expectedTargetKind = "INTEGRATION_CONNECTION"
		expectedTargetRef = assistantString(operation.Parameters, "connectionRef")
	case "ARCHIVE_WORKFLOW":
		expectedTargetKind = "WORKFLOW"
	}
	if expectedTargetKind != "" && operation.Target.Kind != expectedTargetKind {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if expectedTargetRef != "" && operation.Target.Ref != expectedTargetRef {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	operation.Input = make(map[string]any, len(operation.Parameters)+1)
	for key, value := range operation.Parameters {
		operation.Input[key] = value
	}
	if operation.ExpectedVersion != nil {
		operation.Input["expectedVersion"] = *operation.ExpectedVersion
	}
	operation.TargetKind, operation.TargetRef = operation.Target.Kind, operation.Target.Ref
	operation.Permitted = true
	operation.ValidationProblems = []string{}
	return operation, nil
}

// bindAssistantOperationProject binds selected project-scoped command Input to the conversation
// project in a shallow copy, rejecting an explicit different project. Specialized
// project-configuration and self-agent operations retain their separate shape rules; it does not
// rewrite every Parameters/Before/After map or infer authority from a requested project.
// bindAssistantOperationProject связывает Input выбранных проектных команд с проектом беседы в
// поверхностной копии, отклоняя явно другой проект. Specialized project-configuration и self-agent
// операции сохраняют отдельные правила формы; не переписывает все Parameters/Before/After и не
// выводит полномочия из запрошенного проекта.
func bindAssistantOperationProject(operation entity.AssistantPlanOperation, projectRef string) (entity.AssistantPlanOperation, error) {
	if assistantProjectConfigurationOperation(operation) {
		if assistantString(operation.Parameters, "scopeKind") != "PROJECT" || assistantString(operation.Parameters, "projectRef") == "" {
			return operation, errs.ErrInvalid
		}
		return operation, nil
	}
	if operation.Type == "CREATE_PROJECT_ASSISTANT" && projectRef == "" {
		if requested := assistantString(operation.Input, "projectRef"); requested != "" && requested != "current" {
			return operation, nil
		}
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	if operation.Type == "UPDATE_AGENT" || operation.Type == "CREATE_INSTRUCTION_DRAFT" || operation.Type == "BIND_AGENT_RUNTIME_ENVIRONMENT" {
		if projectRef == "" {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		return operation, nil
	}
	switch operation.Type {
	case "UPDATE_PROJECT", "CREATE_PROJECT_FILE", "CREATE_PROJECT_ASSISTANT", "CREATE_AGENT", "CREATE_WORKFLOW", "CREATE_SCHEDULE", "LAUNCH_RUN", "CREATE_RUNTIME_ENVIRONMENT_DRAFT", "CREATE_ROLE_IMAGE_RECIPE":
	default:
		return operation, nil
	}
	if projectRef == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	requested := assistantString(operation.Input, "projectRef")
	if requested != "" && requested != "current" && requested != projectRef {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	boundInput := make(map[string]any, len(operation.Input))
	for key, value := range operation.Input {
		boundInput[key] = value
	}
	boundInput["projectRef"] = projectRef
	operation.Input = boundInput
	return operation, nil
}

// assistantOperationCommand maps the closed plan registry to specialized typed domain commands
// after per-kind field/type/size/version checks. Protected file/assistant/configuration operations
// dispatch to dedicated adapters; ordinary workflow/schedule/run construction stays specialized.
// It preserves server-assigned launch source and does not execute, persist, assign actor authority
// or bypass later command authorization.
// assistantOperationCommand преобразует закрытый реестр плана в специализированные типизированные
// доменные команды после отдельных field/type/size/version проверок. Защищённые
// file/assistant/configuration операции направляет отдельным adapters; workflow/schedule/run
// строит специализированно. Сохраняет назначенный сервером launch source, ничего не исполняет и не
// сохраняет, не назначает actor authority и не обходит последующую command authorization.
func assistantOperationCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	if operation.Type == createProjectFileRevision {
		return assistantFileRevisionCommand(operation)
	}
	if operation.Type == prepareProjectAssistantConnection {
		return projectAssistantConnectionCommand(operation)
	}
	if operation.Type == changeProjectAssistantIntegrationGrant {
		return projectAssistantIntegrationGrantCommand(operation)
	}
	if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
		return systemAssistantIntegrationGrantCommand(operation)
	}
	if assistantString(operation.Input, "projectAssistantRef") != "" {
		return projectAssistantConfigurationCommand(operation)
	}
	if operation.Type == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
		return systemAssistantImageCommand(operation)
	}
	if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
		return assistantRuntimeConfigurationCommand(operation)
	}
	if strings.TrimSpace(operation.Summary) == "" || utf8.RuneCountInString(operation.Summary) > 1000 || operation.Input == nil {
		return command.Command{}, errs.ErrInvalid
	}
	result := command.Command{}
	switch operation.Type {
	case "CREATE_PROJECT_ASSISTANT":
		if !onlyAssistantFields(operation.Input, "projectRef", "name", "purpose", "instructions") ||
			!hasAssistantFields(operation.Input, "projectRef", "name", "purpose", "instructions") {
			return command.Command{}, errs.ErrInvalid
		}
		payload := command.ProjectAssistantInput{ProjectRef: assistantString(operation.Input, "projectRef"),
			Name: assistantString(operation.Input, "name"), Purpose: assistantString(operation.Input, "purpose"),
			Instructions: assistantString(operation.Input, "instructions")}
		if payload.ProjectRef == "" || payload.Name == "" || utf8.RuneCountInString(payload.Name) > 160 ||
			payload.Purpose == "" || utf8.RuneCountInString(payload.Purpose) > 2000 || payload.Instructions == "" || utf8.RuneCountInString(payload.Instructions) > 32768 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateProjectAssistant, payload
	case "CREATE_PROJECT":
		if !onlyAssistantFields(operation.Input, "name", "purpose", "language") || !hasAssistantFields(operation.Input, "name", "purpose", "language") {
			return command.Command{}, errs.ErrInvalid
		}
		name, purpose, language := assistantString(operation.Input, "name"), assistantString(operation.Input, "purpose"), assistantString(operation.Input, "language")
		if name == "" || len(name) > 160 || purpose == "" || len(purpose) > 2000 || !contains([]string{"ru", "en"}, language) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateProject, command.ProjectInput{Name: name, Purpose: purpose, Language: language}
	case "CREATE_PROJECT_FILE":
		if assistantString(operation.Input, "contentRef") != "" {
			if !onlyAssistantFields(operation.Input, "projectRef", "fileName", "mediaType", "contentEncoding", "contentRef", "digest", "sizeBytes") ||
				!hasAssistantFields(operation.Input, "projectRef", "fileName", "mediaType", "contentEncoding", "contentRef", "digest", "sizeBytes") {
				return command.Command{}, errs.ErrInvalid
			}
			size, ok := assistantInt64(operation.Input, "sizeBytes")
			digest := assistantString(operation.Input, "digest")
			if !ok || size < 0 || size > 1<<20 || !objectDigestValid(digest) || !strings.HasPrefix(assistantString(operation.Input, "contentRef"), "pfcnt_") ||
				safeFileName(assistantString(operation.Input, "fileName")) != assistantString(operation.Input, "fileName") || assistantString(operation.Input, "projectRef") == "" {
				return command.Command{}, errs.ErrInvalid
			}
			return command.Command{Kind: command.CreateProjectFile, Payload: command.ProjectFileInput{
				ProjectRef: assistantString(operation.Input, "projectRef"), FileName: assistantString(operation.Input, "fileName"), MediaType: assistantString(operation.Input, "mediaType"),
				SHA256: strings.TrimPrefix(digest, "sha256:"), ContentRef: assistantString(operation.Input, "contentRef"), SizeBytes: size}}, nil
		}
		if !onlyAssistantFields(operation.Input, "projectRef", "fileName", "mediaType", "contentEncoding", "content") ||
			!hasAssistantFields(operation.Input, "projectRef", "fileName", "mediaType", "content") {
			return command.Command{}, errs.ErrInvalid
		}
		projectRef := assistantString(operation.Input, "projectRef")
		fileName := assistantString(operation.Input, "fileName")
		mediaType := assistantString(operation.Input, "mediaType")
		content, contentOK := operation.Input["content"].(string)
		contentEncoding := assistantString(operation.Input, "contentEncoding")
		if contentEncoding == "" {
			contentEncoding = "UTF8"
		}
		body := []byte(content)
		if contentEncoding == "BASE64" {
			body = nil
			if content != "" {
				var err error
				body, err = base64.StdEncoding.Strict().DecodeString(content)
				if err != nil || len(body) == 0 {
					return command.Command{}, errs.ErrInvalid
				}
			}
		}
		textMedia := contains([]string{"text/plain", "text/markdown", "text/csv", "application/json"}, mediaType)
		binaryMedia := contains([]string{"image/png", "image/jpeg", "image/webp", "application/pdf"}, mediaType)
		if projectRef == "" || fileName == "" || safeFileName(fileName) != fileName ||
			!contentOK || len(body) > 1<<20 ||
			(contentEncoding == "UTF8" && !textMedia) ||
			(contentEncoding == "BASE64" && !binaryMedia) ||
			(contentEncoding != "UTF8" && contentEncoding != "BASE64") {
			return command.Command{}, errs.ErrInvalid
		}
		digest := sha256.Sum256(body)
		result.Kind, result.Payload = command.CreateProjectFile, command.ProjectFileInput{
			ProjectRef: projectRef, FileName: fileName, MediaType: mediaType,
			SHA256: fmt.Sprintf("%x", digest[:]), SizeBytes: int64(len(body)), Content: body,
		}
	case "UPDATE_PROJECT":
		if !onlyAssistantFields(operation.Input, "projectRef", "name", "purpose", "language", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "projectRef", "name", "purpose", "language", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		payload := command.ProjectInput{Ref: assistantString(operation.Input, "projectRef"), Name: assistantString(operation.Input, "name"),
			Purpose: assistantString(operation.Input, "purpose"), Language: assistantString(operation.Input, "language")}
		if !expectedOK || expected < 1 || payload.Ref == "" || payload.Name == "" || len(payload.Name) > 160 ||
			payload.Purpose == "" || len(payload.Purpose) > 2000 || !contains([]string{"ru", "en"}, payload.Language) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.UpdateProject, payload
		result.Mutation.ExpectedVersion = &expected
	case "CREATE_AGENT":
		if !onlyAssistantFields(operation.Input, "projectRef", "roleDefinitionRef", "name", "purpose", "roleDescription", "avatarUrl", "runtimeRef", "instructions", "capabilities") ||
			!hasAssistantFields(operation.Input, "projectRef", "name", "purpose", "roleDescription", "instructions") {
			return command.Command{}, errs.ErrInvalid
		}
		var initialCapabilities []string
		if _, requested := operation.Input["capabilities"]; requested {
			var valid bool
			initialCapabilities, valid = assistantStringsValue(operation.Input, "capabilities")
			if !valid || !validInitialAgentCapabilities(initialCapabilities) {
				return command.Command{}, errs.ErrInvalid
			}
		}
		payload := command.AgentInput{ProjectRef: assistantString(operation.Input, "projectRef"), RoleDefinitionRef: assistantString(operation.Input, "roleDefinitionRef"),
			Name: assistantString(operation.Input, "name"), Purpose: assistantString(operation.Input, "purpose"),
			RoleDescription: assistantString(operation.Input, "roleDescription"), AvatarURL: assistantString(operation.Input, "avatarUrl"),
			RuntimeRef: assistantString(operation.Input, "runtimeRef"), Instructions: assistantString(operation.Input, "instructions"),
			InitialCapabilities: initialCapabilities}
		if payload.ProjectRef == "" || payload.Name == "" || len(payload.Name) > 160 || payload.Purpose == "" || len(payload.Purpose) > 2000 ||
			payload.RoleDescription == "" || len(payload.RoleDescription) > 2000 || len(payload.AvatarURL) > 500 || len(payload.Instructions) < 20 || len(payload.Instructions) > 65536 ||
			strings.Contains(payload.Instructions, "i18n:") || strings.Contains(payload.Instructions, "{{ index .") || !validAssistantPromptTemplate(payload.Instructions) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateAgent, payload
	case "CREATE_RUNTIME_ENVIRONMENT_DRAFT":
		if !onlyAssistantFields(operation.Input, "projectRef", "name", "description", "imageArtifactRef", "publicValues", "secretBindings", "secretSuggestions", "tools", "policy") ||
			!hasAssistantFields(operation.Input, "projectRef", "name") {
			return command.Command{}, errs.ErrInvalid
		}
		name := assistantString(operation.Input, "name")
		description := assistantString(operation.Input, "description")
		imageArtifactRef := assistantString(operation.Input, "imageArtifactRef")
		if assistantString(operation.Input, "projectRef") == "" || name == "" || len(name) > 120 || len(description) > 1000 ||
			(imageArtifactRef != "" && (!strings.HasPrefix(imageArtifactRef, "imgart_") || len(imageArtifactRef) > 96)) {
			return command.Command{}, errs.ErrInvalid
		}
		values, valuesOK := assistantEnvironmentPublicValues(operation.Input)
		bindings, bindingsOK := assistantEnvironmentSecretBindings(operation.Input, values)
		tools, toolsOK := assistantEnvironmentTools(operation.Input)
		policy, policyOK := assistantEnvironmentPolicy(operation.Input)
		if !valuesOK || !bindingsOK || !toolsOK || !policyOK || !assistantEnvironmentSecretSuggestions(operation.Input) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind = command.CreateRuntimeEnvironmentDraft
		result.Payload = command.RuntimeEnvironmentDraftInput{
			ProjectRef: assistantString(operation.Input, "projectRef"),
			Specification: entity.RuntimeEnvironmentDraftSpecification{
				Name: name, Description: description, ImageArtifactRef: imageArtifactRef,
				Values: values, SecretBindings: bindings, Tools: tools, Policy: policy,
			},
		}
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		return assistantEnvironmentRevisionCommand(operation)
	case "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS":
		return assistantSystemInstructionsCommand(operation)
	case "BIND_AGENT_RUNTIME_ENVIRONMENT":
		return assistantAgentEnvironmentBindingCommand(operation)
	case "CREATE_ROLE_IMAGE_RECIPE":
		if !onlyAssistantFields(operation.Input, "projectRef", "agentRef", "name", "environmentKey", "dockerfile", "agentVersion") ||
			!hasAssistantFields(operation.Input, "projectRef", "agentRef", "name", "environmentKey", "dockerfile", "agentVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		version, versionOK := assistantInt64(operation.Input, "agentVersion")
		dockerfile, dockerfileOK := operation.Input["dockerfile"].(string)
		payload := command.AssistantRoleImageRecipeInput{
			ProjectRef:   assistantString(operation.Input, "projectRef"),
			AgentRef:     assistantString(operation.Input, "agentRef"),
			Name:         assistantString(operation.Input, "name"),
			AgentVersion: version,
			Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Input, "environmentKey"),
				Dockerfile: dockerfile},
		}
		if !versionOK || version < 1 || !dockerfileOK || dockerfile == "" || len(dockerfile) > 64<<10 ||
			payload.ProjectRef == "" || payload.AgentRef == "" || payload.Name == "" || len(payload.Name) > 160 ||
			payload.Environment.EnvironmentKey == "" || len(payload.Environment.EnvironmentKey) > 96 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateAssistantRoleImageRecipe, payload
	case "UPDATE_ROLE_IMAGE_RECIPE":
		if !onlyAssistantFields(operation.Input, "projectRef", "recipeRef", "name", "environmentKey", "dockerfile", "specSha256", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "projectRef", "recipeRef", "name", "environmentKey", "dockerfile", "specSha256", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, valid := assistantInt64(operation.Input, "expectedVersion")
		dockerfile, dockerfileOK := operation.Input["dockerfile"].(string)
		specSHA256, _ := operation.Input["specSha256"].(string)
		payload := command.AssistantRoleImageUpdateInput{
			ProjectRef: assistantString(operation.Input, "projectRef"), RecipeRef: assistantString(operation.Input, "recipeRef"),
			Name: assistantString(operation.Input, "name"), SpecSHA256: specSHA256,
			Environment: entity.RoleEnvironmentSelection{EnvironmentKey: assistantString(operation.Input, "environmentKey"),
				Dockerfile: dockerfile},
		}
		if !valid || expected < 1 || !exactSHA256(payload.SpecSHA256) || !dockerfileOK || dockerfile == "" || len(dockerfile) > 64<<10 ||
			payload.ProjectRef == "" || payload.RecipeRef == "" ||
			payload.Name == "" || len(payload.Name) > 160 || payload.Environment.EnvironmentKey == "" || len(payload.Environment.EnvironmentKey) > 96 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.UpdateAssistantRoleImageRecipe, payload
		result.Mutation.ExpectedVersion = &expected
	case "UPDATE_AGENT":
		if !onlyAssistantFields(operation.Input, "agentRef", "name", "purpose", "roleDescription", "avatarUrl", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "agentRef", "name", "purpose", "roleDescription", "avatarUrl", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		payload := command.AgentInput{Ref: assistantString(operation.Input, "agentRef"), Name: assistantString(operation.Input, "name"),
			Purpose: assistantString(operation.Input, "purpose"), RoleDescription: assistantString(operation.Input, "roleDescription"),
			AvatarURL: assistantString(operation.Input, "avatarUrl")}
		if !expectedOK || expected < 1 || payload.Ref == "" || payload.Name == "" || len(payload.Name) > 160 ||
			payload.Purpose == "" || len(payload.Purpose) > 2000 || payload.RoleDescription == "" || len(payload.RoleDescription) > 2000 || len(payload.AvatarURL) > 500 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.UpdateAgent, payload
		result.Mutation.ExpectedVersion = &expected
	case "CREATE_INSTRUCTION_DRAFT":
		if !onlyAssistantFields(operation.Input, "agentRef", "instructions", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "agentRef", "instructions", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, valid := assistantInt64(operation.Input, "expectedVersion")
		payload := command.AgentInput{Ref: assistantString(operation.Input, "agentRef"), Instructions: assistantString(operation.Input, "instructions")}
		if !valid || expected < 1 || payload.Ref == "" || len(strings.TrimSpace(payload.Instructions)) < 20 || len(payload.Instructions) > 65536 || !validAssistantPromptTemplate(payload.Instructions) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateInstructions, payload
		result.Mutation.ExpectedVersion = &expected
	case "UPDATE_INTEGRATION_CONNECTION":
		if !onlyAssistantFields(operation.Input, "connectionRef", "definitionKey", "name", "publicConfiguration", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "connectionRef", "definitionKey", "name", "publicConfiguration", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		configuration, configurationOK := assistantObjectValue(operation.Input, "publicConfiguration")
		payload := command.ConnectionInput{Ref: assistantString(operation.Input, "connectionRef"), Name: assistantString(operation.Input, "name"), PublicConfiguration: configuration}
		if !expectedOK || expected < 1 || !configurationOK || payload.Ref == "" ||
			!validCapabilityKey(assistantString(operation.Input, "definitionKey")) || payload.Name == "" || len(payload.Name) > 160 || len(configuration) > 100 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.UpdateConnection, payload
		result.Mutation.ExpectedVersion = &expected
	case "PUBLISH_INTEGRATION_DEFINITION":
		return assistantIntegrationDefinitionPublicationCommand(operation)
	case "CREATE_WORKFLOW":
		workflow, err := assistantWorkflow(operation.Input)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.CreateWorkflow, workflow
	case "UPDATE_WORKFLOW":
		workflow, expected, err := assistantUpdateWorkflow(operation)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.UpdateWorkflow, workflow
		result.Mutation.ExpectedVersion = &expected
	case "CHANGE_CAPABILITY":
		if !onlyAssistantFields(operation.Input, "agentRef", "capabilityKey", "enabled", "expectedVersion") || !hasAssistantFields(operation.Input, "agentRef", "capabilityKey", "enabled", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, ok := assistantInt64(operation.Input, "expectedVersion")
		enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
		if !ok || expected < 1 || !enabledOK || assistantString(operation.Input, "agentRef") == "" || !validCapabilityKey(assistantString(operation.Input, "capabilityKey")) {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind = command.ChangeAgentCapability
		result.Mutation.ExpectedVersion = &expected
		result.Payload = command.AgentBindingInput{AgentRef: assistantString(operation.Input, "agentRef"), BindingRef: assistantString(operation.Input, "capabilityKey"), Enabled: enabled}
	case "CHANGE_INTEGRATION_GRANT":
		if !onlyAssistantFields(operation.Input, "connectionRef", "capabilityKey", "agentRef", "workflowRef", "enabled", "expectedVersion", "approvalScopePaths", "approvalPolicy") || !hasAssistantFields(operation.Input, "connectionRef", "capabilityKey", "enabled", "expectedVersion", "approvalPolicy") {
			return command.Command{}, errs.ErrInvalid
		}
		enabled, enabledOK := assistantBoolValue(operation.Input, "enabled")
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		payload := command.IntegrationGrantInput{ConnectionRef: assistantString(operation.Input, "connectionRef"), CapabilityKey: assistantString(operation.Input, "capabilityKey"),
			AgentRef: assistantString(operation.Input, "agentRef"), WorkflowRef: assistantString(operation.Input, "workflowRef"), Enabled: enabled, ApprovalPolicy: assistantString(operation.Input, "approvalPolicy")}
		if _, present := operation.Input["approvalScopePaths"]; present {
			paths, valid := assistantStringsValue(operation.Input, "approvalScopePaths")
			if !valid || len(paths) > 16 || !enabled && len(paths) != 0 {
				return command.Command{}, errs.ErrInvalid
			}
			payload.ApprovalScopePaths = paths
		}
		if !enabledOK || !expectedOK || expected < 1 || payload.ConnectionRef == "" || !validIntegrationApprovalPolicy(payload.ApprovalPolicy) || !validCapabilityKey(payload.CapabilityKey) || (payload.AgentRef == "") == (payload.WorkflowRef == "") {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.ChangeIntegrationGrant, payload
		result.Mutation.ExpectedVersion = &expected
	case "CREATE_INTEGRATION_CONNECTION":
		if !onlyAssistantFields(operation.Input, "definitionKey", "name", "publicConfiguration") ||
			!hasAssistantFields(operation.Input, "definitionKey", "name", "publicConfiguration") {
			return command.Command{}, errs.ErrInvalid
		}
		configuration, configurationOK := assistantObjectValue(operation.Input, "publicConfiguration")
		payload := command.ConnectionInput{DefinitionKey: assistantString(operation.Input, "definitionKey"), Name: assistantString(operation.Input, "name"), PublicConfiguration: configuration}
		if !configurationOK || !validCapabilityKey(payload.DefinitionKey) || payload.Name == "" || len(payload.Name) > 160 || len(configuration) > 100 {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.CreateConnection, payload
	case "TEST_INTEGRATION_CONNECTION":
		if !onlyAssistantFields(operation.Input, "connectionRef", "expectedVersion") ||
			!hasAssistantFields(operation.Input, "connectionRef", "expectedVersion") {
			return command.Command{}, errs.ErrInvalid
		}
		expected, expectedOK := assistantInt64(operation.Input, "expectedVersion")
		connectionRef := assistantString(operation.Input, "connectionRef")
		if !expectedOK || expected < 1 || connectionRef == "" {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.TestConnection, command.ConnectionInput{Ref: connectionRef}
		result.Mutation.ExpectedVersion = &expected
	case "ARCHIVE_AGENT":
		if !onlyAssistantFields(operation.Input, "expectedVersion") || operation.Target.Ref == "" || operation.ExpectedVersion == nil {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.ArchiveAgent, command.AgentInput{Ref: operation.Target.Ref}
		result.Mutation.ExpectedVersion = operation.ExpectedVersion
	case "ARCHIVE_WORKFLOW":
		if !onlyAssistantFields(operation.Input, "expectedVersion") || operation.Target.Ref == "" || operation.ExpectedVersion == nil {
			return command.Command{}, errs.ErrInvalid
		}
		result.Kind, result.Payload = command.ArchiveWorkflow, command.WorkflowInput{Ref: operation.Target.Ref}
		result.Mutation.ExpectedVersion = operation.ExpectedVersion
	case "CREATE_SCHEDULE":
		schedule, err := assistantSchedule(operation.Input)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.CreateSchedule, schedule
	case "UPDATE_SCHEDULE":
		schedule, expected, err := assistantUpdateSchedule(operation.Input)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.UpdateSchedule, schedule
		result.Mutation.ExpectedVersion = &expected
	case "LAUNCH_RUN":
		run, err := assistantRun(operation.Input)
		if err != nil {
			return command.Command{}, err
		}
		result.Kind, result.Payload = command.LaunchRun, run
	default:
		return command.Command{}, errs.ErrInvalid
	}
	return result, nil
}

// validAssistantPromptTemplate rejects template validation diagnostics only when severity is ERROR
// using the prompt catalog; warnings are allowed and no template execution or publication occurs.
// validAssistantPromptTemplate отклоняет diagnostics валидации шаблона только при severity ERROR
// по prompt catalog; предупреждения допускаются, исполнение и публикация не выполняются.
func validAssistantPromptTemplate(value string) bool {
	for _, diagnostic := range promptservice.Validate(value, promptservice.Catalog()) {
		if diagnostic.Severity == "ERROR" {
			return false
		}
	}
	return true
}

// assistantWorkflow decodes closed workflow/input/step shapes, assigns stable numbered keys and
// derives dependencies from sequential/parallel frontiers. It defaults unparsed
// concurrency/timeout, preserves existing int32 casts, then applies domain version validation and
// returns safe field diagnostics. It builds a draft payload rather than publishing or admitting a
// launch.
// assistantWorkflow декодирует закрытые workflow/input/step формы, назначает стабильные
// нумерованные keys и выводит зависимости из sequential/parallel frontier. Задаёт defaults для
// неразобранных concurrency/timeout, сохраняет прежние int32 casts, затем применяет доменную
// валидацию версии и безопасные field diagnostics. Строит draft payload без публикации и допуска
// запуска.
func assistantWorkflow(input map[string]any) (command.WorkflowInput, error) {
	if !onlyAssistantFields(input, "projectRef", "name", "purpose", "coordinatorAgentRef", "inputFields", "steps", "maxConcurrency", "timeoutSeconds", "completionCriteria") ||
		!hasAssistantFields(input, "projectRef", "name", "purpose", "coordinatorAgentRef", "steps") {
		return command.WorkflowInput{}, errs.ErrInvalid
	}
	projectRef, name := assistantString(input, "projectRef"), assistantString(input, "name")
	coordinator := assistantString(input, "coordinatorAgentRef")
	concurrency, concurrencyOK := assistantInt64(input, "maxConcurrency")
	timeout, timeoutOK := assistantInt64(input, "timeoutSeconds")
	if !concurrencyOK {
		concurrency = 1
	}
	if !timeoutOK {
		timeout = 7200
	}
	rawSteps, ok := input["steps"].([]any)
	if !ok || len(rawSteps) == 0 || len(rawSteps) > 200 {
		return command.WorkflowInput{}, errs.ErrInvalid
	}
	draft := entity.WorkflowVersion{Ref: "draft", Name: name, Purpose: assistantString(input, "purpose"), CoordinatorAgentRef: coordinator,
		VersionNumber: 1, Concurrency: int32(concurrency), TimeoutSeconds: timeout, CompletionCriteria: assistantString(input, "completionCriteria"), ResultSchema: map[string]any{}}
	if rawFields, exists := input["inputFields"]; exists {
		fields, ok := rawFields.([]any)
		if !ok || len(fields) > 100 {
			return command.WorkflowInput{}, errs.ErrInvalid
		}
		for index, raw := range fields {
			field, ok := raw.(map[string]any)
			if !ok || !onlyAssistantFields(field, "label", "description", "valueType", "required", "options") ||
				!hasAssistantFields(field, "label", "valueType", "required", "options") {
				return command.WorkflowInput{}, errs.ErrInvalid
			}
			required, requiredOK := assistantBoolValue(field, "required")
			options, optionsOK := assistantStringsValue(field, "options")
			if !requiredOK || !optionsOK {
				return command.WorkflowInput{}, errs.ErrInvalid
			}
			draft.Inputs = append(draft.Inputs, entity.WorkflowInputField{
				Key: "field-" + leftPad(index+1, 3), Label: assistantString(field, "label"),
				Help: assistantString(field, "description"), Type: assistantString(field, "valueType"),
				Required: required, Options: options,
			})
		}
	}
	frontier := []string{}
	parallelGroups := map[int32][]string{}
	parallelGroupLabels := map[string]int64{}
	for index, raw := range rawSteps {
		stepInput, ok := raw.(map[string]any)
		if !ok || !onlyAssistantFields(stepInput, "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys") ||
			!hasAssistantFields(stepInput, "name", "purpose", "agentRef", "parallel", "parallelGroup", "timeoutSeconds", "expectedResult", "humanGate", "gateDecisions", "requiredCapabilityKeys") {
			return command.WorkflowInput{}, errs.ErrInvalid
		}
		parallel, parallelOK := assistantBoolValue(stepInput, "parallel")
		humanGate, humanGateOK := assistantBoolValue(stepInput, "humanGate")
		parallelGroup, parallelGroupOK := assistantParallelGroup(stepInput, "parallelGroup", parallel, parallelGroupLabels)
		stepTimeout, timeoutOK := assistantInt64(stepInput, "timeoutSeconds")
		gateDecisions, gateDecisionsOK := assistantStringsValue(stepInput, "gateDecisions")
		requiredCapabilities, requiredCapabilitiesOK := assistantStringsValue(stepInput, "requiredCapabilityKeys")
		if !parallelOK || !humanGateOK || !parallelGroupOK || !timeoutOK || !gateDecisionsOK || !requiredCapabilitiesOK {
			return command.WorkflowInput{}, errs.WithAssistantPlanField(errs.ErrInvalid, "STEP_SHAPE")
		}
		key := "step-" + leftPad(index+1, 3)
		dependencies := append([]string(nil), frontier...)
		if parallel {
			group := int32(parallelGroup)
			if known, exists := parallelGroups[group]; exists {
				dependencies = append([]string(nil), known...)
			} else {
				parallelGroups[group] = append([]string(nil), frontier...)
				frontier = nil
			}
			frontier = append(frontier, key)
		} else {
			parallelGroups = map[int32][]string{}
			frontier = []string{key}
		}
		draft.Steps = append(draft.Steps, entity.WorkflowStep{Key: key, Position: int32(index + 1), Name: assistantString(stepInput, "name"),
			AgentRef: assistantString(stepInput, "agentRef"), Instructions: assistantString(stepInput, "purpose"), Parallel: parallel,
			ParallelGroup: int32(parallelGroup), TimeoutSeconds: int32(stepTimeout), ExpectedResult: assistantString(stepInput, "expectedResult"),
			HumanGateAfter: humanGate, DependsOn: dependencies,
			GateDecisions: gateDecisions, RequiredCapabilityKeys: requiredCapabilities})
	}
	if projectRef == "" || !validWorkflowVersion(draft) {
		field := "WORKFLOW_BINDING"
		if projectRef != "" {
			field = workflowVersionInvalidField(draft)
		}
		return command.WorkflowInput{}, errs.WithAssistantPlanField(errs.ErrInvalid, field)
	}
	return command.WorkflowInput{ProjectRef: projectRef, Name: name, Purpose: draft.Purpose, CoordinatorAgentRef: coordinator, Draft: &draft}, nil
}

// assistantSchedule decodes a closed schedule-create shape into a typed payload, validating
// target/preset/policy sets, bounded text and run input. It requires automation text but leaves
// authoritative cron/timezone/target execution eligibility to the command path.
// assistantSchedule декодирует закрытую schedule-create форму в типизированный payload, проверяет
// наборы target/preset/policy, ограниченные тексты и run input. Требует automation text, но
// авторитетную cron/timezone/target eligibility оставляет пути команды.
func assistantSchedule(input map[string]any) (command.ScheduleInput, error) {
	if !onlyAssistantFields(input, "projectRef", "name", "targetType", "targetRef", "preset", "cronExpression", "timeOfDay", "dayOfWeek", "timezone", "input", "automationText", "sessionPolicy", "notificationPolicy") ||
		!hasAssistantFields(input, "projectRef", "name", "targetType", "targetRef", "preset", "timeOfDay", "timezone", "input", "automationText", "sessionPolicy", "notificationPolicy") {
		return command.ScheduleInput{}, errs.ErrInvalid
	}
	boundedInput, boundedInputOK := assistantObjectValue(input, "input")
	payload := command.ScheduleInput{ProjectRef: assistantString(input, "projectRef"), Name: assistantString(input, "name"),
		Preset: assistantString(input, "preset"), CronExpression: assistantString(input, "cronExpression"), TimeOfDay: assistantString(input, "timeOfDay"), DayOfWeek: assistantString(input, "dayOfWeek"), Timezone: assistantString(input, "timezone"),
		AutomationText: assistantString(input, "automationText"),
		SessionPolicy:  assistantString(input, "sessionPolicy"), NotificationPolicy: assistantString(input, "notificationPolicy"),
		Target: entity.RunTarget{Type: assistantString(input, "targetType"), Ref: assistantString(input, "targetRef")}, Input: boundedInput}
	if !boundedInputOK || payload.ProjectRef == "" || payload.Name == "" || len(payload.Name) > 160 || !contains([]string{"AGENT", "WORKFLOW"}, payload.Target.Type) || payload.Target.Ref == "" ||
		!contains([]string{"HOURLY", "DAILY", "WEEKDAYS", "WEEKLY", "CUSTOM"}, payload.Preset) || len(payload.CronExpression) > 120 || len(payload.TimeOfDay) > 5 || len(payload.DayOfWeek) > 9 || payload.Timezone == "" || len(payload.Timezone) > 80 ||
		payload.AutomationText == "" || len(payload.AutomationText) > 32768 ||
		!contains([]string{"NEW_EACH_RUN", "CONTINUE_ONE"}, payload.SessionPolicy) || !contains([]string{"CONTROL_CENTER_ONLY", "CONTROL_CENTER_AND_OPTIONAL_CHANNELS"}, payload.NotificationPolicy) || !validBoundedRunInput(payload.Input) {
		return command.ScheduleInput{}, errs.ErrInvalid
	}
	return payload, nil
}

// assistantRun decodes a closed launch shape with bounded input/text and an AGENT or WORKFLOW
// target, retaining optional session/attachment references and assigning SYSTEM_ASSISTANT source.
// It does not authorize those references or start execution.
// assistantRun декодирует закрытую launch форму с ограниченными input/text и целью AGENT либо
// WORKFLOW, сохраняет необязательные session/attachment ссылки, назначает source SYSTEM_ASSISTANT.
// Не авторизует ссылки и не запускает исполнение.
func assistantRun(input map[string]any) (command.LaunchRunInput, error) {
	if !onlyAssistantFields(input, "projectRef", "targetType", "targetRef", "title", "task", "input", "attachmentSetRef", "sessionRef") ||
		!hasAssistantFields(input, "projectRef", "targetType", "targetRef", "title", "task", "input") {
		return command.LaunchRunInput{}, errs.ErrInvalid
	}
	boundedInput, boundedInputOK := assistantObjectValue(input, "input")
	payload := command.LaunchRunInput{ProjectRef: assistantString(input, "projectRef"), Title: assistantString(input, "title"), Task: assistantString(input, "task"),
		SessionRef: assistantString(input, "sessionRef"), Source: "SYSTEM_ASSISTANT", Input: boundedInput, AttachmentSetRef: assistantString(input, "attachmentSetRef"),
		Target: entity.RunTarget{Type: assistantString(input, "targetType"), Ref: assistantString(input, "targetRef")}}
	if !boundedInputOK || payload.ProjectRef == "" || !contains([]string{"AGENT", "WORKFLOW"}, payload.Target.Type) || payload.Target.Ref == "" || payload.Title == "" || len(payload.Title) > 240 ||
		payload.Task == "" || len(payload.Task) > 32768 || !validBoundedRunInput(payload.Input) {
		return command.LaunchRunInput{}, errs.ErrInvalid
	}
	return payload, nil
}

// assistantProjectFileContentReady accepts nonfile operations immediately and considers file
// content prepared only with a nonempty contentRef, valid digest shape and no inline Parameters
// content. It checks shape, not ledger ownership or object existence.
// assistantProjectFileContentReady сразу принимает нефайловую операцию, для файла считает
// содержимое подготовленным лишь при непустом contentRef, правильной форме digest и отсутствии
// inline content в Parameters. Проверяет форму, не владение ledger и не существование объекта.
func assistantProjectFileContentReady(operation entity.AssistantPlanOperation) bool {
	if !assistantFileOperation(operation.Type) {
		return true
	}
	return assistantString(operation.Parameters, "contentRef") != "" && objectDigestValid(assistantString(operation.Parameters, "digest")) && operation.Parameters["content"] == nil
}

// onlyAssistantFields rejects map keys outside the supplied allowlist without requiring any key or
// inspecting values; nil and empty maps pass this shape check.
// onlyAssistantFields отклоняет ключи карты вне переданного списка без требования наличия ключей и
// проверки значений; nil и пустая карта проходят эту проверку формы.
func onlyAssistantFields(input map[string]any, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range input {
		if _, ok := known[key]; !ok {
			return false
		}
	}
	return true
}

// hasAssistantFields requires map key presence for every requested field regardless of value or
// nil contents; it does not perform type or nonempty validation.
// hasAssistantFields требует наличия каждого запрошенного ключа независимо от значения и nil
// содержимого; не проверяет тип и непустоту.
func hasAssistantFields(input map[string]any, required ...string) bool {
	for _, key := range required {
		if _, ok := input[key]; !ok {
			return false
		}
	}
	return true
}

// assistantString returns a trimmed string value or empty for absent/nil/nonstring input; callers
// must separately distinguish required fields and reject invalid shapes.
// assistantString возвращает очищенное строковое значение либо пустое для
// отсутствующего/nil/нестрокового поля; вызывающий отдельно различает обязательные поля и
// отклоняет неверные формы.
func assistantString(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return strings.TrimSpace(value)
}

// assistantBoolValue extracts only a dynamic bool and reports its type assertion result without
// coercing strings/numbers or treating false as missing.
// assistantBoolValue извлекает лишь динамический bool и результат проверки типа без преобразования
// строк/чисел и без признания false отсутствующим.
func assistantBoolValue(input map[string]any, key string) (bool, bool) {
	value, ok := input[key].(bool)
	return value, ok
}

// assistantInt64 accepts int/int64, exactly round-trippable float64 conversion or an integral
// json.Number; other shapes return zero/false. It preserves existing conversion semantics rather
// than adding a separate numeric range or precision policy.
// assistantInt64 принимает int/int64, float64 с точным обратным преобразованием либо целый
// json.Number; прочие формы дают zero/false. Сохраняет прежнюю семантику преобразования без
// отдельной новой политики диапазона и точности.
func assistantInt64(input map[string]any, key string) (int64, bool) {
	switch value := input[key].(type) {
	case float64:
		integer := int64(value)
		return integer, float64(integer) == value
	case int64:
		return value, true
	case int:
		return int64(value), true
	case json.Number:
		integer, err := value.Int64()
		return integer, err == nil
	default:
		return 0, false
	}
}

// assistantParallelGroup accepts an existing numeric group unchanged or maps a bounded trimmed
// string label to a stable positive ordinal for parallel steps. Nonparallel valid labels yield
// zero; new labels mutate the shared map up to fifty entries, so callers supply a writable map.
// assistantParallelGroup принимает числовую группу без изменения либо сопоставляет ограниченную
// очищенную метку стабильному положительному номеру parallel steps. Корректная метка
// непараллельного шага даёт ноль; новая метка меняет общую карту до пятидесяти записей, вызывающий
// передаёт доступную для записи карту.
func assistantParallelGroup(input map[string]any, key string, parallel bool, labels map[string]int64) (int64, bool) {
	if value, ok := assistantInt64(input, key); ok {
		return value, true
	}
	label, ok := input[key].(string)
	label = strings.TrimSpace(label)
	if !ok || label == "" || len(label) > 80 {
		return 0, false
	}
	if !parallel {
		return 0, true
	}
	if value, exists := labels[label]; exists {
		return value, true
	}
	if len(labels) >= 50 {
		return 0, false
	}
	value := int64(len(labels) + 1)
	labels[label] = value
	return value, true
}

// assistantObjectValue returns only a dynamic map[string]any and its assertion result without
// copying or recursively validating the object.
// assistantObjectValue возвращает лишь динамическую map[string]any и результат проверки типа без
// копирования и рекурсивной валидации.
func assistantObjectValue(input map[string]any, key string) (map[string]any, bool) {
	value, ok := input[key].(map[string]any)
	return value, ok
}

// assistantStrings delegates to the strict string-slice extractor and discards its validity flag,
// returning nil on malformed/missing input; callers needing absence/error distinction use
// assistantStringsValue.
// assistantStrings делегирует строгому извлечению среза строк и отбрасывает validity flag,
// возвращая nil при неверном/отсутствующем поле; для различения отсутствия/ошибки нужен
// assistantStringsValue.
func assistantStrings(input map[string]any, key string) []string {
	result, _ := assistantStringsValue(input, key)
	return result
}

// assistantStringsValue accepts []string or []any containing only nonblank strings, trims into a
// fresh slice and rejects the entire value on a malformed item. Missing or wrong-shaped input
// returns nil/false; an existing empty slice succeeds with a nonnil empty result.
// assistantStringsValue принимает []string либо []any только с непустыми строками, очищает их в
// новый срез и отклоняет всё значение при неверном элементе. Отсутствующее либо неверное поле даёт
// nil/false; существующий пустой срез успешно даёт ненулевой пустой результат.
func assistantStringsValue(input map[string]any, key string) ([]string, bool) {
	value, exists := input[key]
	if !exists {
		return nil, false
	}
	if raw, ok := value.([]string); ok {
		result := make([]string, 0, len(raw))
		for _, item := range raw {
			item = strings.TrimSpace(item)
			if item == "" {
				return nil, false
			}
			result = append(result, item)
		}
		return result, true
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, false
		}
		result = append(result, strings.TrimSpace(value))
	}
	return result, true
}

// leftPad formats a positive integer in decimal and left-pads with zeros to the requested minimum
// width; zero/negative inputs retain the existing all-padding behavior and widths do not truncate
// larger values.
// leftPad форматирует положительное целое десятично и дополняет слева нулями до минимальной
// ширины; ноль/отрицательные сохраняют прежнее поведение только заполнения, ширина не обрезает
// большие значения.
func leftPad(value, width int) string {
	result := ""
	for value > 0 {
		result = string(rune('0'+value%10)) + result
		value /= 10
	}
	for len(result) < width {
		result = "0" + result
	}
	return result
}
