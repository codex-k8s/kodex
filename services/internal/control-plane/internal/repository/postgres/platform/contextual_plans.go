package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantPlanSnapshotConflictFormat defines the indexed snapshot-conflict problem format shared
// by all seventeen specialized plan validation branches. Reusing this exact wire text does not
// merge their distinct owner reads, authorization, snapshot checks or failure order.
// assistantPlanSnapshotConflictFormat задаёт формат indexed snapshot-conflict problem общих
// семнадцати специализированных веток validation плана. Повторное использование точного wire text
// не объединяет разные owner reads, authorization, snapshot checks и порядок отказов.
const assistantPlanSnapshotConflictFormat = "operation-%d-snapshot-conflict"

// updateAssistantConversationTitle validates a bounded nonblank title and expected conversation
// version, locks the organization conversation and requires ACTIVE before storing USER_EDITED
// title metadata. Read failures are historically hidden as NotFound; it returns conversation/event
// metadata without committing the caller transaction.
// updateAssistantConversationTitle проверяет ограниченный непустой title и expected conversation
// version, блокирует беседу организации, требует ACTIVE до записи USER_EDITED title metadata.
// Ошибки чтения исторически скрывает как NotFound; возвращает conversation/event metadata без
// commit транзакции вызывающего.
func (repository *Repository) updateAssistantConversationTitle(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantConversationTitleInput)
	title := strings.TrimSpace(payload.Title)
	if !ok || payload.ConversationRef == "" || title == "" || len([]rune(title)) > 160 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var conversationID, projectID, state string
	var version, titleRevision int64
	if err := tx.QueryRow(ctx, queryConfigurationUpdateassistantconversationtitleSelectConversation,
		scope.organizationID, payload.ConversationRef,
	).Scan(&conversationID, &projectID, &version, &titleRevision, &state); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state != "ACTIVE" {
		return commandOutcome{}, errs.ErrConflict
	}
	item := entity.AssistantConversation{ProjectRef: projectRefByID(ctx, tx, projectID)}
	if err := tx.QueryRow(ctx, queryConfigurationUpdateassistantconversationtitleUpdateConversation,
		conversationID, title, "USER_EDITED",
	).Scan(&item.Ref, &item.Title, &item.TitleSource, &item.TitleRevision, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{Conversation: &item}, projectID: projectID,
		projectRef: item.ProjectRef, resourceKind: "ASSISTANT_CONVERSATION", resourceRef: item.Ref,
		summary: "i18n:ASSISTANT_CONVERSATION_TITLE_UPDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// projectRefByID returns empty for an absent project ID and otherwise performs an internal ID
// lookup, historically ignoring scan errors and returning the accumulated reference. The caller
// must already own the project identity boundary.
// projectRefByID возвращает пустое для отсутствующего project ID, иначе выполняет внутреннее
// чтение по ID, исторически игнорируя scan errors и возвращая накопленную ссылку. Границей project
// identity уже должен владеть вызывающий.
func projectRefByID(ctx context.Context, tx pgx.Tx, projectID string) string {
	if projectID == "" {
		return ""
	}
	var ref string
	_ = tx.QueryRow(ctx, queryCommandsEmitruneventSelectProjectsId, projectID).Scan(&ref)
	return ref
}

// assistantPlanDraftPrepared carries the locked plan/conversation/project identity,
// version/revision, constrained actor scope, normalized operations and original-by-key snapshot
// for a draft update. It is transaction-local preparation, not a persisted revision or execution
// receipt.
// assistantPlanDraftPrepared несёт заблокированные plan/conversation/project identity,
// version/revision, ограниченную actor scope, нормализованные операции и original-by-key snapshot
// обновления draft. Это подготовка внутри транзакции, не сохранённая revision и не execution
// receipt.
type assistantPlanDraftPrepared struct {
	payload                                    command.AssistantPlanDraftInput
	planID, conversationRef, projectRef, state string
	version, revision                          int64
	scope                                      scope
	operations                                 []entity.AssistantPlanOperation
	originals                                  map[string]entity.AssistantPlanOperation
}

// prepareAssistantPlanDraftTx validates draft bounds/OCC and unresolved state, preserves operation
// count/key/type and constrains scope from the conversation. Specialized editors restore immutable
// owner envelopes; STALE plans refresh eligible server snapshots while ordinary DRAFT retains
// them. It normalizes/binds and authorizes every operation, verifies file-revision pins and
// returns preparation without persistence or commit. Existing read errors remain NotFound and
// stale-file editing remains Conflict.
// prepareAssistantPlanDraftTx проверяет draft bounds/OCC и неразрешённое состояние, сохраняет
// operation count/key/type, ограничивает scope по беседе. Специализированные editors
// восстанавливают immutable owner envelopes; STALE обновляет допустимые серверные снимки, обычный
// DRAFT сохраняет прежние. Нормализует, связывает и авторизует каждую операцию, сверяет
// file-revision pins, возвращает подготовку без сохранения и commit. Прежние ошибки чтения дают
// NotFound, редактирование stale file — Conflict.
func (repository *Repository) prepareAssistantPlanDraftTx(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (assistantPlanDraftPrepared, error) {
	payload, ok := input.Payload.(command.AssistantPlanDraftInput)
	if !ok || payload.PlanRef == "" || strings.TrimSpace(payload.Summary) == "" || utf8.RuneCountInString(payload.Summary) > 2000 ||
		input.Mutation.ExpectedVersion == nil {
		return assistantPlanDraftPrepared{}, errs.ErrInvalid
	}
	var planID, conversationRef, state, projectRef string
	var version, revision int64
	var rawCurrent []byte
	if err := tx.QueryRow(ctx, queryConfigurationUpdateassistantplandraftSelectPlan, scope.organizationID, payload.PlanRef).Scan(
		&planID, &conversationRef, &state, &version, &revision, &projectRef, &rawCurrent,
	); err != nil {
		return assistantPlanDraftPrepared{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return assistantPlanDraftPrepared{}, errs.ErrVersionMismatch
	}
	if state == "APPLIED" || state == "REJECTED" {
		return assistantPlanDraftPrepared{}, errs.ErrAlreadyResolved
	}
	var current []entity.AssistantPlanOperation
	if json.Unmarshal(rawCurrent, &current) != nil {
		return assistantPlanDraftPrepared{}, errs.ErrConflict
	}
	if len(payload.Operations) != len(current) {
		return assistantPlanDraftPrepared{}, errs.ErrForbidden
	}
	if err := repository.constrainAssistantPlanScope(ctx, tx, &scope, conversationRef, payload.Operations); err != nil {
		return assistantPlanDraftPrepared{}, err
	}
	currentByKey := make(map[string]entity.AssistantPlanOperation, len(current))
	for _, operation := range current {
		currentByKey[operation.Key] = operation
	}
	for index, operation := range payload.Operations {
		original, exists := currentByKey[operation.Key]
		if !exists || original.Type != operation.Type {
			return assistantPlanDraftPrepared{}, errs.ErrForbidden
		}
		if assistantFileOperation(operation.Type) {
			if state == "STALE" || assistantString(original.Parameters, "contentRef") == "" {
				return assistantPlanDraftPrepared{}, errs.ErrConflict
			}
			updated, err := rehydrateEditedAssistantFile(original, operation, payload.PreparedContent[operation.Key] != "")
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
			continue
		}
		if assistantProjectConfigurationOperation(original) {
			updated, err := repository.refreshProjectAssistantConfiguration(ctx, tx, scope, original, operation, state == "STALE")
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
			continue
		}
		switch operation.Type {
		case "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE", "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION", "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT", changeProjectAssistantIntegrationGrant, prepareProjectAssistantConnection:
			updated, err := repository.rehydrateEditedAssistantConfiguration(ctx, tx, scope, original, operation, state == "STALE")
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "BIND_AGENT_RUNTIME_ENVIRONMENT":
			updated, err := repository.rehydrateEditedAssistantBinding(ctx, tx, scope, projectRef, original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_AGENT":
			updated, err := rehydrateEditedAssistantAgent(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "CREATE_INSTRUCTION_DRAFT":
			updated, err := rehydrateEditedAssistantInstructionDraft(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "CHANGE_CAPABILITY":
			updated, err := rehydrateEditedAssistantAgentCapability(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "CHANGE_INTEGRATION_GRANT":
			updated, err := repository.rehydrateEditedAssistantIntegrationGrant(ctx, tx, scope, projectRef, original, operation, state == "STALE")
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_WORKFLOW":
			updated, err := rehydrateEditedAssistantWorkflow(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
			updated, err := rehydrateEditedAssistantEnvironment(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS":
			updated, err := rehydrateEditedAssistantSystemInstructions(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_INTEGRATION_CONNECTION":
			updated, err := rehydrateEditedAssistantConnection(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_SCHEDULE":
			updated, err := rehydrateEditedAssistantSchedule(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "CREATE_ROLE_IMAGE_RECIPE":
			updated, err := rehydrateEditedAssistantRoleImage(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "UPDATE_ROLE_IMAGE_RECIPE":
			updated, err := repository.refreshEditedAssistantRoleImageUpdate(ctx, tx, scope, projectRef, original, operation, state == "STALE")
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		case "PUBLISH_INTEGRATION_DEFINITION":
			updated, err := rehydrateEditedAssistantIntegrationDefinitionPublication(original, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			payload.Operations[index] = updated
		}
		// STALE означает, что владелец уже увидел конфликт с авторитетным
		// состоянием. Новая immutable revision должна заново снять server-owned
		// snapshot, сохранив только разрешённые пользовательские поля формы.
		// Для обычного DRAFT прежний snapshot остаётся неизменным: скрытый rebase
		// без явного конфликта владельцу не допускается.
		if state == "STALE" && payload.Operations[index].ExpectedVersion != nil && !assistantConfigurationOperationType(operation.Type) && operation.Type != "UPDATE_ROLE_IMAGE_RECIPE" {
			selected := payload.Operations[index].Selected
			refreshed, refreshErr := repository.hydrateAssistantOperation(ctx, tx, scope, projectRef, payload.Operations[index])
			if refreshErr != nil {
				return assistantPlanDraftPrepared{}, refreshErr
			}
			refreshed.Selected = selected
			payload.Operations[index] = refreshed
		}
	}
	operations, err := normalizeAssistantOperations(payload.Operations, projectRef)
	if err != nil {
		return assistantPlanDraftPrepared{}, err
	}
	for _, operation := range operations {
		planned, err := assistantOperationCommand(operation)
		if err != nil {
			return assistantPlanDraftPrepared{}, err
		}
		if err := repository.authorizeAssistantPreparedOperation(ctx, tx, scope, operation, planned); err != nil {
			return assistantPlanDraftPrepared{}, err
		}
		if operation.Type == createProjectFileRevision {
			matches, err := repository.assistantFileRevisionSnapshotMatches(ctx, tx, scope, operation)
			if err != nil {
				return assistantPlanDraftPrepared{}, err
			}
			if !matches {
				return assistantPlanDraftPrepared{}, errs.ErrVersionMismatch
			}
		}
	}
	return assistantPlanDraftPrepared{payload: payload, planID: planID, conversationRef: conversationRef, projectRef: projectRef, state: state, version: version, revision: revision, scope: scope, operations: operations, originals: currentByKey}, nil
}

// updateAssistantPlanDraft persists the prepared next immutable USER revision and DRAFT plan with
// digest, then links each prepared file ledger to that exact revision/operation. It uses the
// constrained scope and returns versioned plan/event metadata; outer execution owns
// audit/idempotency/commit and object retention.
// updateAssistantPlanDraft сохраняет подготовленную следующую immutable USER revision и DRAFT план
// с digest, связывает каждый prepared file ledger с точной revision/operation. Использует
// ограниченную scope, возвращает версионные plan/event metadata; внешнее execution владеет
// audit/idempotency/commit и object retention.
func (repository *Repository) updateAssistantPlanDraft(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	prepared, err := repository.prepareAssistantPlanDraftTx(ctx, tx, scope, input)
	if err != nil {
		return commandOutcome{}, err
	}
	payload, planID, conversationRef, projectRef := prepared.payload, prepared.planID, prepared.conversationRef, prepared.projectRef
	version, revision, operations := prepared.version, prepared.revision, prepared.operations
	scope = prepared.scope
	if err := assistantFilePersistenceReady(operations, payload.PreparedContent); err != nil {
		return commandOutcome{}, err
	}
	raw := asJSON(operations)
	digest := assistantPlanDigest(payload.Summary, raw)
	nextRevision := revision + 1
	revisionRef, err := newRef("prv")
	if err != nil {
		return commandOutcome{}, err
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryConfigurationInsertAssistantPlanRevision, revisionRef, scope.organizationID,
		planID, nextRevision, strings.TrimSpace(payload.Summary), raw, digest, "USER", scope.actorRef,
	).Scan(&createdAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryConfigurationUpdateassistantplandraftUpdatePlan, planID,
		strings.TrimSpace(payload.Summary), raw, nextRevision, digest); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	for _, operation := range operations {
		if !assistantFileOperation(operation.Type) {
			continue
		}
		ledgerID := payload.PreparedContent[operation.Key]
		if ledgerID == "" {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.linkPreparedContentTx(ctx, tx, scope, ledgerID, planID, nextRevision, operation.Key); err != nil {
			return commandOutcome{}, err
		}
	}
	plan := entity.AssistantPlan{Ref: payload.PlanRef, ConversationRef: conversationRef, ProjectRef: projectRef,
		Summary: strings.TrimSpace(payload.Summary), State: "DRAFT", Version: version + 1, Revision: nextRevision,
		ContentDigest: digest, Operations: operations, CreatedAt: createdAt}
	return commandOutcome{result: command.Result{Plan: &plan}, projectID: mustProjectID(ctx, tx, scope.organizationID, projectRef),
		projectRef: projectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef,
		summary: "i18n:ASSISTANT_PLAN_DRAFT_UPDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// assistantPlanAgentVersionKey returns the affected assistant/agent reference for the explicit
// self-image/project-configuration/runtime-configuration and ordinary agent mutation kinds.
// Unsupported or incompletely bound kinds return empty, allowing later plan execution to avoid
// rebasing unrelated target versions.
// assistantPlanAgentVersionKey возвращает ссылку затронутого assistant/agent для явных
// self-image/project-configuration/runtime-configuration и обычных agent mutations.
// Неподдерживаемые либо неполно привязанные виды дают пустое, чтобы позднее исполнение плана не
// меняло версии несвязанных targets.
func assistantPlanAgentVersionKey(operation entity.AssistantPlanOperation) string {
	if operation.Type == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
		return assistantString(operation.Parameters, "systemAssistantRef")
	}
	if assistantProjectConfigurationOperation(operation) {
		return assistantString(operation.Parameters, "projectAssistantRef")
	}
	if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
		return assistantString(operation.Parameters, "agentRef")
	}
	switch operation.Type {
	case "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT", "CHANGE_CAPABILITY", "ARCHIVE_AGENT":
		if operation.Target.Kind == "AGENT" && operation.Target.Ref != "" {
			return operation.Target.Ref
		}
	}
	return ""
}

// rebaseAssistantPlanAgentVersion copies operation maps for a valid affected-agent key and updates
// only existing agentVersion pins. Self-image/environment kinds preserve their distinct target
// OCC; other recognized kinds also get independent expected/target version pointers and Input
// expectedVersion. It uses a caller-supplied version without querying or authorizing state.
// rebaseAssistantPlanAgentVersion копирует карты операции при допустимом affected-agent key,
// обновляет лишь существующие agentVersion pins. Self-image/environment виды сохраняют отдельный
// target OCC; остальные распознанные виды получают независимые expected/target version pointers и
// Input expectedVersion. Использует версию вызывающего без чтения и авторизации состояния.
func rebaseAssistantPlanAgentVersion(operation entity.AssistantPlanOperation, version int64) entity.AssistantPlanOperation {
	if version < 1 || assistantPlanAgentVersionKey(operation) == "" {
		return operation
	}
	operation.Input = cloneAssistantFields(operation.Input)
	operation.Parameters = cloneAssistantFields(operation.Parameters)
	operation.Before = cloneAssistantFields(operation.Before)
	operation.After = cloneAssistantFields(operation.After)
	for _, fields := range []map[string]any{operation.Input, operation.Parameters, operation.Before, operation.After} {
		if _, exists := fields["agentVersion"]; exists {
			fields["agentVersion"] = version
		}
	}
	if operation.Type == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
		return operation
	}
	expectedVersion, targetVersion := version, version
	operation.ExpectedVersion = &expectedVersion
	operation.Target.Version = &targetVersion
	operation.Input = cloneAssistantFields(operation.Input)
	operation.Input["expectedVersion"] = version
	return operation
}

// normalizeAssistantOperations requires one through32 operations with unique keys, normalizes each
// and binds project inputs, and requires at least one selected operation. It returns no partial
// slice on failure; normalization does not replace per-operation authorization or snapshot
// validation.
// normalizeAssistantOperations требует от одной до32 операций с уникальными keys, нормализует
// каждую, связывает project inputs, требует хотя бы одну selected. При отказе не возвращает
// частичный срез; нормализация не заменяет пооперационные authorization и snapshot validation.
func normalizeAssistantOperations(input []entity.AssistantPlanOperation, projectRef string) ([]entity.AssistantPlanOperation, error) {
	if len(input) == 0 || len(input) > maximumAssistantPlanOperations {
		return nil, errs.ErrInvalid
	}
	seen := make(map[string]struct{}, len(input))
	selected := 0
	result := make([]entity.AssistantPlanOperation, 0, len(input))
	for _, operation := range input {
		if _, duplicate := seen[operation.Key]; duplicate {
			return nil, errs.ErrInvalid
		}
		seen[operation.Key] = struct{}{}
		var err error
		operation, err = normalizeAssistantOperation(operation)
		if err != nil {
			return nil, err
		}
		operation, err = bindAssistantOperationProject(operation, projectRef)
		if err != nil {
			return nil, err
		}
		if operation.Selected {
			selected++
		}
		result = append(result, operation)
	}
	if selected == 0 {
		return nil, errs.ErrInvalid
	}
	return result, nil
}

// validateAssistantPlan locks a plan, checks version/revision and constrained conversation scope,
// then checks only selected operations for duplicate grant targets, prepared file content, typed
// command authorization, kind-specific snapshots, launch readiness and current target versions.
// Failures are accumulated as indexed safe problems and stored as INVALID or VALID for that
// revision, not executed effects. Read/snapshot failures can become validation problems under
// existing mappings; caller owns commit.
// validateAssistantPlan блокирует план, проверяет version/revision и ограниченную conversation
// scope, затем лишь selected operations: duplicate grant targets, prepared file content, typed
// command authorization, отдельные snapshots, launch readiness и текущие target versions. Отказы
// собирает в indexed safe problems, сохраняет INVALID либо VALID для revision без исполнения
// effects. Ошибки read/snapshot могут стать validation problems по прежним mappings; commit
// принадлежит вызывающему.
func (repository *Repository) validateAssistantPlan(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantPlanInput)
	if !ok || payload.PlanRef == "" || payload.Revision < 1 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var planID, conversationRef, summary, rawState, digest, projectRef string
	var raw []byte
	var version, revision int64
	if err := tx.QueryRow(ctx, queryConfigurationValidateassistantplanSelectPlan, scope.organizationID, payload.PlanRef).Scan(
		&planID, &conversationRef, &summary, &raw, &rawState, &version, &revision, &digest, &projectRef,
	); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if version != *input.Mutation.ExpectedVersion || revision != payload.Revision {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var stored []entity.AssistantPlanOperation
	if json.Unmarshal(raw, &stored) != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	operations, err := normalizeAssistantOperations(stored, projectRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.constrainAssistantPlanScope(ctx, tx, &scope, conversationRef, operations); err != nil {
		return commandOutcome{}, err
	}
	problems := make([]string, 0)
	grantTargets := make(map[string]struct{})
	for index, operation := range operations {
		if !operation.Selected {
			continue
		}
		if connectionRef := assistantGrantConnection(operation); connectionRef != "" {
			ownerRef := assistantString(operation.Parameters, "agentRef")
			if operation.Type == changeProjectAssistantIntegrationGrant {
				ownerRef = assistantString(operation.Parameters, "projectAssistantRef")
			}
			if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
				ownerRef = assistantString(operation.Parameters, "systemAssistantRef")
			}
			grantTarget := connectionRef + "\x00" + ownerRef + "\x00" + assistantString(operation.Parameters, "capabilityKey")
			if _, duplicate := grantTargets[grantTarget]; duplicate {
				problems = append(problems, fmt.Sprintf("operation-%d-duplicate-integration-grant", index+1))
				continue
			}
			grantTargets[grantTarget] = struct{}{}
		}
		if !assistantProjectFileContentReady(operation) {
			problems = append(problems, fmt.Sprintf("operation-%d-content-required", index+1))
			continue
		}
		planned, commandErr := assistantOperationCommand(operation)
		if commandErr != nil {
			problems = append(problems, fmt.Sprintf("operation-%d-invalid", index+1))
			continue
		}
		if commandErr = repository.authorizeAssistantPreparedOperation(ctx, tx, scope, operation, planned); commandErr != nil {
			problems = append(problems, assistantPlanAuthorizationProblem(index, commandErr))
			continue
		}
		if assistantFileOperation(operation.Type) {
			if _, preparedErr := repository.resolveAssistantFileOperationTx(ctx, tx, scope, planID, revision, operation, planned); preparedErr != nil {
				problems = append(problems, fmt.Sprintf("operation-%d-content-unavailable", index+1))
				continue
			}
		}
		if operation.Type == createProjectFileRevision {
			matching, snapshotErr := repository.assistantFileRevisionSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == prepareProjectAssistantConnection {
			matching, snapshotErr := repository.projectAssistantConnectionSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "LAUNCH_RUN" {
			if readinessErr := repository.validateAssistantLaunchReadiness(ctx, tx, scope, operation); readinessErr != nil {
				problems = append(problems, fmt.Sprintf("operation-%d-runtime-unavailable", index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_AGENT" {
			matching, snapshotErr := repository.assistantAgentUpdateSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "CREATE_INSTRUCTION_DRAFT" {
			matching, snapshotErr := repository.assistantInstructionDraftSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_WORKFLOW" {
			matching, snapshotErr := repository.assistantWorkflowUpdateSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "PREPARE_RUNTIME_ENVIRONMENT_REVISION" {
			matching, snapshotErr := repository.assistantEnvironmentSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS" {
			matching, snapshotErr := repository.assistantSystemInstructionsSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "BIND_AGENT_RUNTIME_ENVIRONMENT" {
			matching, snapshotErr := repository.assistantAgentBindingSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_INTEGRATION_CONNECTION" {
			matching, snapshotErr := repository.assistantConnectionUpdateSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "CHANGE_INTEGRATION_GRANT" {
			matching, snapshotErr := repository.assistantIntegrationGrantSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_SCHEDULE" {
			matching, snapshotErr := repository.assistantScheduleUpdateSnapshotMatches(ctx, tx, scope, projectRef, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "UPDATE_ROLE_IMAGE_RECIPE" {
			matching, snapshotErr := repository.assistantRoleImageUpdateSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" || operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
			matching, snapshotErr := repository.systemAssistantImageSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == changeProjectAssistantIntegrationGrant {
			matching, snapshotErr := repository.projectAssistantIntegrationGrantSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "CHANGE_SYSTEM_ASSISTANT_INTEGRATION_GRANT" {
			matching, snapshotErr := repository.systemAssistantIntegrationGrantSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
			matching, snapshotErr := repository.assistantRuntimeConfigurationSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		if operation.Type == "PUBLISH_INTEGRATION_DEFINITION" {
			matching, snapshotErr := repository.assistantIntegrationDefinitionPublicationSnapshotMatches(ctx, tx, scope, operation)
			if snapshotErr != nil || !matching {
				problems = append(problems, fmt.Sprintf(assistantPlanSnapshotConflictFormat, index+1))
				continue
			}
		}
		current, checked, versionErr := repository.assistantTargetVersion(ctx, tx, scope, operation)
		if versionErr != nil {
			problems = append(problems, fmt.Sprintf("operation-%d-target-unavailable", index+1))
		} else if checked && current != *operation.ExpectedVersion {
			problems = append(problems, fmt.Sprintf("operation-%d-version-conflict", index+1))
		}
	}
	state := "VALID"
	if len(problems) > 0 {
		state = "INVALID"
	}
	if _, err := tx.Exec(ctx, queryConfigurationValidateassistantplanUpdatePlan, planID, state, revision, problems); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	now := time.Now().UTC()
	plan := entity.AssistantPlan{Ref: payload.PlanRef, ConversationRef: conversationRef, ProjectRef: projectRef,
		Summary: summary, State: state, Version: version + 1, Revision: revision, ValidatedRevision: &revision,
		ContentDigest: digest, ValidationProblems: problems, Operations: operations, ValidatedAt: &now}
	return commandOutcome{result: command.Result{Plan: &plan}, projectID: mustProjectID(ctx, tx, scope.organizationID, projectRef),
		projectRef: projectRef, resourceKind: "ASSISTANT_PLAN", resourceRef: payload.PlanRef,
		summary: "i18n:ASSISTANT_PLAN_VALIDATED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// assistantPlanAuthorizationProblem formats a one-based operation problem, distinguishing
// VersionMismatch from the generic not-permitted classification using errors.Is without exposing
// underlying diagnostics.
// assistantPlanAuthorizationProblem форматирует problem операции с индексом от единицы, отличает
// VersionMismatch от общего not-permitted через errors.Is без раскрытия исходной диагностики.
func assistantPlanAuthorizationProblem(index int, err error) string {
	problem := "not-permitted"
	if errors.Is(err, errs.ErrVersionMismatch) {
		problem = "version-conflict"
	}
	return fmt.Sprintf("operation-%d-%s", index+1, problem)
}

// validateAssistantLaunchReadiness decodes the planned launch, resolves its project and active
// agent or published workflow snapshot, and checks role-runtime contract pins for all target
// agents. Provider account/catalog selection is performed for the first agent or workflow
// coordinator; this is submission validation rather than live execution or a full per-step
// provider probe.
// validateAssistantLaunchReadiness декодирует planned launch, разрешает проект и active agent либо
// published workflow snapshot, проверяет role-runtime contract pins всех target agents. Provider
// account/catalog selection выполняет для первого агента либо workflow coordinator; это submission
// validation, не реальное исполнение и не полная per-step проверка провайдера.
func (repository *Repository) validateAssistantLaunchReadiness(
	ctx context.Context,
	tx pgx.Tx,
	scope scope,
	operation entity.AssistantPlanOperation,
) error {
	payload, err := assistantRun(operation.Input)
	if err != nil {
		return err
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
	if projectID == "" {
		return errs.ErrNotFound
	}
	targetAgentRefs := []string{}
	switch payload.Target.Type {
	case "AGENT":
		var targetName string
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef,
			scope.organizationID, projectID, payload.Target.Ref).Scan(&targetName); err != nil {
			return errs.ErrConflict
		}
		targetAgentRefs = append(targetAgentRefs, payload.Target.Ref)
	case "WORKFLOW":
		var targetName, workflowVersionID, workflowVersionRef, workflowVersionDigest string
		var coordinatorRef, coordinatorName string
		var workflowSpec []byte
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"project_id":      projectID,
			"workflow_ref":    payload.Target.Ref,
		}).Scan(&targetName, &workflowVersionID, &workflowVersionRef, &workflowSpec,
			&workflowVersionDigest, &coordinatorRef, &coordinatorName); err != nil {
			return errs.ErrConflict
		}
		var workflowVersion entity.WorkflowVersion
		if json.Unmarshal(workflowSpec, &workflowVersion) != nil || !validWorkflowVersion(workflowVersion) ||
			workflowVersion.CoordinatorAgentRef != coordinatorRef {
			return errs.ErrConflict
		}
		targetAgentRefs = append(targetAgentRefs, coordinatorRef)
		for _, step := range workflowVersion.Steps {
			targetAgentRefs = append(targetAgentRefs, step.AgentRef)
		}
	default:
		return errs.ErrInvalid
	}
	var runtimeContractReady bool
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunValidateAgentRuntimeContract, pgx.StrictNamedArgs{
		"organization_id":                scope.organizationID,
		"project_id":                     projectID,
		"agent_refs":                     targetAgentRefs,
		"role_runtime_contract_revision": repository.roleImages.RoleRuntimeContractRevision,
		"role_runtime_contract_sha256":   repository.roleImages.RoleRuntimeContractSHA256,
		"default_role_image_digest":      repository.roleImages.DefaultImageDigest,
	}).Scan(&runtimeContractReady); err != nil {
		return errs.ErrUnavailable
	}
	if !runtimeContractReady {
		return errs.ErrConflict
	}
	if _, err = repository.selectProviderAccountForAgent(ctx, tx, scope.organizationID, targetAgentRefs[0]); err != nil {
		return err
	}
	configuration, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, scope.organizationID, targetAgentRefs[0], "")
	if err != nil {
		return err
	}
	_, _, err = validateRuntimeCatalogCandidates(ctx, tx, scope, configuration.Provider, configuration.Model,
		overlay, configuration.ProviderPolicy.AccountCandidates, false)
	return err
}

// assistantTargetVersion reads current OCC for the explicit supported target kinds, with dedicated
// runtime-config/system-image/system-assistant readers and organization-scoped ordinary target
// lookup. Ordinary operations without ExpectedVersion or unsupported kinds return unchecked;
// missing/failed checked lookups preserve NotFound/Unavailable. The caller compares the version
// after separate authorization/snapshot checks.
// assistantTargetVersion читает текущий OCC явных поддерживаемых target kinds, использует
// отдельные runtime-config/system-image/system-assistant readers и organization-scoped обычный
// lookup. Обычные операции без ExpectedVersion либо неподдерживаемые виды дают unchecked;
// отсутствующие/неуспешные checked lookups сохраняют NotFound/Unavailable. Вызывающий сравнивает
// версию после отдельных authorization/snapshot проверок.
func (repository *Repository) assistantTargetVersion(ctx context.Context, tx pgx.Tx, scope scope, operation entity.AssistantPlanOperation) (int64, bool, error) {
	if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
		target, err := repository.assistantConfigurationTarget(ctx, tx, scope, operation.Target.Ref)
		return target.version, true, err
	}
	if operation.Type == "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" {
		_, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, scope, operation.Target.Ref, "", "ORGANIZATION")
		if err != nil {
			return 0, true, err
		}
		detail, err := repository.getRoleImageRecipe(ctx, tx, scope, operation.Target.Ref, true, false)
		return int64(detail.Recipe.Version), true, err
	}
	if operation.ExpectedVersion == nil {
		return 0, false, nil
	}
	var kind, ref string
	switch operation.Type {
	case "UPDATE_PROJECT":
		kind, ref = "PROJECT", assistantString(operation.Input, "projectRef")
	case "UPDATE_AGENT", "CREATE_INSTRUCTION_DRAFT", "BIND_AGENT_RUNTIME_ENVIRONMENT":
		kind, ref = "AGENT", assistantString(operation.Input, "agentRef")
	case "CHANGE_CAPABILITY", "ARCHIVE_AGENT":
		kind, ref = "AGENT", assistantString(operation.Input, "agentRef")
		if operation.Type == "ARCHIVE_AGENT" {
			ref = operation.Target.Ref
		}
	case "UPDATE_WORKFLOW", "ARCHIVE_WORKFLOW":
		kind, ref = "WORKFLOW", operation.Target.Ref
	case "PREPARE_RUNTIME_ENVIRONMENT_REVISION":
		kind, ref = "ENVIRONMENT", operation.Target.Ref
	case "UPDATE_SYSTEM_ASSISTANT_INSTRUCTIONS":
		assistant, err := repository.getAssistantTx(ctx, tx, scope)
		if err != nil {
			return 0, true, err
		}
		if assistant.Ref != operation.Target.Ref {
			return 0, true, errs.ErrNotFound
		}
		return assistant.Version, true, nil
	case "CHANGE_INTEGRATION_GRANT", "UPDATE_INTEGRATION_CONNECTION", "TEST_INTEGRATION_CONNECTION":
		kind, ref = "INTEGRATION_CONNECTION", assistantString(operation.Input, "connectionRef")
	case "UPDATE_SCHEDULE":
		kind, ref = "SCHEDULE", assistantString(operation.Input, "scheduleRef")
	case "UPDATE_ROLE_IMAGE_RECIPE":
		kind, ref = "ROLE_IMAGE_RECIPE", assistantString(operation.Input, "recipeRef")
	default:
		return 0, false, nil
	}
	var current int64
	if err := tx.QueryRow(ctx, queryConfigurationValidateassistantplanSelectTargetVersion,
		scope.organizationID, kind, ref,
	).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, true, errs.ErrNotFound
		}
		return 0, true, errs.ErrUnavailable
	}
	return current, true, nil
}

// rehydrateEditedAssistantAgent validates the original update identity/version, exact edited agent
// ref and immutable avatar, then reapplies only allowed nonblank string edits against original
// owner fields. It requires the rebuilt Before to equal the original and restores the user
// selection; forged preview target/version/before cannot replace the owner envelope.
// rehydrateEditedAssistantAgent проверяет исходные update identity/version, точный edited agent
// ref и immutable avatar, повторно применяет лишь допустимые непустые строки к original owner
// fields. Требует равенства восстановленного Before исходному, возвращает user selection;
// подставленные preview target/version/before не заменяют owner envelope.
func rehydrateEditedAssistantAgent(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_AGENT" || original.Key != edited.Key || original.Target.Kind != "AGENT" ||
		original.Target.Ref == "" || original.ExpectedVersion == nil || *original.ExpectedVersion < 1 ||
		edited.Parameters == nil || !onlyAssistantFields(edited.Parameters, "agentRef", "name", "purpose", "roleDescription", "avatarUrl") ||
		assistantString(edited.Parameters, "agentRef") != original.Target.Ref {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	if value, supplied := edited.Parameters["avatarUrl"]; supplied && value != original.Before["avatarUrl"] {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	parameters := map[string]any{"agentRef": original.Target.Ref}
	for _, field := range []string{"name", "purpose", "roleDescription"} {
		value, supplied := edited.Parameters[field]
		if !supplied {
			continue
		}
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		parameters[field] = text
	}
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrateAssistantAgentFields(original.Target.Ref,
		assistantString(original.Before, "name"), assistantString(original.Before, "purpose"),
		assistantString(original.Before, "roleDescription"), assistantString(original.Before, "avatarUrl"),
		*original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

// rehydrateEditedAssistantAgentCapability requires exact original agent/key/version/capability and
// typed original/requested enabled flags, then rebuilds the toggle from owner metadata. Before
// mismatch is Conflict and Selected is restored; it does not grant the capability or read current
// database authority.
// rehydrateEditedAssistantAgentCapability требует точные original agent/key/version/capability и
// типизированные original/requested enabled, затем перестраивает переключение из owner metadata.
// Before mismatch даёт Conflict, Selected восстанавливается; capability не предоставляет, текущую
// database authority не читает.
func rehydrateEditedAssistantAgentCapability(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "CHANGE_CAPABILITY" || original.Key != edited.Key || original.Target.Kind != "AGENT" ||
		original.Target.Ref == "" || original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "agentRef", "capabilityKey", "enabled") ||
		assistantString(edited.Parameters, "agentRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "capabilityKey") != assistantString(original.Before, "capabilityKey") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	enabled, ok := assistantBoolValue(edited.Parameters, "enabled")
	currentlyEnabled, currentOK := assistantBoolValue(original.Before, "enabled")
	if !ok || !currentOK {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	selected := edited.Selected
	hydrated, err := hydrateAssistantAgentCapabilityFields(original.Target.Ref, original.Target.Name,
		assistantString(original.Before, "capabilityKey"), currentlyEnabled, enabled, *original.ExpectedVersion, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

// rehydrateEditedAssistantIntegrationGrant preserves original connection/recipient identity and
// supported fields, requiring the original current snapshot unless an explicit stale refresh was
// requested. It hydrates fresh grant/approval metadata and restores selection; an ordinary
// snapshot read/mismatch is returned as the edited operation plus Conflict under the existing
// contract.
// rehydrateEditedAssistantIntegrationGrant сохраняет исходные connection/recipient identity и
// поддерживаемые поля, требует текущий original snapshot без явного stale refresh. Загружает
// свежие grant/approval metadata, восстанавливает selection; обычный snapshot read/mismatch
// возвращает edited operation вместе с Conflict по прежнему контракту.
func (repository *Repository) rehydrateEditedAssistantIntegrationGrant(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	projectRef string,
	original, edited entity.AssistantPlanOperation,
	refreshStale bool,
) (entity.AssistantPlanOperation, error) {
	if original.Type != "CHANGE_INTEGRATION_GRANT" || original.Key != edited.Key ||
		original.Target.Kind != "INTEGRATION_CONNECTION" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "connectionRef", "capabilityKey", "agentRef", "workflowRef", "enabled", "approvalScopePaths", "approvalPolicy") ||
		assistantString(edited.Parameters, "connectionRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "agentRef") != assistantString(original.Before, "agentRef") ||
		assistantString(edited.Parameters, "workflowRef") != assistantString(original.Before, "workflowRef") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	if !refreshStale {
		matching, err := repository.assistantIntegrationGrantSnapshotMatches(ctx, tx, current, projectRef, original)
		if err != nil || !matching {
			return edited, errs.ErrConflict
		}
	}
	selected := edited.Selected
	hydrated, err := repository.hydrateAssistantIntegrationGrant(ctx, tx, current, projectRef, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	hydrated.Selected = selected
	return hydrated, nil
}

// rehydrateEditedAssistantConnection requires original connection/key/version and unchanged
// definition identity, rebuilds allowed name/public-configuration edits from original public
// fields and validates equality of rebuilt Before. It restores selection and original
// target/version, rejecting malformed/no-op changes through the shared field hydrator without
// modifying credentials.
// rehydrateEditedAssistantConnection требует исходные connection/key/version и неизменённый
// definition identity, перестраивает name/public-configuration edits из original public fields,
// проверяет равенство Before. Восстанавливает selection и original target/version, отклоняет
// malformed/no-op через общий field hydrator без изменения credentials.
func rehydrateEditedAssistantConnection(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "UPDATE_INTEGRATION_CONNECTION" || original.Key != edited.Key ||
		original.Target.Kind != "INTEGRATION_CONNECTION" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "connectionRef", "definitionKey", "name", "publicConfiguration") ||
		assistantString(edited.Parameters, "connectionRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "definitionKey") != assistantString(original.Before, "definitionKey") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	parameters := map[string]any{"connectionRef": original.Target.Ref}
	if name, supplied := edited.Parameters["name"]; supplied {
		parameters["name"] = name
	}
	if configuration, supplied := edited.Parameters["publicConfiguration"]; supplied {
		parameters["publicConfiguration"] = configuration
	}
	connection := entity.IntegrationConnection{Ref: original.Target.Ref,
		DefinitionKey: assistantString(original.Before, "definitionKey"),
		Name:          assistantString(original.Before, "name"), Version: *original.ExpectedVersion}
	configuration, ok := assistantObjectValue(original.Before, "publicConfiguration")
	if !ok {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	connection.PublicConfiguration = configuration
	edited.Parameters = parameters
	selected := edited.Selected
	hydrated, err := hydrateAssistantConnectionFields(connection, edited)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	if !reflect.DeepEqual(hydrated.Before, original.Before) {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	hydrated.Selected = selected
	return hydrated, nil
}

// assistantConnectionUpdateSnapshotMatches reads the current connection/package/grants, validates
// proposed After configuration against its definition and compares exact versions/name/Before,
// Parameters/After equality and definition key. It returns read/configuration failures unchanged;
// it does not update or authorize the connection independently.
// assistantConnectionUpdateSnapshotMatches читает текущие connection/package/grants, проверяет
// предлагаемую After configuration по definition, сравнивает точные versions/name/Before,
// равенство Parameters/After и definition key. Возвращает исходные read/configuration failures; не
// обновляет и самостоятельно не авторизует connection.
func (repository *Repository) assistantConnectionUpdateSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	connection, err := readConnection(ctx, tx, actorScope, operation.Target.Ref)
	if err != nil {
		return false, err
	}
	before := map[string]any{"connectionRef": connection.Ref, "definitionKey": connection.DefinitionKey,
		"name": connection.Name, "publicConfiguration": connection.PublicConfiguration}
	if err := repository.validateAssistantConnectionConfiguration(ctx, tx, actorScope, connection, operation.After); err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == connection.Version &&
		operation.Target.Version != nil && *operation.Target.Version == connection.Version &&
		operation.Target.Name == connection.Name && reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "definitionKey") == connection.DefinitionKey, nil
}

// assistantIntegrationGrantSnapshotMatches rehydrates the proposed grant from current owner state
// and compares expected version/target plus JSON-equivalent Parameters/Before/After. Hydration
// errors, including a current no-op conflict, remain errors; equality does not itself apply or
// authorize a grant change.
// assistantIntegrationGrantSnapshotMatches повторно загружает предлагаемый grant из текущего owner
// state, сравнивает expected version/target и JSON-equivalent Parameters/Before/After. Hydration
// errors, включая текущий no-op conflict, остаются ошибками; равенство само не применяет и не
// авторизует изменение grant.
func (repository *Repository) assistantIntegrationGrantSnapshotMatches(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	projectRef string,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	hydrated, err := repository.hydrateAssistantIntegrationGrant(ctx, tx, current, projectRef, operation)
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && hydrated.ExpectedVersion != nil &&
		*operation.ExpectedVersion == *hydrated.ExpectedVersion && reflect.DeepEqual(operation.Target, hydrated.Target) &&
		assistantJSONEqual(operation.Parameters, hydrated.Parameters) && assistantJSONEqual(operation.Before, hydrated.Before) &&
		assistantJSONEqual(operation.After, hydrated.After), nil
}

// rehydrateEditedAssistantRoleImage preserves project/agent identity and agent-version pin while
// allowing bounded nonblank image name/environment/Dockerfile edits. It rebuilds a CREATE preview
// with empty Before and no target OCC from original parameters; catalog recipe validation,
// build/admission and write authority remain later specialized checks.
// rehydrateEditedAssistantRoleImage сохраняет project/agent identity и agent-version pin,
// допускает ограниченные непустые image name/environment/Dockerfile edits. Перестраивает CREATE
// preview с пустым Before и без target OCC из original parameters; catalog recipe validation,
// build/admission и write authority остаются поздними специализированными проверками.
func rehydrateEditedAssistantRoleImage(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "CREATE_ROLE_IMAGE_RECIPE" || original.Key != edited.Key ||
		original.Target.Kind != "ROLE_IMAGE_RECIPE" || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, "projectRef", "agentRef", "agentVersion", "name", "environmentKey", "dockerfile") ||
		assistantString(edited.Parameters, "projectRef") != assistantString(original.Parameters, "projectRef") ||
		assistantString(edited.Parameters, "agentRef") != assistantString(original.Parameters, "agentRef") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	originalVersion, originalOK := assistantInt64(original.Parameters, "agentVersion")
	editedVersion, editedOK := assistantInt64(edited.Parameters, "agentVersion")
	if !originalOK || !editedOK || originalVersion != editedVersion {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	name := assistantString(edited.Parameters, "name")
	environmentKey := assistantString(edited.Parameters, "environmentKey")
	dockerfile, dockerfileOK := edited.Parameters["dockerfile"].(string)
	if name == "" || environmentKey == "" || !dockerfileOK || dockerfile == "" || len(dockerfile) > 64<<10 {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	parameters := cloneAssistantFields(original.Parameters)
	parameters["name"], parameters["environmentKey"], parameters["dockerfile"] = name, environmentKey, dockerfile
	edited.Parameters = parameters
	edited.Action = "CREATE"
	edited.Target = entity.AssistantPlanTarget{Kind: "ROLE_IMAGE_RECIPE", Name: name}
	edited.Before = map[string]any{}
	edited.After = cloneAssistantFields(parameters)
	edited.ExpectedVersion = nil
	return edited, nil
}

// assistantAgentUpdateSnapshotMatches reads the exact organization/project/agent fields and
// version, then compares expected/target versions/name, original Before, Parameters/After equality
// and unchanged avatar. Driver errors remain unchanged for the caller to classify; the comparison
// performs no mutation or access evaluation.
// assistantAgentUpdateSnapshotMatches читает точные поля/версию organization/project/agent,
// сравнивает expected/target versions/name, original Before, равенство Parameters/After и
// неизменённый avatar. Driver errors сохраняет для классификации вызывающим; сравнение ничего не
// меняет и не проверяет доступ.
func (repository *Repository) assistantAgentUpdateSnapshotMatches(ctx context.Context, tx pgx.Tx, scope scope, projectRef string,
	operation entity.AssistantPlanOperation,
) (bool, error) {
	var name, purpose, roleDescription, avatarURL string
	var version int64
	err := tx.QueryRow(ctx, queryConfigurationHydrateassistantoperationSelectAgent,
		scope.organizationID, projectRef, operation.Target.Ref,
	).Scan(&name, &purpose, &roleDescription, &avatarURL, &version)
	if err != nil {
		return false, err
	}
	before := map[string]any{"agentRef": operation.Target.Ref, "name": name, "purpose": purpose,
		"roleDescription": roleDescription, "avatarUrl": avatarURL}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == name && reflect.DeepEqual(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "avatarUrl") == avatarURL, nil
}

// rejectAssistantPlan validates typed plan/ref/revision/OCC, rejects already APPLIED/REJECTED
// plans and writes REJECTED plus a receipt for the exact revision in the caller transaction. It
// returns minimal plan/receipt/event metadata without applying operations; outer execution owns
// current authorization, audit and commit.
// rejectAssistantPlan проверяет typed plan/ref/revision/OCC, отклоняет уже APPLIED/REJECTED, пишет
// REJECTED и receipt точной revision в транзакции вызывающего. Возвращает минимальные
// plan/receipt/event metadata без применения операций; внешнее execution владеет текущими
// authorization, audit и commit.
func (repository *Repository) rejectAssistantPlan(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AssistantPlanInput)
	if !ok || payload.PlanRef == "" || payload.Revision < 1 || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var planID, state string
	var version, revision int64
	if err := tx.QueryRow(ctx, queryConfigurationRejectassistantplanSelectPlan, scope.organizationID, payload.PlanRef).Scan(
		&planID, &state, &version, &revision,
	); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion || revision != payload.Revision {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state == "APPLIED" || state == "REJECTED" {
		return commandOutcome{}, errs.ErrAlreadyResolved
	}
	if _, err := tx.Exec(ctx, queryConfigurationRejectassistantplanUpdatePlan, planID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	receipt, err := repository.insertAssistantPlanReceipt(ctx, tx, scope, planID, payload.PlanRef, revision, "REJECTED", nil, nil, nil)
	if err != nil {
		return commandOutcome{}, err
	}
	plan := entity.AssistantPlan{Ref: payload.PlanRef, State: "REJECTED", Version: version + 1, Revision: revision}
	return commandOutcome{result: command.Result{Plan: &plan, PlanReceipt: &receipt}, resourceKind: "ASSISTANT_PLAN",
		resourceRef: payload.PlanRef, summary: "i18n:ASSISTANT_PLAN_REJECTED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// insertAssistantPlanReceipt normalizes nil operation/conflict/created lists to empty, generates a
// receipt reference and collects nonempty operation audit refs in order. It inserts the
// actor-bound outcome for the exact plan/revision and returns recorded creation time without
// committing; the receipt records effects rather than granting future authority.
// insertAssistantPlanReceipt заменяет nil operation/conflict/created lists пустыми, создаёт
// receipt ref, собирает непустые audit refs операций по порядку. Вставляет actor-bound outcome
// точных plan/revision, возвращает сохранённое время без commit; receipt фиксирует effects, не
// даёт будущих полномочий.
func (repository *Repository) insertAssistantPlanReceipt(ctx context.Context, tx pgx.Tx, scope scope, planID, planRef string,
	revision int64, outcome string, operations []entity.AssistantPlanOperationReceipt, conflicts []entity.AssistantPlanConflict,
	created []string,
) (entity.AssistantPlanReceipt, error) {
	if operations == nil {
		operations = []entity.AssistantPlanOperationReceipt{}
	}
	if conflicts == nil {
		conflicts = []entity.AssistantPlanConflict{}
	}
	if created == nil {
		created = []string{}
	}
	ref, err := newRef("rct")
	if err != nil {
		return entity.AssistantPlanReceipt{}, err
	}
	auditRefs := make([]string, 0, len(operations))
	for _, operation := range operations {
		if operation.AuditRef != "" {
			auditRefs = append(auditRefs, operation.AuditRef)
		}
	}
	var createdAt time.Time
	if err := tx.QueryRow(ctx, queryConfigurationInsertAssistantPlanReceipt, ref, scope.organizationID, planID,
		revision, outcome, asJSON(operations), asJSON(conflicts), auditRefs, created, scope.actorID,
	).Scan(&createdAt); err != nil {
		return entity.AssistantPlanReceipt{}, errs.ErrUnavailable
	}
	return entity.AssistantPlanReceipt{Ref: ref, PlanRef: planRef, PlanRevision: revision, Outcome: outcome,
		Operations: operations, Conflicts: conflicts, AuditRefs: auditRefs, CreatedResourceRefs: created, CreatedAt: createdAt}, nil
}
