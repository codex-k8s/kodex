package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// commandOutcome carries a specialized command result with resolved project/resource routing,
// audit/platform-event metadata and runtime maintenance flags for Execute; it does not persist or
// authorize anything by itself.
// commandOutcome несёт специализированный command result с разрешёнными project/resource routing,
// audit/platform-event metadata и runtime maintenance flags для Execute; сам ничего не сохраняет и
// не авторизует.
type commandOutcome struct {
	result                                                                   command.Result
	projectID, projectRef, resourceKind, resourceRef, summary, platformEvent string
	platformAggregateVersion                                                 int64
	platformState                                                            string
	runtimeGraphChanged                                                      bool
	runtimeReadinessChanged                                                  bool
}

// defaultAgentRunConcurrency sets the default concurrency of direct-agent runs; published
// workflows supply their own bounded concurrency instead.
// defaultAgentRunConcurrency задаёт default concurrency прямых agent runs; опубликованные
// workflows передают собственную ограниченную concurrency.
const defaultAgentRunConcurrency = 8

// Execute resolves actor scope, prepares object receipts and opens the ReadCommitted owner
// transaction with project/idempotency locks. Authorized terminal-delete replay is special;
// ordinary replay follows current command authorization and refreshes selected receipt
// metadata/actions. Fresh commands apply state, reconcile workflow launches, write
// audit/outbox/receipt and commit atomically; an empty unchanged runtime poll commits without
// receipt/audit. Prepared-object retention is set only after successful commit; historical ignored
// rollback and commit-error mappings remain unchanged.
// Execute разрешает actor scope, готовит object receipts и открывает ReadCommitted owner
// transaction с project/idempotency locks. Авторизованный terminal-delete replay выделен отдельно;
// обычный replay следует после текущей command authorization и обновляет некоторые receipt
// metadata/actions. Новые команды атомарно применяют state, reconcile workflow launches,
// записывают audit/outbox/receipt и commit; пустой неизменяющий runtime poll фиксируется без
// receipt/audit. Prepared objects сохраняются лишь после успешного commit; прежние ignored
// rollback и commit-error mappings не меняются.
func (repository *Repository) Execute(ctx context.Context, input command.Command) (command.Result, error) {
	scope, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return command.Result{}, err
	}
	prepared, err := repository.prepareCommandObjects(ctx, scope, &input)
	if err != nil {
		return command.Result{}, err
	}
	keepPrepared := false
	defer func() { repository.cleanupPreparedObjects(ctx, prepared, keepPrepared) }()
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return command.Result{}, fmt.Errorf("begin command transaction: %w", errs.ErrUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	launchProjects, err := lockWorkflowLaunchProjects(ctx, tx, scope, input)
	if err != nil {
		return command.Result{}, err
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteLockIdempotencyScope, scope.organizationID, scope.actorID,
		input.Mutation.Operation, input.Mutation.IdempotencyKey); err != nil {
		return command.Result{}, fmt.Errorf("lock command idempotency scope: %w", errs.ErrUnavailable)
	}
	if result, replayed, err := repository.replayDeletedCommand(ctx, tx, scope, input); err != nil {
		return command.Result{}, err
	} else if replayed {
		if err := tx.Commit(ctx); err != nil {
			return command.Result{}, errs.ErrConflict
		}
		return result, executionDeadlineReceiptError(result)
	}
	if err := repository.authorizeCommand(ctx, tx, scope, input); err != nil {
		return command.Result{}, err
	}
	if err := repository.bindFinancialCallbackReceipt(ctx, tx, scope, &input); err != nil {
		return command.Result{}, err
	}
	var storedDigest string
	var storedPayload []byte
	receiptQuery := queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation
	if payload, ok := input.Payload.(command.LeaseInput); input.Kind == command.ReportExecutionProgress && ok && payload.Financial != nil {
		// Долгий активный execution сохраняет semantic receipt; текущее право
		// уже проверено bindFinancialCallbackReceipt, TTL не выдаёт authority.
		receiptQuery = queryProviderFinancialReceipt
	}
	err = tx.QueryRow(ctx, receiptQuery, scope.organizationID, scope.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey).Scan(&storedDigest, &storedPayload)
	if err == nil {
		if storedDigest != input.Mutation.IntentDigest {
			return command.Result{}, errs.ErrIdempotencyReuse
		}
		var result command.Result
		if json.Unmarshal(storedPayload, &result) != nil {
			return command.Result{}, errs.ErrConflict
		}
		if err := repository.validateImpactOwnerReceipt(ctx, tx, scope, input, result); err != nil {
			return command.Result{}, err
		}
		if err := repository.refreshMemoryReceipt(ctx, tx, scope, &result); err != nil {
			return command.Result{}, err
		}
		if err := repository.refreshSkillReceipt(ctx, tx, scope, &result); err != nil {
			return command.Result{}, err
		}
		if err := repository.refreshCFGReceipt(ctx, tx, scope, &result); err != nil {
			return command.Result{}, err
		}
		if exposesActorActions(input.Principal.CallerWorkload) {
			if err := repository.applyResultActionPermissions(ctx, tx, scope, &result, ""); err != nil {
				return command.Result{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return command.Result{}, errs.ErrConflict
		}
		return result, executionDeadlineReceiptError(result)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return command.Result{}, fmt.Errorf("read idempotency receipt: %w", errs.ErrUnavailable)
	}
	if preservesMailboxSpecification(input.Kind) {
		if _, err := tx.Exec(ctx, queryEmailMailboxPublicationLock); err != nil {
			return command.Result{}, errs.ErrUnavailable
		}
	}
	outcome, expiredExecution, err := repository.executionDeadlineCommand(ctx, tx, scope, input)
	if err != nil {
		return command.Result{}, err
	}
	if !expiredExecution {
		outcome, err = repository.applyCommand(ctx, tx, scope, input)
	}
	if err != nil {
		return command.Result{}, err
	}
	if err := repository.reconcileWorkflowLaunches(ctx, tx, scope, launchProjects); err != nil {
		return command.Result{}, err
	}
	if len(launchProjects) > 0 && outcome.result.Run != nil {
		run, graph, err := repository.readRunGraphTx(ctx, tx, scope, outcome.result.Run.Ref)
		if err != nil {
			return command.Result{}, err
		}
		outcome.result.Run, outcome.result.Graph = &run, &graph
	}
	// Пустой опрос runtime является наблюдением, а не устойчивым доменным действием.
	// Receipt и аудит для него превращали бы исправный простой в постоянную запись.
	if input.Kind == command.ClaimExecution && len(outcome.result.RuntimeItems) == 0 && !outcome.runtimeGraphChanged && !outcome.runtimeReadinessChanged {
		if err := tx.Commit(ctx); err != nil {
			return command.Result{}, fmt.Errorf("commit empty runtime claim: %w", errs.ErrConflict)
		}
		return outcome.result, nil
	}
	if outcome.resourceRef == "" {
		return command.Result{}, errs.ErrConflict
	}
	if exposesActorActions(input.Principal.CallerWorkload) {
		if err := repository.applyResultActionPermissions(ctx, tx, scope, &outcome.result, outcome.projectRef); err != nil {
			return command.Result{}, err
		}
	}
	clearDeletedResultActions(&outcome.result)
	auditRef, err := newRef("aud")
	if err != nil {
		return command.Result{}, err
	}
	var project any
	if outcome.projectID != "" {
		project = outcome.projectID
	} else {
		project = nil
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, scope.organizationID, project, scope.actorID, input.Mutation.Operation, outcome.resourceKind, outcome.resourceRef, outcome.summary, input.Principal.CorrelationRef); err != nil {
		return command.Result{}, fmt.Errorf("insert command audit event: %w", errs.ErrUnavailable)
	}
	if outcome.platformEvent != "" {
		if err := repository.emitCommandOutcomePlatformEvent(ctx, tx, scope, outcome); err != nil {
			return command.Result{}, err
		}
	}
	encoded, err := json.Marshal(memoryReceiptMetadata(outcome.result))
	if err != nil {
		return command.Result{}, errs.ErrConflict
	}
	if _, err = tx.Exec(ctx, queryCommandsExecuteInsertIdempotencyReceiptsOrganizationIdOperationIntentDigest, scope.organizationID, scope.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey, input.Mutation.IntentDigest, string(input.Kind), encoded); err != nil {
		return command.Result{}, fmt.Errorf("insert command idempotency receipt: %w", errs.ErrConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return command.Result{}, fmt.Errorf("commit command transaction: %w", errs.ErrConflict)
	}
	keepPrepared = resultContainsPreparedObjects(outcome.result, prepared)
	return outcome.result, executionDeadlineReceiptError(outcome.result)
}

// clearDeletedResultActions clears only next-action hints on deleted connection, schedule or
// runtime-environment results, tolerating a nil result without changing other metadata.
// clearDeletedResultActions очищает лишь next-action hints у deleted connection, schedule либо
// runtime-environment result, допуская nil result без изменения остальных metadata.
func clearDeletedResultActions(result *command.Result) {
	if result == nil {
		return
	}
	if result.Connection != nil && result.Connection.State == "DELETED" {
		result.Connection.NextActions = nil
	}
	if result.Schedule != nil && result.Schedule.State == "DELETED" {
		result.Schedule.NextActions = nil
	}
	if result.RuntimeEnvironment != nil && result.RuntimeEnvironment.State == "DELETED" {
		result.RuntimeEnvironment.NextActions = nil
	}
}

// replayDeletedCommand handles only three terminal-delete kinds after authoritative
// deleted-resource authorization. It requires a usable matching receipt and exact resulting
// version, refreshes deleted connection metadata and clears actions. Missing, malformed,
// wrong-digest or mismatched receipts return a miss rather than supplying authority; the caller
// owns commit.
// replayDeletedCommand обрабатывает лишь три terminal-delete kind после авторитетной проверки
// удалённого ресурса. Требует пригодную совпадающую receipt и точную resulting version, обновляет
// deleted connection metadata и очищает actions. Отсутствующая, повреждённая, wrong-digest либо
// несовпавшая receipt даёт отсутствие replay, не authority; commit принадлежит caller.
func (repository *Repository) replayDeletedCommand(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	input command.Command,
) (command.Result, bool, error) {
	switch input.Kind {
	case command.DeleteConnection, command.DeleteSchedule, command.DeleteRuntimeEnvironment:
	default:
		return command.Result{}, false, nil
	}
	version, err := repository.authorizeDeletedCommand(ctx, tx, current, input)
	if err != nil {
		return command.Result{}, false, err
	}
	if version == 0 {
		return command.Result{}, false, nil
	}
	if input.Mutation.ExpectedVersion == nil {
		return command.Result{}, false, nil
	}
	var storedDigest string
	var storedPayload []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
		current.organizationID, current.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey,
	).Scan(&storedDigest, &storedPayload)
	if errors.Is(err, pgx.ErrNoRows) {
		return command.Result{}, false, nil
	}
	if err != nil {
		return command.Result{}, false, fmt.Errorf("read terminal delete receipt: %w", errs.ErrUnavailable)
	}
	if storedDigest != input.Mutation.IntentDigest {
		return command.Result{}, false, nil
	}
	var result command.Result
	if json.Unmarshal(storedPayload, &result) != nil || !matchesDeletedCommand(input, result) {
		return command.Result{}, false, nil
	}
	if version != *input.Mutation.ExpectedVersion+1 {
		return command.Result{}, false, errs.ErrConflict
	}
	if result.Connection != nil {
		result.Connection.Grants = nil
		if err := attachConnection(ctx, tx, current, result.Connection); err != nil {
			return command.Result{}, false, errs.ErrUnavailable
		}
	}
	clearDeletedResultActions(&result)
	return result, true, nil
}

// matchesDeletedCommand compares a typed delete payload against a complete deleted result, exact
// ref and expected-version-plus-one. The caller must supply nonnil ExpectedVersion; structural
// completeness is not fresh authorization.
// matchesDeletedCommand сравнивает typed delete payload с полным deleted result, точным ref и
// expected-version-plus-one. Caller обязан передать nonnil ExpectedVersion; структурная полнота не
// является свежей authorization.
func matchesDeletedCommand(input command.Command, result command.Result) bool {
	expectedVersion := *input.Mutation.ExpectedVersion + 1
	switch payload := input.Payload.(type) {
	case command.ConnectionInput:
		item := result.Connection
		return input.Kind == command.DeleteConnection && item != nil && item.Ref == payload.Ref &&
			item.Version == expectedVersion && item.State == "DELETED" && item.LifecycleState == "DELETED" &&
			item.DefinitionKey != "" && item.DefinitionName != "" && item.Name != "" &&
			item.DefinitionVersion != "" && item.DefinitionDigest != "" && item.PublicConfiguration != nil &&
			!item.CreatedAt.IsZero() && !item.UpdatedAt.IsZero()
	case command.ScheduleInput:
		item := result.Schedule
		return input.Kind == command.DeleteSchedule && item != nil && item.Ref == payload.Ref &&
			item.Version == expectedVersion && item.State == "DELETED" && item.ProjectRef != "" &&
			item.Name != "" && item.Target.Type != "" && item.Target.Ref != "" && item.Input != nil &&
			!item.CreatedAt.IsZero() && !item.UpdatedAt.IsZero()
	case command.RuntimeEnvironmentLifecycleInput:
		item := result.RuntimeEnvironment
		return input.Kind == command.DeleteRuntimeEnvironment && item != nil && item.Ref == payload.EnvironmentRef &&
			item.Version == expectedVersion && item.State == "DELETED" && item.ProjectRef != "" && item.Name != "" &&
			item.CurrentVersion.Ref != "" && item.CurrentVersion.Digest != "" &&
			!item.CurrentVersion.CreatedAt.IsZero() && !item.UpdatedAt.IsZero()
	default:
		return false
	}
}

// exposesActorActions permits actor-specific next-action enrichment only for the exact
// control-api-gateway workload, leaving machine responses free of browser action hints.
// exposesActorActions разрешает actor-specific next-action enrichment лишь точному
// control-api-gateway workload, оставляя machine responses без browser action hints.
func exposesActorActions(workload string) bool {
	return workload == "control-api-gateway"
}

// applyCommand dispatches the closed command registry to specialized domain handlers in the
// existing transaction, preserving nominal payload checks and rejecting unknown kinds as Invalid;
// no generic lifecycle mutation or commit is introduced.
// applyCommand направляет закрытый command registry специализированным domain handlers в
// существующей транзакции, сохраняя nominal payload checks и отклоняя неизвестные kinds как
// Invalid; не создаёт generic lifecycle mutation либо commit.
func (repository *Repository) applyCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.CreateMemoryRecord, command.ReviseMemoryRecord, command.ArchiveMemoryRecord, command.RestoreMemoryRecord, command.PurgeMemoryRecord:
		return repository.changeMemoryRecord(ctx, tx, scope, input)
	case command.CreateSkillBundleDraft, command.SaveSkillBundleDraft:
		return repository.saveSkillDraft(ctx, tx, scope, input)
	case command.ValidateSkillBundleDraft:
		return repository.validateSkillDraft(ctx, tx, scope, input)
	case command.BindAgentMemoryRecord, command.UnbindAgentMemoryRecord, command.BindAgentSkillBundle, command.UnbindAgentSkillBundle:
		return repository.changeContextBinding(ctx, tx, scope, input)
	case command.ReviewSkillBundleDraft, command.PublishSkillBundleDraft, command.DiscardSkillBundleDraft, command.ArchiveSkillBundle, command.RestoreSkillBundle, command.PurgeSkillBundle:
		return repository.transitionSkillBundle(ctx, tx, scope, input)
	case command.CompleteOnboarding:
		return repository.completeOnboarding(ctx, tx, scope)
	case command.CreateProject:
		return repository.createProject(ctx, tx, scope, input.Payload)
	case command.CreateProjectFile:
		return repository.createProjectFile(ctx, tx, scope, input.Payload)
	case command.CreateProjectFileRevision:
		return repository.createProjectFileRevision(ctx, tx, scope, input)
	case command.UpdateProject:
		return repository.updateProject(ctx, tx, scope, input.Mutation, input.Payload)
	case command.TrashProject, command.RestoreProject, command.PurgeProject:
		return repository.changeProjectLifecycle(ctx, tx, scope, input)
	case command.ResolveProjectPurgeRecovery:
		return repository.resolveProjectPurgeRecovery(ctx, tx, scope, input)
	case command.AddPlatformMembership, command.ChangePlatformMembership, command.RemovePlatformMembership:
		return repository.changePlatformMembership(ctx, tx, scope, input)
	case command.AddMembership, command.ChangeMembership, command.RemoveMembership:
		return repository.changeMembership(ctx, tx, scope, input)
	case command.CreateAgent:
		return repository.createAgent(ctx, tx, scope, input.Payload)
	case command.CreateProjectAssistant:
		return repository.createProjectAssistant(ctx, tx, scope, input)
	case command.CreateAssistantRoleImageRecipe:
		payload, ok := input.Payload.(command.AssistantRoleImageRecipeInput)
		if !ok {
			return commandOutcome{}, errs.ErrInvalid
		}
		return repository.createAssistantRoleImage(ctx, tx, scope, payload)
	case command.CreateSystemAssistantRoleImageRecipe, command.UpdateSystemAssistantRoleImageRecipe:
		return repository.applySystemAssistantImage(ctx, tx, scope, input)
	case command.PublishAssistantRuntimeConfig:
		return repository.publishAssistantRuntimeConfiguration(ctx, tx, scope, input)
	case command.UpdateAssistantRoleImageRecipe:
		return repository.updateAssistantRoleImage(ctx, tx, scope, input)
	case command.UpdateAgent, command.SetAgentEnabled, command.ArchiveAgent:
		return repository.changeAgent(ctx, tx, scope, input)
	case command.CreateRuntimeEnvironmentDraft, command.CreateOrganizationRuntimeEnvironmentDraft, command.SaveRuntimeEnvironmentDraft, command.ValidateRuntimeEnvironmentDraft,
		command.PublishRuntimeEnvironmentDraft, command.DiscardRuntimeEnvironmentDraft:
		return repository.changeRuntimeEnvironmentDraft(ctx, tx, scope, input)
	case command.PrepareEnvironmentDraftImpact:
		return repository.prepareEnvironmentDraftImpact(ctx, tx, scope, input)
	case command.PrepareInstructionsImpact:
		return repository.prepareInstructionsImpact(ctx, tx, scope, input)
	case command.RebindRuntimeEnvironment:
		return repository.rebindRuntimeEnvironment(ctx, tx, scope, input)
	case command.RebindRuntimeSecret:
		return repository.rebindRuntimeSecret(ctx, tx, scope, input)
	case command.BindInteractionIdentity, command.RevokeInteractionIdentity:
		return repository.changeInteractionIdentity(ctx, tx, scope, input)
	case command.ReconcileEmailEffect:
		return repository.reconcileEmailEffect(ctx, tx, scope, input)
	case command.ReportEmailEffect:
		return repository.reportEmailEffect(ctx, tx, scope, input)
	case command.SetAgentAvatar, command.RemoveAgentAvatar:
		return repository.changeAgentAvatar(ctx, tx, scope, input)
	case command.CreateInstructions, command.ValidateInstructions, command.PublishInstructions, command.RollbackInstructions:
		return repository.changeInstructions(ctx, tx, scope, input)
	case command.PublishAgentRuntimeConfig, command.CreateConfigOverlayDraft, command.ValidateConfigOverlayDraft,
		command.PublishConfigOverlayDraft, command.RollbackConfigOverlay, command.CreateRuntimeEnvironment,
		command.PublishRuntimeEnvironment, command.RollbackRuntimeEnvironment, command.SetRuntimeEnvironmentEnabled,
		command.DeleteRuntimeEnvironment, command.BindAgentRuntimeEnvironment:
		return repository.changeRuntimeConfiguration(ctx, tx, scope, input)
	case command.ChangeAgentCapability, command.ChangeAgentGrant:
		return repository.changeAgentBinding(ctx, tx, scope, input)
	case command.CreateWorkflow, command.UpdateWorkflow, command.ValidateWorkflow, command.PublishWorkflow, command.ArchiveWorkflow:
		return repository.changeWorkflow(ctx, tx, scope, input)
	case command.LaunchRun:
		return repository.launchRun(ctx, tx, scope, input)
	case command.AddSessionTurn:
		return repository.addSessionTurn(ctx, tx, scope, input)
	case command.CancelRun, command.RetryRun:
		return repository.changeRun(ctx, tx, scope, input)
	case command.ResolveOwnerGate:
		return repository.resolveGate(ctx, tx, scope, input)
	case command.ChangeArtifactBinding:
		return repository.changeArtifactBinding(ctx, tx, scope, input)
	case command.DeleteArtifact, command.RestoreArtifact:
		return repository.changeArtifactLifecycle(ctx, tx, scope, input)
	case command.CreateAttachmentSetDraft, command.AddAttachmentSetItems,
		command.RemoveAttachmentSetItems, command.FinalizeAttachmentSet:
		return repository.changeAttachmentSet(ctx, tx, scope, input)
	case command.CreateSchedule, command.UpdateSchedule, command.SetScheduleEnabled, command.ArchiveSchedule, command.DeleteSchedule:
		return repository.changeSchedule(ctx, tx, scope, input)
	case command.CreateProviderAccount, command.StartProviderDeviceAuth, command.AuthorizeProviderAPIKey,
		command.RefreshProviderAuthorization, command.VerifyProviderAuthorization, command.CancelProviderAccountQueuedWork, command.RevokeProviderAccount, command.DeleteProviderAccount, command.SetProviderAccountEnabled, command.SetProviderAccountConcurrency:
		return repository.changeProviderAccount(ctx, tx, scope, input)
	case command.CreateProjectAssistantIntegrationConnection:
		return repository.createProjectAssistantConnection(ctx, tx, scope, input)
	case command.CreateConnection, command.UpdateConnection, command.DeleteConnection, command.ConfigureConnectionCredential,
		command.TestConnection, command.SetConnectionEnabled, command.ChangeIntegrationGrant, command.ChangeSystemAssistantIntegrationGrant, command.ChangeProjectAssistantIntegrationGrant:
		return repository.changeConnection(ctx, tx, scope, input)
	case command.ConfigureEmailCredential:
		return repository.configureEmailCredential(ctx, tx, scope, input)
	case command.CreateAssistantConversation, command.UpdateAssistantConversation, command.ArchiveAssistantConversation, command.RestoreAssistantConversation, command.PurgeAssistantConversation, command.MoveAssistantConversationToProject, command.AddAssistantTurn, command.CancelAssistantTurn,
		command.UpdateAssistantPlan, command.ValidateAssistantPlan, command.ApplyAssistantPlan, command.RejectAssistantPlan,
		command.UpdateAssistantInstructions, command.RecoverAssistant:
		return repository.changeAssistant(ctx, tx, scope, input)
	case command.ClaimExecution, command.RenewExecution, command.ReportExecutionProgress, command.CommitProviderCredentialRefresh,
		command.CompleteExecution,
		command.DelegateExecution, command.LaunchWorkflowExecution, command.ProposeAssistantPlan, command.ProposeAssistantMetadata,
		command.ProposeRunMetadata, command.RecordRunToolCall:
		return repository.changeExecution(ctx, tx, scope, input)
	case command.CompleteSessionSnapshot, command.CompleteSessionRestore,
		command.CompleteSessionPVCDeletion, command.CompleteSessionObjectDeletion,
		command.FailSessionArchiveTask:
		return repository.changeSessionArchive(ctx, tx, scope, input)
	case command.MaterializeOccurrence, command.FailScheduleOccurrence:
		return repository.changeOccurrence(ctx, tx, scope, input)
	case command.CompleteConnectionTest:
		return repository.completeIntegrationConnectionTest(ctx, tx, scope, input)
	case command.CompleteIntegrationInvocation:
		return repository.completeIntegrationInvocation(ctx, tx, scope, input)
	case command.CompleteInteractionDelivery:
		return repository.completeInteractionDelivery(ctx, tx, scope, input)
	case command.AcceptInteractionMessage:
		return repository.acceptInteractionMessage(ctx, tx, scope, input)
	case command.CreateAccessRole, command.CreateAccessRoleVersion, command.ArchiveAccessRole,
		command.CreateAccessBinding, command.ChangeAccessBinding, command.RevokeAccessBinding:
		return repository.applyAccessCommand(ctx, tx, scope, input)
	case command.ConfigureRoleImageGitSource, command.ConfigureIntegrationDefinitionGitSource, command.RefreshRoleImageGitSource, command.RefreshIntegrationDefinitionGitSource:
		return repository.changeConfigurationSource(ctx, tx, scope, input)
	case command.PrepareRoleImageGitWriteBack, command.PrepareIntegrationDefinitionGitWriteBack, command.ApproveManagedConfigurationGitWriteBack, command.RejectManagedConfigurationGitWriteBack, command.CancelManagedConfigurationGitWriteBack:
		return repository.changeConfigurationWriteBack(ctx, tx, scope, input)
	case command.CreateEmailMailboxDraft, command.SaveEmailMailboxDraft, command.ValidateEmailMailboxDraft,
		command.PublishEmailMailboxDraft, command.DiscardEmailMailboxDraft:
		return repository.changeEmailMailbox(ctx, tx, scope, input)
	case command.BindEmailMailboxConfiguration, command.UnbindEmailMailboxConfiguration:
		return repository.bindEmailMailbox(ctx, tx, scope, input)
	case command.CreatePromptTemplateDraft, command.ValidatePromptTemplateDraft,
		command.PublishPromptTemplateDraft, command.RebindPromptTemplate,
		command.SavePromptTemplateDraft,
		command.DiscardPromptTemplateDraft,
		command.SaveRoleImageRevisionDraft,
		command.DiscardRoleImageRevisionDraft,
		command.SaveIntegrationDefinitionDraft,
		command.DiscardIntegrationDefinitionDraft,
		command.SaveSystemSTTConfigurationDraft,
		command.DiscardSystemSTTConfigurationDraft,
		command.CreateRoleImageRevisionDraft, command.ValidateRoleImageRevision,
		command.PublishRoleImageRevision, command.RebindRoleImage, command.PrepareRoleImageImpactPlan, command.PreparePromptTemplateImpact,
		command.CreateIntegrationDefinition, command.ValidateIntegrationDefinition,
		command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
		command.CreateSystemSTTDraft, command.ValidateSystemSTTDraft,
		command.PublishSystemSTTDraft, command.RebindSystemSTT,
		command.DetachGitManagedConfiguration, command.CopyGitManagedConfiguration,
		command.CopyRoleImageConfiguration, command.CopyIntegrationDefinitionConfiguration, command.ArchiveRoleImageConfiguration, command.ArchiveIntegrationDefinitionConfiguration:
		return repository.changeManagedConfiguration(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

// completeOnboarding updates installation onboarding completion and returns organization routing
// with an assistant-change invalidation outcome; it does not run bootstrap or prove runtime
// readiness.
// completeOnboarding обновляет installation onboarding completion и возвращает organization
// routing с assistant-change invalidation outcome; не запускает bootstrap и не доказывает runtime
// readiness.
func (repository *Repository) completeOnboarding(ctx context.Context, tx pgx.Tx, scope scope) (commandOutcome, error) {
	if _, err := tx.Exec(ctx, queryCommandsCompleteonboardingUpdateInstallationOnboardingCompletedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{CreatedRefs: []string{scope.organizationRef}}, resourceKind: "INSTALLATION", resourceRef: scope.organizationRef, summary: "i18n:ONBOARDING_COMPLETED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

// createProject validates a typed nonempty bounded name, defaults language to ru, inserts the
// project and grants its creator the existing full project permission set in the caller
// transaction. The returned routing/actions do not replace outer command authorization.
// createProject проверяет typed непустое ограниченное name, назначает ru при отсутствии language,
// вставляет project и выдаёт создателю существующий полный project permission set в транзакции
// caller. Возвращаемые routing/actions не заменяют внешнюю command authorization.
func (repository *Repository) createProject(ctx context.Context, tx pgx.Tx, scope scope, payload any) (commandOutcome, error) {
	input, ok := payload.(command.ProjectInput)
	if !ok || strings.TrimSpace(input.Name) == "" || len(input.Name) > 160 {
		return commandOutcome{}, errs.ErrInvalid
	}
	ref, err := newRef("prj")
	if err != nil {
		return commandOutcome{}, err
	}
	language := input.Language
	if language == "" {
		language = "ru"
	}
	var item entity.Project
	err = tx.QueryRow(ctx, queryCommandsCreateprojectInsertProjectsRefNameLanguage, ref, scope.organizationID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Purpose), language, scope.actorID).Scan(&item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	membershipRef, _ := newRef("abnd")
	if _, err = tx.Exec(ctx, queryCommandsCreateprojectInsertMembershipsRefProjectIdRole, membershipRef, scope.organizationID, ref, scope.actorID, allPermissions()); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.NextActions = []string{"OPEN", "EDIT"}
	return commandOutcome{result: command.Result{Project: &item}, projectID: mustProjectID(ctx, tx, scope.organizationID, ref), projectRef: ref, resourceKind: "PROJECT", resourceRef: ref, summary: "i18n:PROJECT_CREATED", platformEvent: "PROJECT_CHANGED"}, nil
}

// updateProject requires typed ref/expected version and performs the tenant/OCC update with
// trimmed name/purpose, returning canonical metadata and event routing. No row maps to
// VersionMismatch and other write errors retain the shared mapper.
// updateProject требует typed ref/expected version и выполняет tenant/OCC update с trim
// name/purpose, возвращая канонические metadata и event routing. Отсутствие строки даёт
// VersionMismatch, остальные write errors сохраняют общий mapper.
func (repository *Repository) updateProject(ctx context.Context, tx pgx.Tx, scope scope, mutation value.Mutation, payload any) (commandOutcome, error) {
	input, ok := payload.(command.ProjectInput)
	if !ok || input.Ref == "" || mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var item entity.Project
	var projectID string
	err := tx.QueryRow(ctx, queryCommandsUpdateprojectUpdateProjectsNamePurposeLanguage, scope.organizationID, input.Ref, *mutation.ExpectedVersion, strings.TrimSpace(input.Name), strings.TrimSpace(input.Purpose), input.Language).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.Language, &item.Lifecycle, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	item.NextActions = []string{"OPEN", "EDIT"}
	return commandOutcome{result: command.Result{Project: &item}, projectID: projectID, projectRef: item.Ref, resourceKind: "PROJECT", resourceRef: item.Ref, summary: "i18n:PROJECT_UPDATED", platformEvent: "PROJECT_CHANGED"}, nil
}

// changeMembership handles project membership add/change/remove after project resolution and
// permission-grant eligibility for nonremoval. Change/remove lock exact project bindings, check
// OCC and reject self mutation; distinct update/deactivate SQL and no-row mappings remain
// explicit. It returns membership action hints and one outer event outcome without committing.
// changeMembership обрабатывает project membership add/change/remove после project resolution и
// permission-grant eligibility для nonremoval. Change/remove блокируют точные project bindings,
// сверяют OCC и отклоняют self mutation; разные update/deactivate SQL и no-row mappings остаются
// явными. Возвращает membership action hints и один внешний event outcome без commit.
func (repository *Repository) changeMembership(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.MembershipInput)
	if !ok || payload.ProjectRef == "" || (input.Kind != command.RemoveMembership && !validProjectPermissions(payload.Permissions)) {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
	}
	if input.Kind != command.RemoveMembership {
		var allowed bool
		if err := tx.QueryRow(ctx, queryProjectMembershipCanGrant, pgx.StrictNamedArgs{
			"actor_platform_role":   scope.role,
			"organization_id":       scope.organizationID,
			"project_id":            projectID,
			"actor_id":              scope.actorID,
			"requested_permissions": payload.Permissions,
		}).Scan(&allowed); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !allowed {
			return commandOutcome{}, errs.ErrForbidden
		}
	}
	var item entity.Membership
	switch input.Kind {
	case command.AddMembership:
		if payload.UserRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, err := newRef("abnd")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		err = tx.QueryRow(ctx, queryProjectMembershipInsert, pgx.StrictNamedArgs{
			"membership_ref":  ref,
			"organization_id": scope.organizationID,
			"project_id":      projectID,
			"user_ref":        payload.UserRef,
			"permissions":     payload.Permissions,
			"actor_id":        scope.actorID,
		}).Scan(
			&item.Ref, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked,
			&item.User.Active, &item.Role, &item.Permissions, &item.Active, &item.Version,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.ChangeMembership:
		if input.Mutation.ExpectedVersion == nil || payload.MembershipRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		membershipID, subjectID, err := lockProjectMembership(ctx, tx, scope.organizationID, projectID, payload.MembershipRef, &item)
		if err != nil {
			return commandOutcome{}, err
		}
		if item.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if subjectID == scope.actorID {
			return commandOutcome{}, errs.ErrForbidden
		}
		err = tx.QueryRow(ctx, queryProjectMembershipUpdate, pgx.StrictNamedArgs{
			"membership_id":    membershipID,
			"organization_id":  scope.organizationID,
			"project_id":       projectID,
			"expected_version": *input.Mutation.ExpectedVersion,
			"permissions":      payload.Permissions,
			"active":           payload.Active,
			"actor_id":         scope.actorID,
		}).Scan(&item.Ref, &item.Permissions, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.RemoveMembership:
		if input.Mutation.ExpectedVersion == nil || payload.MembershipRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		membershipID, subjectID, err := lockProjectMembership(ctx, tx, scope.organizationID, projectID, payload.MembershipRef, &item)
		if err != nil {
			return commandOutcome{}, err
		}
		if item.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if subjectID == scope.actorID {
			return commandOutcome{}, errs.ErrForbidden
		}
		err = tx.QueryRow(ctx, queryProjectMembershipDeactivate, pgx.StrictNamedArgs{
			"membership_id":    membershipID,
			"organization_id":  scope.organizationID,
			"project_id":       projectID,
			"expected_version": *input.Mutation.ExpectedVersion,
		}).Scan(&item.Ref, &item.Permissions, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrConflict
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	item.ProjectRef = payload.ProjectRef
	item.NextActions = projectMembershipActions(scope, item)
	return commandOutcome{result: command.Result{Membership: &item}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "MEMBERSHIP", resourceRef: item.Ref, summary: "i18n:PROJECT_ACCESS_UPDATED", platformEvent: "MEMBERSHIP_CHANGED"}, nil
}

// changePlatformMembership handles organization membership add/change/remove with closed roles,
// owner-role restrictions, OCC, self-demotion/removal prevention and last-owner protection.
// Deactivation also revokes project memberships, while returned permissions are empty;
// project-specific grant policy is not reused here.
// changePlatformMembership обрабатывает organization membership add/change/remove с закрытыми
// roles, owner-role restrictions, OCC, запретом self-demotion/removal и last-owner protection.
// Deactivation также отзывает project memberships, возвращаемые permissions пусты;
// project-specific grant policy здесь не используется.
func (repository *Repository) changePlatformMembership(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.PlatformMembershipInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind != command.RemovePlatformMembership && !validPlatformRole(payload.Role) {
		return commandOutcome{}, errs.ErrInvalid
	}
	if scope.role != "OWNER" && payload.Role == "OWNER" {
		return commandOutcome{}, errs.ErrForbidden
	}
	var item entity.Membership
	var membershipID, subjectID string
	switch input.Kind {
	case command.AddPlatformMembership:
		if payload.UserRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		ref, err := newRef("abnd")
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		err = tx.QueryRow(ctx, queryPlatformMembershipInsert, pgx.StrictNamedArgs{
			"membership_ref":  ref,
			"organization_id": scope.organizationID,
			"user_ref":        payload.UserRef,
			"platform_role":   payload.Role,
			"actor_id":        scope.actorID,
		}).Scan(
			&item.Ref, &subjectID, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked,
			&item.User.Active, &item.Role, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.ChangePlatformMembership:
		if input.Mutation.ExpectedVersion == nil || payload.MembershipRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		var err error
		membershipID, subjectID, err = lockPlatformMembership(ctx, tx, scope.organizationID, payload.MembershipRef, &item)
		if err != nil {
			return commandOutcome{}, err
		}
		if item.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if scope.role != "OWNER" && item.Role == "OWNER" {
			return commandOutcome{}, errs.ErrForbidden
		}
		if subjectID == scope.actorID && (!payload.Active || payload.Role != item.Role) {
			return commandOutcome{}, errs.ErrForbidden
		}
		if err := repository.protectLastOwner(ctx, tx, scope.organizationID, membershipID, item, payload.Role, payload.Active); err != nil {
			return commandOutcome{}, err
		}
		err = tx.QueryRow(ctx, queryPlatformMembershipUpdate, pgx.StrictNamedArgs{
			"membership_id":    membershipID,
			"organization_id":  scope.organizationID,
			"expected_version": *input.Mutation.ExpectedVersion,
			"platform_role":    payload.Role,
			"active":           payload.Active,
		}).Scan(&item.Ref, &item.Role, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.RemovePlatformMembership:
		if input.Mutation.ExpectedVersion == nil || payload.MembershipRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		var err error
		membershipID, subjectID, err = lockPlatformMembership(ctx, tx, scope.organizationID, payload.MembershipRef, &item)
		if err != nil {
			return commandOutcome{}, err
		}
		if item.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if (scope.role != "OWNER" && item.Role == "OWNER") || subjectID == scope.actorID {
			return commandOutcome{}, errs.ErrForbidden
		}
		if err := repository.protectLastOwner(ctx, tx, scope.organizationID, membershipID, item, item.Role, false); err != nil {
			return commandOutcome{}, err
		}
		err = tx.QueryRow(ctx, queryPlatformMembershipDeactivate, pgx.StrictNamedArgs{
			"membership_id":    membershipID,
			"organization_id":  scope.organizationID,
			"expected_version": *input.Mutation.ExpectedVersion,
		}).Scan(&item.Ref, &item.Role, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrConflict
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	if !item.Active {
		if _, err := tx.Exec(ctx, queryPlatformMembershipDeactivateProjects, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"subject_id":      subjectID,
		}); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	item.Permissions = []string{}
	item.NextActions = platformMembershipActions(scope, item)
	return commandOutcome{result: command.Result{Membership: &item}, resourceKind: "PLATFORM_MEMBERSHIP", resourceRef: item.Ref, summary: "i18n:PLATFORM_ACCESS_UPDATED", platformEvent: "PLATFORM_MEMBERSHIP_CHANGED"}, nil
}

// protectLastOwner checks other active in-window OWNER bindings only when an active owner would be
// demoted or disabled, returning Conflict for zero and Unavailable on read error. It relies on
// caller locking and does not independently prove every subject eligibility.
// protectLastOwner считает прочие active in-window OWNER bindings лишь при понижении либо
// отключении активного owner, возвращая Conflict при нуле и Unavailable при read error. Полагается
// на caller locking и не доказывает отдельно eligibility каждого subject.
func (repository *Repository) protectLastOwner(ctx context.Context, tx pgx.Tx, organizationID, membershipID string, current entity.Membership, nextRole string, nextActive bool) error {
	if current.Role != "OWNER" || !current.Active || (nextRole == "OWNER" && nextActive) {
		return nil
	}
	var remaining int
	if err := tx.QueryRow(ctx, queryPlatformMembershipCountOtherActiveOwners, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"membership_id":   membershipID,
	}).Scan(&remaining); err != nil {
		return errs.ErrUnavailable
	}
	if remaining == 0 {
		return errs.ErrConflict
	}
	return nil
}

// validPlatformRole accepts exactly OWNER, ADMINISTRATOR, OPERATOR, MEMBER or AUDITOR without case
// normalization or role assignment.
// validPlatformRole принимает ровно OWNER, ADMINISTRATOR, OPERATOR, MEMBER либо AUDITOR без
// нормализации регистра или назначения роли.
func validPlatformRole(role string) bool {
	switch role {
	case "OWNER", "ADMINISTRATOR", "OPERATOR", "MEMBER", "AUDITOR":
		return true
	default:
		return false
	}
}

// validProjectPermissions requires a nonempty bounded duplicate-free subset of the canonical
// legacy permission list containing VIEW; it validates shape, not the actor right to grant those
// permissions.
// validProjectPermissions требует непустое ограниченное подмножество канонического legacy
// permission list без повторов и с VIEW; проверяет форму, не право actor выдавать permissions.
func validProjectPermissions(permissions []string) bool {
	if len(permissions) == 0 || len(permissions) > len(allPermissions()) {
		return false
	}
	allowed := make(map[string]struct{}, len(allPermissions()))
	for _, permission := range allPermissions() {
		allowed[permission] = struct{}{}
	}
	seen := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		if _, ok := allowed[permission]; !ok {
			return false
		}
		if _, duplicate := seen[permission]; duplicate {
			return false
		}
		seen[permission] = struct{}{}
	}
	_, canView := seen["VIEW"]
	return canView
}

// createAgent checks project, initial capability authority, avatar and enabled runtime, resolves
// or creates a role and inserts the agent with canonical runtime bootstrap. It creates published
// instructions and binding, validates prompt context and returns initial metadata/actions in the
// same transaction; no provider execution or runtime Pod is launched.
// createAgent сверяет project, initial capability authority, avatar и enabled runtime, разрешает
// либо создаёт role и вставляет agent с каноническим runtime bootstrap. Создаёт published
// instructions и binding, проверяет prompt context и возвращает initial metadata/actions в той же
// транзакции; provider execution или runtime Pod не запускаются.
func (repository *Repository) createAgent(ctx context.Context, tx pgx.Tx, scope scope, payload any) (commandOutcome, error) {
	input, ok := payload.(command.AgentInput)
	if !ok || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Instructions) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, input.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err := repository.authorizeInitialAgentCapabilities(ctx, tx, scope, input.ProjectRef, input.InitialCapabilities); err != nil {
		return commandOutcome{}, err
	}
	avatarURL, err := repository.validateAvatarArtifact(ctx, tx, scope, projectID, input.AvatarURL)
	if err != nil {
		return commandOutcome{}, err
	}
	input.AvatarURL = avatarURL
	runtimeKey := input.RuntimeRef
	if runtimeKey == "" {
		runtimeKey = defaultRuntimeKey
	}
	runtime, err := resolveEnabledRuntime(ctx, tx, runtimeKey)
	if err != nil {
		return commandOutcome{}, err
	}
	var roleID, roleRef, roleName string
	if input.RoleDefinitionRef != "" {
		err := tx.QueryRow(ctx, queryCommandsCreateagentSelectRoleDefinitionsOrganizationIdProjectIdRef,
			scope.organizationID, projectID, input.RoleDefinitionRef).Scan(&roleID, &roleRef, &roleName)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else {
		roleRef, _ = newRef("role")
		roleName = strings.TrimSpace(input.Name)
		if err := tx.QueryRow(ctx, queryCommandsCreateagentInsertRoleDefinitionsRefOrganizationIdProjectIdName,
			roleRef, scope.organizationID, projectID, roleName, strings.TrimSpace(input.RoleDescription), scope.actorID,
		).Scan(&roleID, &roleRef, &roleName); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	ref, _ := newRef("agt")
	var agentID string
	var item entity.Agent
	err = tx.QueryRow(ctx, queryCommandsCreateagentInsertAgentsRefProjectIdPurpose, ref, scope.organizationID, projectID, roleID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Purpose), strings.TrimSpace(input.RoleDescription), strings.TrimSpace(input.AvatarURL), runtimeKey, scope.actorID, input.InitialCapabilities).Scan(&agentID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	_, avatarArtifactRef, _ := parseAvatarArtifactURL(item.AvatarURL)
	item.Avatar, err = repository.syncAgentAvatar(ctx, tx, scope, item.Ref, avatarArtifactRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.bootstrapAgentRuntime(ctx, tx, scope.organizationID, agentID, projectID, runtime, scope.actorID); err != nil {
		return commandOutcome{}, fmt.Errorf("bootstrap agent runtime configuration: %v: %w", err, errs.ErrUnavailable)
	}
	instructionRef, _ := newRef("ins")
	digest := sha256.Sum256([]byte(input.Instructions))
	publishedAt := time.Now().UTC()
	if _, err = tx.Exec(ctx, queryCommandsCreateagentInsertInstructionVersionsRefAgentIdState, instructionRef, scope.organizationID, agentID, input.Instructions, hex.EncodeToString(digest[:]), scope.actorID, publishedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	bindingRef, bindingVersion, err := assignInstructionBinding(ctx, tx, scope.organizationID, agentID, instructionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.validateAgentPromptContextTx(ctx, tx, scope, item.Ref, input.Instructions, false); err != nil {
		return commandOutcome{}, err
	}
	item.InstructionBinding = &entity.AgentInstructionsBinding{Ref: bindingRef, Version: bindingVersion, RevisionRef: instructionRef, Effective: true}
	item.ProjectRef = input.ProjectRef
	item.RoleDefinitionRef = roleRef
	item.RoleDefinitionName = roleName
	item.RuntimeKey = runtimeKey
	item.RuntimeName = runtime.Name
	item.Provider = runtime.Provider
	item.Model = runtime.Model
	item.RuntimeRevision = runtime.RuntimeRevision
	item.Capabilities = append([]string(nil), input.InitialCapabilities...)
	item.PublishedInstructions = &entity.InstructionVersion{Ref: instructionRef, VersionNumber: 1, State: "PUBLISHED", Content: input.Instructions, Digest: hex.EncodeToString(digest[:]), CreatedAt: publishedAt, PublishedAt: &publishedAt}
	item.NextActions = agentActions(item, true, true)
	return commandOutcome{result: command.Result{Agent: &item}, projectID: projectID, projectRef: input.ProjectRef, resourceKind: "AGENT", resourceRef: ref, summary: "i18n:AGENT_CREATED_READY", platformEvent: "AGENT_CHANGED"}, nil
}

// resolveEnabledRuntime reads the enabled runtime profile selection, returning zero/NotFound for
// no row and zero/Unavailable for read failure. Ready marks this catalog selection only and is not
// an external provider probe.
// resolveEnabledRuntime читает enabled runtime profile selection, возвращая zero/NotFound при
// отсутствии строки и zero/Unavailable при read failure. Ready помечает лишь этот catalog
// selection и не является внешним provider probe.
func resolveEnabledRuntime(ctx context.Context, tx pgx.Tx, ref string) (entity.RuntimeSelection, error) {
	var runtime entity.RuntimeSelection
	err := tx.QueryRow(ctx, queryCommandsResolveEnabledRuntimeProfile, ref).Scan(&runtime.Ref, &runtime.Name, &runtime.Provider, &runtime.Model, &runtime.RuntimeRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeSelection{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeSelection{}, errs.ErrUnavailable
	}
	runtime.Ready = true
	return runtime, nil
}

// mapWriteError maps serialization/deadlock conflicts to the existing retry marker plus
// Unavailable, uniqueness to Conflict and foreign/check constraints to Invalid. It preserves
// legacy message-substring fallbacks and returns Unavailable for unknown errors without exposing
// raw driver text.
// mapWriteError отображает serialization/deadlock conflicts в прежний retry marker с Unavailable,
// uniqueness в Conflict, foreign/check constraints в Invalid. Сохраняет legacy message-substring
// fallbacks и возвращает Unavailable для неизвестных ошибок без выдачи raw driver text.
func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	if serializableTransactionConflict(err) {
		return errors.Join(errSerializableTransactionRetry, errs.ErrUnavailable)
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		switch pgError.Code {
		case "23505":
			return errs.ErrConflict
		case "23503", "23514":
			return errs.ErrInvalid
		}
	}
	message := err.Error()
	if strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint") {
		return errs.ErrConflict
	}
	if strings.Contains(message, "violates check constraint") || strings.Contains(message, "violates foreign key") {
		return errs.ErrInvalid
	}
	return errs.ErrUnavailable
}

// mustProjectID returns the tenant/ref project ID or empty on any query/scan error, preserving the
// historical absent-versus-infrastructure conflation; it does not authorize access.
// mustProjectID возвращает tenant/ref project ID либо пустую строку при любой query/scan error,
// сохраняя прежнее смешение отсутствия и инфраструктурного отказа; доступ не авторизует.
func mustProjectID(ctx context.Context, tx pgx.Tx, organizationID, ref string) string {
	var id string
	if tx.QueryRow(ctx, queryCommandsMustprojectidSelectProjectsOrganizationIdRefLifecycle, organizationID, ref).Scan(&id) != nil {
		return ""
	}
	return id
}

// changeAgent handles update/enable/archive with typed ref/OCC, runtime-profile immutability,
// avatar validation and protected assistant archive checks. It reads authoritative
// runtime/role/avatar metadata and suspends target schedules on disable/archive, retaining
// distinct Conflict/VersionMismatch mappings and caller transaction ownership.
// changeAgent обрабатывает update/enable/archive с typed ref/OCC, неизменностью runtime-profile,
// avatar validation и protected assistant archive checks. Читает авторитетные runtime/role/avatar
// metadata и при disable/archive приостанавливает target schedules, сохраняя разные
// Conflict/VersionMismatch mappings и caller transaction ownership.
func (repository *Repository) changeAgent(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentInput)
	if !ok || payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var item entity.Agent
	var projectID string
	switch input.Kind {
	case command.UpdateAgent:
		locked, lockErr := repository.lockRuntimeAgent(ctx, tx, scope, payload.Ref)
		if lockErr != nil {
			return commandOutcome{}, lockErr
		}
		if payload.RuntimeRef != "" && payload.RuntimeRef != locked.runtimeProfileRef {
			return commandOutcome{}, errs.ErrInvalid
		}
		var currentAvatarURL string
		if avatarErr := tx.QueryRow(ctx, queryCommandsSelectAgentAvatarURL, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"agent_ref":       payload.Ref,
		}).Scan(&currentAvatarURL); avatarErr != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		avatarURL, avatarErr := repository.validateAvatarUpdate(
			ctx, tx, scope, locked.projectID, currentAvatarURL, payload.AvatarURL,
		)
		if avatarErr != nil {
			return commandOutcome{}, avatarErr
		}
		payload.AvatarURL = avatarURL
		err := tx.QueryRow(ctx, queryCommandsChangeagentUpdateAgentsNamePurposeRoleDescription, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Name, payload.Purpose, payload.RoleDescription, payload.AvatarURL, payload.RuntimeRef, payload.RoleDefinitionRef).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		_, avatarArtifactRef, _ := parseAvatarArtifactURL(item.AvatarURL)
		item.Avatar, err = repository.syncAgentAvatar(ctx, tx, scope, item.Ref, avatarArtifactRef)
		if err != nil {
			return commandOutcome{}, err
		}
	case command.SetAgentEnabled:
		state := "DISABLED"
		if payload.Enabled {
			state = "READY"
		}
		err := tx.QueryRow(ctx, queryCommandsChangeagentEnableAgent, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion, payload.Enabled, state).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.ArchiveAgent:
		if err := repository.rejectAssistantAgentArchive(ctx, tx, scope, payload.Ref); err != nil {
			return commandOutcome{}, err
		}
		err := tx.QueryRow(ctx, queryCommandsChangeagentArchiveAgent, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrConflict
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	if err := tx.QueryRow(ctx, queryCommandsChangeagentSelectAgentsRef, item.Ref).Scan(&item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs, &item.Avatar.ArtifactRef, &item.Avatar.ArtifactRevision); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	setAgentAvatarReadback(&item)
	if input.Kind == command.ArchiveAgent || input.Kind == command.SetAgentEnabled && !payload.Enabled {
		if err := repository.suspendTargetSchedules(ctx, tx, scope, projectID, item.ProjectRef, "AGENT", item.Ref); err != nil {
			return commandOutcome{}, err
		}
	}
	item.NextActions = agentActions(item, true, true)
	return commandOutcome{result: command.Result{Agent: &item}, projectID: projectID, projectRef: item.ProjectRef, resourceKind: "AGENT", resourceRef: item.Ref, summary: "i18n:AGENT_UPDATED", platformEvent: "AGENT_CHANGED"}, nil
}

// suspendTargetSchedules updates schedules for the exact tenant/project/target and emits one
// schedule invalidation only when the SQL reports a change; it does not launch or claim an
// occurrence.
// suspendTargetSchedules обновляет schedules точных tenant/project/target и создаёт одну schedule
// invalidation лишь при изменении по SQL readback; occurrence не запускает и не выдаёт.
func (repository *Repository) suspendTargetSchedules(ctx context.Context, tx pgx.Tx, scope scope, projectID, projectRef, targetType, targetRef string) error {
	var suspended int64
	var scheduleRef string
	if err := tx.QueryRow(ctx, queryCommandsSuspendTargetSchedules, scope.organizationID, projectID, targetType, targetRef).Scan(&suspended, &scheduleRef); err != nil {
		return errs.ErrUnavailable
	}
	if suspended == 0 {
		return nil
	}
	return repository.emitPlatformEvent(ctx, tx, scope, "SCHEDULE_CHANGED", projectRef, scheduleRef, "i18n:SCHEDULE_UPDATED")
}

// changeInstructions handles ordinary agent draft create/validate/publish/forward rollback with
// protected-system rejection, agent OCC and prompt-context checks. Publication applies the exact
// impact plan/binding receipts; rollback creates a new published revision. It increments agent
// version and returns an event outcome, preserving historical read-error handling rather than
// silently fixing it.
// changeInstructions обрабатывает обычные agent draft create/validate/publish/forward rollback с
// protected-system rejection, agent OCC и prompt-context checks. Publication применяет точные
// impact plan/binding receipts; rollback создаёт новую published revision. Увеличивает agent
// version и возвращает event outcome, сохраняя прежнюю обработку read errors без неявного
// исправления.
func (repository *Repository) changeInstructions(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentInput)
	if !ok || payload.Ref == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var agentID, projectID, projectRef, systemKey string
	var agentVersion int64
	if err := tx.QueryRow(ctx, queryCommandsChangeinstructionsSelectAgentsOrganizationIdRef, scope.organizationID, payload.Ref).Scan(&agentID, &projectID, &projectRef, &systemKey, &agentVersion); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if systemKey != "" {
		return commandOutcome{}, errs.ErrProtected
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var impactPlan *entity.RevisionImpactPlan
	switch input.Kind {
	case command.CreateInstructions:
		if strings.TrimSpace(payload.Instructions) == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		digest := sha256.Sum256([]byte(payload.Instructions))
		tag, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateCurrentDraft, agentID, payload.Instructions, hex.EncodeToString(digest[:]), scope.actorID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() == 0 {
			var number int32
			if err := tx.QueryRow(ctx, queryCommandsChangeinstructionsSelectNextDraftVersion, agentID).Scan(&number); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			ref, _ := newRef("ins")
			if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsInsertDraftVersion, ref, scope.organizationID, agentID, number, payload.Instructions, hex.EncodeToString(digest[:]), scope.actorID); err != nil {
				return commandOutcome{}, mapWriteError(err)
			}
		}
	case command.ValidateInstructions:
		var content string
		var ref string
		if err := tx.QueryRow(ctx, queryCommandsChangeinstructionsSelectCurrentDraft, agentID).Scan(&ref, &content); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		state := "VALID"
		problems := []string{}
		if len(strings.TrimSpace(content)) < 20 {
			state = "INVALID"
			problems = append(problems, "i18n:INSTRUCTIONS_TOO_SHORT")
		}
		if err := repository.validateAgentPromptContextTx(ctx, tx, scope, payload.Ref, content, false); err != nil {
			if !errors.Is(err, errs.ErrInvalid) {
				return commandOutcome{}, err
			}
			state = "INVALID"
			problems = append(problems, "PROMPT_CONTEXT_INVALID")
		}
		if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateInstructionVersionsStateValidationProblems, ref, state, asJSON(problems)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.PublishInstructions:
		impact, items, err := repository.instructionPlanForPublish(ctx, tx, scope, payload, agentVersion)
		if err != nil {
			return commandOutcome{}, err
		}
		var draftRef, draftContent string
		if err := tx.QueryRow(ctx, queryCommandsChangeinstructionsSelectCurrentDraft, agentID).Scan(&draftRef, &draftContent); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err := repository.validateAgentPromptContextTx(ctx, tx, scope, payload.Ref, draftContent, false); err != nil {
			return commandOutcome{}, err
		}
		tag, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateInstructionVersionsStatePublishedAt, agentID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
		for i := range items {
			item := &items[i]
			if item.Outcome != "PENDING" {
				continue
			}
			bindingRef, bindingVersion, err := assignInstructionBinding(ctx, tx, scope.organizationID, agentID, draftRef)
			if err != nil {
				return commandOutcome{}, err
			}
			item.Outcome, item.ResultRevisionRef, item.ResultBindingRef, item.ResultBindingVersion, item.ResultConsumerVersion = "APPLIED", draftRef, bindingRef, bindingVersion, agentVersion+1
		}
		plan, err := finishRevisionImpact(ctx, tx, impact, items, draftRef)
		if err != nil {
			return commandOutcome{}, err
		}
		impactPlan = &plan
	case command.RollbackInstructions:
		var content string
		if err := tx.QueryRow(ctx, queryCommandsChangeinstructionsSelectInstructionVersionsAgentIdRefState, agentID, payload.Instructions).Scan(&content); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err := repository.validateAgentPromptContextTx(ctx, tx, scope, payload.Ref, content, false); err != nil {
			return commandOutcome{}, err
		}
		var number int32
		if err := tx.QueryRow(ctx, queryCommandsChangeinstructionsSelectNextRollbackVersion, agentID).Scan(&number); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		ref, _ := newRef("ins")
		digest := sha256.Sum256([]byte(content))
		if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsInsertRollbackVersion, ref, scope.organizationID, agentID, number, content, hex.EncodeToString(digest[:]), payload.Instructions, scope.actorID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if _, _, err := assignInstructionBinding(ctx, tx, scope.organizationID, agentID, ref); err != nil {
			return commandOutcome{}, err
		}
	}
	if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt, agentID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	agent := entity.Agent{Ref: payload.Ref, ProjectRef: projectRef, Version: agentVersion + 1}
	event := "AGENT_CHANGED"
	if input.Kind == command.PublishInstructions || input.Kind == command.RollbackInstructions {
		event = "INSTRUCTIONS_PUBLISHED"
	}
	return commandOutcome{result: command.Result{Agent: &agent, RevisionImpactPlan: impactPlan}, projectID: projectID, projectRef: projectRef, resourceKind: "INSTRUCTIONS", resourceRef: payload.Ref, summary: "i18n:AGENT_INSTRUCTIONS_UPDATED", platformEvent: event, platformAggregateVersion: agent.Version}, nil
}

// changeAgentBinding resolves the agent and enabled capability or exact integration grant
// authority before OCC, then changes only the specialized capability/grant binding. It increments
// agent version and reads the canonical runtime/avatar/capability result; payload binding refs do
// not confer grant authority.
// changeAgentBinding разрешает agent и enabled capability либо exact integration grant authority
// до OCC, затем меняет лишь специализированный capability/grant binding. Увеличивает agent version
// и читает канонический runtime/avatar/capability result; payload binding refs не выдают grant
// authority.
func (repository *Repository) changeAgentBinding(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentBindingInput)
	if !ok || payload.AgentRef == "" || payload.BindingRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID, projectRef string
	var current int64
	if err := tx.QueryRow(ctx, queryCommandsChangeagentbindingSelectAgentsOrganizationIdRef, scope.organizationID, payload.AgentRef).Scan(&projectID, &projectRef, &current); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	var capabilityKey string
	if input.Kind == command.ChangeAgentCapability {
		if !validCapabilityKey(payload.BindingRef) {
			return commandOutcome{}, errs.ErrInvalid
		}
		if err := tx.QueryRow(ctx, queryCommandsChangeagentbindingSelectEnabledCapability, payload.BindingRef).Scan(&capabilityKey); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	}
	if input.Kind == command.ChangeAgentGrant {
		if err := repository.requireAgentIntegrationGrantAuthority(ctx, tx, scope, payload.AgentRef, payload.BindingRef); err != nil {
			return commandOutcome{}, err
		}
	} else if payload.Enabled {
		if err := repository.requireCapabilityGrantAuthority(ctx, tx, scope, projectRef, payload.AgentRef, payload.BindingRef); err != nil {
			return commandOutcome{}, err
		}
	}
	if current != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if input.Kind == command.ChangeAgentGrant {
		if payload.Enabled {
			tag, err := tx.Exec(ctx, queryCommandsChangeagentbindingEnableIntegrationGrant, scope.organizationID, payload.BindingRef, payload.AgentRef)
			if err != nil || tag.RowsAffected() != 1 {
				return commandOutcome{}, errs.ErrNotFound
			}
		} else {
			if _, err := tx.Exec(ctx, queryCommandsChangeagentbindingRevokeIntegrationGrant, scope.organizationID, payload.BindingRef, payload.AgentRef); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
	} else if input.Kind == command.ChangeAgentCapability {
		query := queryCommandsChangeagentbindingRemoveCapability
		if payload.Enabled {
			query = queryCommandsChangeagentbindingAppendCapability
		}
		tag, err := tx.Exec(ctx, query, scope.organizationID, payload.AgentRef, capabilityKey)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	} else {
		return commandOutcome{}, errs.ErrInvalid
	}
	if _, err := tx.Exec(ctx, queryCommandsChangeagentbindingUpdateAgentsVersionUpdatedAt, scope.organizationID, payload.AgentRef); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var agent entity.Agent
	if err := tx.QueryRow(ctx, queryCommandsChangeagentbindingSelectAgentReadback, scope.organizationID, payload.AgentRef).Scan(
		&agent.Ref, &agent.ProjectRef, &agent.RoleDefinitionRef, &agent.RoleDefinitionName,
		&agent.Name, &agent.Purpose, &agent.RoleDescription, &agent.AvatarURL,
		&agent.Avatar.ArtifactRef, &agent.Avatar.ArtifactRevision,
		&agent.State, &agent.Enabled, &agent.Version, &agent.RuntimeKey,
		&agent.RuntimeName, &agent.Provider, &agent.Model, &agent.RuntimeRevision,
		&agent.Capabilities, &agent.KnowledgeArtifactRefs, &agent.CreatedAt, &agent.UpdatedAt,
	); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	setAgentAvatarReadback(&agent)
	agent.NextActions = agentActions(agent, true, true)
	return commandOutcome{result: command.Result{Agent: &agent}, projectID: projectID, projectRef: projectRef, resourceKind: "AGENT", resourceRef: payload.AgentRef, summary: "i18n:AGENT_PERMISSIONS_UPDATED", platformEvent: "AGENT_CHANGED"}, nil
}

// changeWorkflow creates or updates server-identified drafts and handles
// validation/publication/archive under workflow OCC. Validation checks coordinator/step
// capabilities; publication requires VALID and freezes exact raw spec/digest in a new version;
// archive suspends target schedules. It rereads canonical workflow metadata and leaves
// commit/event emission to the owner.
// changeWorkflow создаёт либо обновляет drafts с серверной identity и обрабатывает
// validation/publication/archive под workflow OCC. Validation проверяет coordinator/step
// capabilities; publication требует VALID и закрепляет точные raw spec/digest в новой version;
// archive приостанавливает target schedules. Перечитывает канонические workflow metadata и
// оставляет commit/event emission владельцу.
func (repository *Repository) changeWorkflow(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.WorkflowInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateWorkflow {
		projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		ref, _ := newRef("wfl")
		draft := entity.WorkflowVersion{Ref: "draft", Name: payload.Name, Purpose: payload.Purpose, CoordinatorAgentRef: payload.CoordinatorAgentRef, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, ResultSchema: map[string]any{}}
		if payload.Draft != nil {
			draft = *payload.Draft
			if !validWorkflowVersion(draft) {
				return commandOutcome{}, errs.ErrInvalid
			}
		}
		normalizeWorkflowDraftIdentity(ref, &draft)
		var item entity.Workflow
		raw := asJSON(draft)
		err := tx.QueryRow(ctx, queryCommandsChangeworkflowInsertWorkflowsRefProjectIdPurpose, ref, scope.organizationID, projectID, payload.Name, payload.Purpose, payload.CoordinatorAgentRef, raw, scope.actorID).Scan(&item.Ref, &item.Name, &item.Purpose, &item.State, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		item.ProjectRef = payload.ProjectRef
		item.CoordinatorAgentRef = payload.CoordinatorAgentRef
		item.Draft = &draft
		item.NextActions = workflowActions(item, true, true)
		return commandOutcome{result: command.Result{Workflow: &item}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "WORKFLOW", resourceRef: ref, summary: "i18n:WORKFLOW_CREATED", platformEvent: "WORKFLOW_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var workflowID, projectID, projectRef, state string
	var version int64
	if err := tx.QueryRow(ctx, queryCommandsChangeworkflowSelectWorkflowsOrganizationIdRef, scope.organizationID, payload.Ref).Scan(&workflowID, &projectID, &projectRef, &state, &version); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	switch input.Kind {
	case command.UpdateWorkflow:
		if payload.Draft == nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		normalizeWorkflowDraftIdentity(payload.Ref, payload.Draft)
		if !validWorkflowVersion(*payload.Draft) {
			return commandOutcome{}, errs.ErrInvalid
		}
		tag, err := tx.Exec(ctx, queryCommandsChangeworkflowUpdateWorkflowsDraftSpecStateVersion, workflowID, payload.Draft.Name, payload.Draft.Purpose, payload.Draft.CoordinatorAgentRef, asJSON(payload.Draft))
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrInvalid
		}
	case command.ValidateWorkflow:
		var raw []byte
		if err := tx.QueryRow(ctx, queryCommandsChangeworkflowSelectDraftForValidation, workflowID).Scan(&raw); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var draft entity.WorkflowVersion
		if json.Unmarshal(raw, &draft) != nil || !validWorkflowVersion(draft) {
			return commandOutcome{}, errs.ErrInvalid
		}
		var coordinatorEligible bool
		coordinatorCapabilities := []string{}
		if workflowRequiresDelegation(draft) {
			coordinatorCapabilities = []string{"platform.run.delegate"}
		}
		if err := tx.QueryRow(ctx, queryCommandsChangeworkflowValidateAgentCapabilities, scope.organizationID, projectID, draft.CoordinatorAgentRef, coordinatorCapabilities).Scan(&coordinatorEligible); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !coordinatorEligible {
			return commandOutcome{}, errs.ErrInvalid
		}
		for _, step := range draft.Steps {
			var eligible bool
			if err := tx.QueryRow(ctx, queryCommandsChangeworkflowValidateAgentCapabilities, scope.organizationID, projectID, step.AgentRef, step.RequiredCapabilityKeys).Scan(&eligible); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			if !eligible {
				return commandOutcome{}, errs.ErrInvalid
			}
		}
		_, err := tx.Exec(ctx, queryCommandsChangeworkflowMarkWorkflowValid, workflowID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.PublishWorkflow:
		if state != "VALID" {
			return commandOutcome{}, errs.ErrConflict
		}
		var raw []byte
		var next int32
		if err := tx.QueryRow(ctx, queryCommandsChangeworkflowSelectDraftForPublish, workflowID).Scan(&raw, &next); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var draft entity.WorkflowVersion
		if json.Unmarshal(raw, &draft) != nil || !validWorkflowVersion(draft) {
			return commandOutcome{}, errs.ErrConflict
		}
		digest := sha256.Sum256(raw)
		versionRef, _ := newRef("wfv")
		if _, err := tx.Exec(ctx, queryCommandsChangeworkflowInsertWorkflowVersionsRefWorkflowIdSpec, versionRef, scope.organizationID, workflowID, next, raw, hex.EncodeToString(digest[:]), scope.actorID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		if _, err := tx.Exec(ctx, queryCommandsChangeworkflowUpdateWorkflowsPublishedSpecPublishedVersionState, workflowID, raw, next); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.ArchiveWorkflow:
		tag, err := tx.Exec(ctx, queryCommandsChangeworkflowArchiveWorkflow, workflowID)
		if err != nil || tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.suspendTargetSchedules(ctx, tx, scope, projectID, projectRef, "WORKFLOW", payload.Ref); err != nil {
			return commandOutcome{}, err
		}
	}
	workflow, readErr := scanWorkflow(tx.QueryRow(ctx, queryCommandsChangeworkflowSelectAuthoritativeReadback, scope.organizationID, payload.Ref), false)
	if readErr != nil {
		return commandOutcome{}, readErr
	}
	return commandOutcome{result: command.Result{Workflow: &workflow}, projectID: projectID, projectRef: projectRef, resourceKind: "WORKFLOW", resourceRef: payload.Ref, summary: "i18n:WORKFLOW_UPDATED", platformEvent: "WORKFLOW_CHANGED"}, nil
}

// validWorkflowVersion accepts only versions whose closed structural/executable invariant checker
// returns no error code; it does not query agent eligibility.
// validWorkflowVersion принимает лишь versions без кода ошибки закрытой structural/executable
// invariant проверки; agent eligibility не запрашивает.
func validWorkflowVersion(version entity.WorkflowVersion) bool {
	return workflowVersionInvalidField(version) == ""
}

const (
	// workflowInvalidStepsInstructions names the fixed safe validation category for empty or
	// oversized/invalid step instructions without exposing their content or index.
	// workflowInvalidStepsInstructions задаёт фиксированную безопасную validation category пустых либо
	// чрезмерных/невалидных step instructions без раскрытия содержимого или индекса.
	workflowInvalidStepsInstructions = "STEPS_INSTRUCTIONS"
	// workflowInvalidStepsExpectedResult names the fixed safe validation category for invalid or
	// oversized step expected-result text.
	// workflowInvalidStepsExpectedResult задаёт фиксированную безопасную validation category
	// невалидного либо чрезмерного step expected-result text.
	workflowInvalidStepsExpectedResult = "STEPS_EXPECTED_RESULT"
	// workflowInvalidInvariants names the generic closed workflow invariant failure category for
	// structure, capability and profile checks.
	// workflowInvalidInvariants задаёт общую закрытую workflow invariant failure category для
	// structure, capability и profile checks.
	workflowInvalidInvariants = "WORKFLOW_INVARIANTS"
)

// workflowVersionInvalidField returns the first closed structural error, then rejects delegation
// capability on steps when no distinct-agent delegation is required. It never returns payload text
// or a step index as diagnostics.
// workflowVersionInvalidField возвращает первую закрытую structural error, затем отклоняет
// delegation capability у steps, когда distinct-agent delegation не требуется. Никогда не
// возвращает payload text либо step index в диагностике.
func workflowVersionInvalidField(version entity.WorkflowVersion) string {
	if field := workflowVersionStructureInvalidField(version); field != "" {
		return field
	}
	if !workflowRequiresDelegation(version) {
		for _, step := range version.Steps {
			if contains(step.RequiredCapabilityKeys, "platform.run.delegate") {
				return workflowInvalidInvariants
			}
		}
	}
	return ""
}

// workflowVersionStructureInvalidField checks Unicode text/rune bounds,
// coordinator/concurrency/timeouts/input shape, one-to-two-hundred ordered unique steps,
// backward-only dependencies, gate decisions and capability grammar. It enforces the aggregate
// byte budget but performs no database eligibility check, allowing structurally repairable catalog
// profiles to remain distinguishable from executable acceptance.
// workflowVersionStructureInvalidField проверяет Unicode text/rune bounds,
// coordinator/concurrency/timeouts/input shape, от одного до двухсот ordered unique steps,
// зависимости лишь на прежние steps, gate decisions и capability grammar. Ограничивает общий byte
// budget, но не выполняет database eligibility check, отличая структурное исправление catalog
// profiles от допуска исполнения.
func workflowVersionStructureInvalidField(version entity.WorkflowVersion) string {
	// maxLength считается в Unicode-символах, общий бюджет ниже остаётся байтовым.
	if !validWorkflowText(version.Name, 160) || !validWorkflowText(version.Purpose, 2000) || !validWorkflowText(version.CompletionCriteria, 2000) || !utf8.ValidString(version.Instructions) {
		return workflowInvalidInvariants
	}
	if strings.TrimSpace(version.Name) == "" || strings.TrimSpace(version.CoordinatorAgentRef) == "" || version.Concurrency < 1 || version.Concurrency > 100 || version.TimeoutSeconds < 1 || version.TimeoutSeconds > 7*24*60*60 || len(version.Inputs) > 100 || len(version.Steps) < 1 || len(version.Steps) > 200 || !validWorkflowInputFields(version.Inputs) {
		return workflowInvalidInvariants
	}
	knownSteps := make(map[string]struct{}, len(version.Steps))
	totalInstructions := len(version.Instructions) + len(version.CompletionCriteria)
	for index, step := range version.Steps {
		if strings.TrimSpace(step.Instructions) == "" || !validWorkflowText(step.Instructions, 1000) {
			return workflowInvalidStepsInstructions
		}
		if !validWorkflowText(step.ExpectedResult, 1000) {
			return workflowInvalidStepsExpectedResult
		}
		if step.Key == "" || len(step.Key) > 96 || !utf8.ValidString(step.Key) || step.Position != int32(index+1) || strings.TrimSpace(step.Name) == "" || !validWorkflowText(step.Name, 160) || strings.TrimSpace(step.AgentRef) == "" || step.TimeoutSeconds < 1 || step.TimeoutSeconds > 24*60*60 || step.ParallelGroup < 0 || step.ParallelGroup > 50 || len(step.GateDecisions) > 4 || len(step.RequiredCapabilityKeys) > 50 {
			return workflowInvalidInvariants
		}
		if _, duplicate := knownSteps[step.Key]; duplicate {
			return workflowInvalidInvariants
		}
		for _, dependency := range step.DependsOn {
			if _, exists := knownSteps[dependency]; !exists {
				return workflowInvalidInvariants
			}
		}
		for _, decision := range step.GateDecisions {
			if !contains([]string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}, decision) {
				return workflowInvalidInvariants
			}
		}
		if step.HumanGateAfter && len(step.GateDecisions) == 0 {
			return workflowInvalidInvariants
		}
		for _, capability := range step.RequiredCapabilityKeys {
			if !validCapabilityKey(capability) {
				return workflowInvalidInvariants
			}
		}
		knownSteps[step.Key] = struct{}{}
		totalInstructions += len(step.Name) + len(step.Instructions) + len(step.ExpectedResult)
	}
	if totalInstructions > 64<<10 {
		return workflowInvalidInvariants
	}
	return ""
}

// validWorkflowText requires valid UTF-8 and at most the supplied Unicode rune count; empty text
// is allowed unless the caller separately requires content.
// validWorkflowText требует валидный UTF-8 и не более переданного Unicode rune count; пустой текст
// разрешён, если caller отдельно не требует содержимое.
func validWorkflowText(value string, maximumCharacters int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= maximumCharacters
}

// normalizeWorkflowDraftIdentity preserves a plausible bounded wfv_ reference or derives a
// deterministic workflow-owned draft ref and ensures a positive version number. Nil is ignored and
// published immutable revisions are not rewritten; prefix/length acceptance is not full reference
// validation.
// normalizeWorkflowDraftIdentity сохраняет подходящую по prefix/length wfv_ reference либо выводит
// детерминированный workflow-owned draft ref и обеспечивает положительный version number. Nil
// игнорируется, published immutable revisions не переписываются; prefix/length acceptance не
// является полной reference validation.
func normalizeWorkflowDraftIdentity(workflowRef string, version *entity.WorkflowVersion) {
	if version == nil {
		return
	}
	if !strings.HasPrefix(version.Ref, "wfv_") || len(version.Ref) < 12 || len(version.Ref) > 96 {
		digest := sha256.Sum256([]byte("workflow-draft\x00" + workflowRef))
		version.Ref = "wfv_" + hex.EncodeToString(digest[:12])
	}
	if version.VersionNumber < 1 {
		version.VersionNumber = 1
	}
}

// validWorkflowInputFields validates unique field keys, bounded labels/help/options and the closed
// TEXT/LONG_TEXT/NUMBER/BOOLEAN/DATE/SELECT schema. Options are unique and present only for
// SELECT; user-supplied runtime values are checked separately.
// validWorkflowInputFields проверяет уникальные field keys, ограниченные labels/help/options и
// закрытую TEXT/LONG_TEXT/NUMBER/BOOLEAN/DATE/SELECT schema. Options уникальны и присутствуют лишь
// у SELECT; пользовательские runtime values проверяются отдельно.
func validWorkflowInputFields(fields []entity.WorkflowInputField) bool {
	known := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !validWorkflowInputKey(field.Key) || strings.TrimSpace(field.Label) == "" || !validWorkflowText(field.Label, 160) || !validWorkflowText(field.Help, 500) || !contains([]string{"TEXT", "LONG_TEXT", "NUMBER", "BOOLEAN", "DATE", "SELECT"}, field.Type) || len(field.Options) > 50 {
			return false
		}
		if _, duplicate := known[field.Key]; duplicate {
			return false
		}
		known[field.Key] = struct{}{}
		options := make(map[string]struct{}, len(field.Options))
		for _, option := range field.Options {
			if strings.TrimSpace(option) == "" || !validWorkflowText(option, 160) {
				return false
			}
			if _, duplicate := options[option]; duplicate {
				return false
			}
			options[option] = struct{}{}
		}
		if field.Type == "SELECT" && len(field.Options) == 0 || field.Type != "SELECT" && len(field.Options) != 0 {
			return false
		}
	}
	return true
}

// validWorkflowInputKey accepts a one-to-eighty-byte lowercase ASCII key starting with a letter
// and continuing with letters/digits/underscore/hyphen, without normalization.
// validWorkflowInputKey принимает lowercase ASCII key длиной от одного до восьмидесяти bytes,
// начинающийся с буквы и продолжающийся буквами/digits/underscore/hyphen, без normalization.
func validWorkflowInputKey(key string) bool {
	if len(key) < 1 || len(key) > 80 || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for _, character := range key {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

// validWorkflowRunInput requires known input keys and every required nonempty value, then applies
// the declared field-specific value rules. It does not fill defaults or revalidate the field
// schema itself.
// validWorkflowRunInput требует известные input keys и все обязательные непустые values, затем
// применяет объявленные field-specific value rules. Не назначает defaults и не перепроверяет саму
// field schema.
func validWorkflowRunInput(fields []entity.WorkflowInputField, input map[string]any) bool {
	if len(input) > len(fields) {
		return false
	}
	known := make(map[string]entity.WorkflowInputField, len(fields))
	for _, field := range fields {
		known[field.Key] = field
		value, exists := input[field.Key]
		if field.Required && (!exists || emptyWorkflowInputValue(value)) {
			return false
		}
	}
	for key, value := range input {
		field, exists := known[key]
		if !exists || !validWorkflowRunInputValue(field, value) {
			return false
		}
	}
	return true
}

// emptyWorkflowInputValue treats only nil or whitespace-only string values as empty; false, zero
// and other dynamic types are not empty by this predicate.
// emptyWorkflowInputValue считает пустыми лишь nil либо whitespace-only string values; false, zero
// и другие dynamic types этим predicate не считаются пустыми.
func emptyWorkflowInputValue(value any) bool {
	text, isText := value.(string)
	return value == nil || isText && strings.TrimSpace(text) == ""
}

// validWorkflowRunInputValue checks per-type string byte limits, finite float64 numbers, bools,
// exact parseable YYYY-MM-DD dates or SELECT membership. It neither coerces dynamic values nor
// applies requiredness itself.
// validWorkflowRunInputValue проверяет per-type string byte limits, конечные float64 numbers,
// bools, точные разбираемые YYYY-MM-DD dates либо SELECT membership. Не преобразует dynamic values
// и сам не применяет requiredness.
func validWorkflowRunInputValue(field entity.WorkflowInputField, value any) bool {
	switch field.Type {
	case "TEXT":
		text, ok := value.(string)
		return ok && len(text) <= 4000
	case "LONG_TEXT":
		text, ok := value.(string)
		return ok && len(text) <= 32768
	case "NUMBER":
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0)
	case "BOOLEAN":
		_, ok := value.(bool)
		return ok
	case "DATE":
		date, ok := value.(string)
		if !ok || len(date) != len("2006-01-02") {
			return false
		}
		_, err := time.Parse("2006-01-02", date)
		return err == nil
	case "SELECT":
		selected, ok := value.(string)
		return ok && contains(field.Options, selected)
	default:
		return false
	}
}

// validBoundedRunInput requires at most one hundred keys and successful JSON encoding within64
// KiB, permitting nil maps while rejecting marshal errors; it does not authorize or interpret the
// values.
// validBoundedRunInput требует не более ста ключей и успешный JSON encoding в пределах64 KiB,
// допуская nil maps и отклоняя marshal errors; значения не авторизует и не интерпретирует.
func validBoundedRunInput(input map[string]any) bool {
	if len(input) > 100 {
		return false
	}
	raw, err := json.Marshal(input)
	return err == nil && len(raw) <= 64<<10
}

// workflowRequiresDelegation reports whether any step agent differs from the coordinator; it does
// not infer permissions from that relationship.
// workflowRequiresDelegation показывает, отличается ли agent хотя бы одного step от coordinator;
// permissions из этой связи не выводит.
func workflowRequiresDelegation(version entity.WorkflowVersion) bool {
	for _, step := range version.Steps {
		if step.AgentRef != version.CoordinatorAgentRef {
			return true
		}
	}
	return false
}

// validCapabilityKey accepts a nonempty bounded already-trimmed lowercase ASCII capability key
// with digits/dot/underscore/hyphen; grammar alone does not register or grant the capability.
// validCapabilityKey принимает непустой ограниченный уже обрезанный lowercase ASCII capability key
// с digits/dot/underscore/hyphen; grammar сама не регистрирует и не выдаёт capability.
func validCapabilityKey(value string) bool {
	if value == "" || len(value) > 80 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

// agentsHaveCapabilities checks each distinct nonempty project agent through the canonical
// capability query, stopping on denial/error and requiring at least one checked agent. Driver
// errors are returned unchanged for caller classification.
// agentsHaveCapabilities проверяет каждого distinct непустого project agent каноническим
// capability query, останавливается при отказе/ошибке и требует хотя бы одного проверенного agent.
// Driver errors передаются без изменения для классификации caller.
func (repository *Repository) agentsHaveCapabilities(ctx context.Context, tx pgx.Tx, organizationID, projectID string, agentRefs, capabilities []string) (bool, error) {
	seen := make(map[string]struct{}, len(agentRefs))
	for _, agentRef := range agentRefs {
		if agentRef == "" {
			continue
		}
		if _, exists := seen[agentRef]; exists {
			continue
		}
		seen[agentRef] = struct{}{}
		var allowed bool
		if err := tx.QueryRow(ctx, queryCommandsChangeworkflowValidateAgentCapabilities, organizationID, projectID, agentRef, capabilities).Scan(&allowed); err != nil {
			return false, err
		}
		if !allowed {
			return false, nil
		}
	}
	return len(seen) > 0, nil
}

// launchRun delegates normal launch with finalized attachment snapshot reuse disabled;
// target/runtime/catalog/lineage checks remain in the specialized shared implementation.
// launchRun передаёт обычный launch с отключённым finalized attachment snapshot reuse;
// target/runtime/catalog/lineage checks остаются в общей специализированной реализации.
func (repository *Repository) launchRun(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	return repository.launchRunWithAttachmentPolicy(ctx, tx, scope, input, false)
}

// launchRunWithAttachmentPolicy validates project/task/source and exact AGENT or published
// WORKFLOW target, model/runtime contracts and attachment eligibility before binding a new or
// matching existing session. It inserts root run/turn/process and agent/coordinator/planned
// workflow lineage, emits RUN_CREATED and returns graph readback in the caller transaction. Reuse
// changes only attachment snapshot policy; no provider execution or runtime claim occurs here.
// launchRunWithAttachmentPolicy проверяет project/task/source и точные AGENT либо published
// WORKFLOW target, model/runtime contracts и attachment eligibility до связи с новой либо
// совпавшей existing session. Вставляет root run/turn/process и agent/coordinator/planned workflow
// lineage, создаёт RUN_CREATED и возвращает graph readback в транзакции caller. Reuse меняет лишь
// attachment snapshot policy; provider execution либо runtime claim здесь нет.
func (repository *Repository) launchRunWithAttachmentPolicy(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, reuseAttachmentSnapshot bool) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LaunchRunInput)
	if !ok || payload.ProjectRef == "" || !runtimecontract.ValidAssistantTurnContent(payload.Task) || len([]rune(payload.Title)) > 240 || payload.Target.Ref == "" || !validBoundedRunInput(payload.Input) {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
	}
	source := payload.Source
	if source == "" {
		source = "CONTROL_CENTER"
	}
	if !contains([]string{"CONTROL_CENTER", "SYSTEM_ASSISTANT", "SCHEDULE", "INTEGRATION", "AGENT_DELEGATION", "MATTERMOST"}, source) {
		return commandOutcome{}, errs.ErrInvalid
	}
	var targetName string
	var workflowSpec []byte
	var workflowVersionID, workflowVersionRef, workflowVersionDigest string
	var coordinatorRef, coordinatorName string
	var workflowVersion entity.WorkflowVersion
	var targetAgentRefs []string
	runConcurrency := int32(defaultAgentRunConcurrency)
	switch payload.Target.Type {
	case "AGENT":
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef, scope.organizationID, projectID, payload.Target.Ref).Scan(&targetName); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		targetAgentRefs = []string{payload.Target.Ref}
	case "WORKFLOW":
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"project_id":      projectID,
			"workflow_ref":    payload.Target.Ref,
		}).Scan(&targetName, &workflowVersionID, &workflowVersionRef, &workflowSpec, &workflowVersionDigest, &coordinatorRef, &coordinatorName); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		if json.Unmarshal(workflowSpec, &workflowVersion) != nil || !validWorkflowVersion(workflowVersion) || workflowVersion.CoordinatorAgentRef != coordinatorRef {
			return commandOutcome{}, errs.ErrConflict
		}
		if !validWorkflowRunInput(workflowVersion.Inputs, payload.Input) {
			return commandOutcome{}, errs.ErrInvalid
		}
		targetAgentRefs = append(targetAgentRefs, coordinatorRef)
		for _, step := range workflowVersion.Steps {
			targetAgentRefs = append(targetAgentRefs, step.AgentRef)
		}
		runConcurrency = workflowVersion.Concurrency
	default:
		return commandOutcome{}, errs.ErrInvalid
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
		return commandOutcome{}, fmt.Errorf("validate run runtime contract: %w", errs.ErrUnavailable)
	}
	if !runtimeContractReady {
		return commandOutcome{}, errs.ErrConflict
	}
	attachmentKind := "RUN_INPUT"
	if payload.Target.Type == "WORKFLOW" {
		attachmentKind = "WORKFLOW_INPUT"
	}
	attachmentPurpose := payload.AttachmentPurpose
	if attachmentPurpose == "" {
		attachmentPurpose = attachmentKind
	}
	if !contains(attachmentSetPurposes, attachmentPurpose) {
		return commandOutcome{}, errs.ErrInvalid
	}
	attachmentSet, err := repository.resolveFinalizedAttachmentSet(ctx, tx, scope, projectID, payload.AttachmentSetRef, attachmentPurpose, reuseAttachmentSnapshot)
	if err != nil {
		return commandOutcome{}, err
	}
	if attachmentSet.ID != "" {
		allowed, err := repository.attachmentAgentsAllowed(ctx, tx, scope.organizationID, projectID, targetAgentRefs)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if !allowed {
			return commandOutcome{}, errs.ErrCapabilityRequired
		}
	}
	sessionRef := payload.SessionRef
	var sessionID string
	if sessionRef == "" {
		sessionRef, _ = newRef("ses")
		providerAccountID, err := repository.selectProviderAccountForAgent(ctx, tx, scope.organizationID, targetAgentRefs[0])
		if err != nil {
			return commandOutcome{}, err
		}
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunInsertSessionsRefProjectIdTargetRef, sessionRef, scope.organizationID, projectID, payload.Target.Type, payload.Target.Ref, providerAccountID, scope.actorID).Scan(&sessionID); err != nil {
			return commandOutcome{}, fmt.Errorf("insert run session: %w", errs.ErrUnavailable)
		}
		if err := bindSessionModelCatalog(ctx, tx, scope.organizationID, sessionID, targetAgentRefs[0]); err != nil {
			return commandOutcome{}, err
		}
	} else if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectSessionsOrganizationIdProjectIdRef, scope.organizationID, projectID, sessionRef, payload.Target.Type, payload.Target.Ref).Scan(&sessionID); err != nil {
		return commandOutcome{}, fmt.Errorf("resolve continuation session: %w", errs.ErrConflict)
	}
	if err := validateSessionRuntimeCatalog(ctx, tx, scope.organizationID, sessionID, targetAgentRefs[0]); err != nil {
		return commandOutcome{}, err
	}
	runRef, _ := newRef("run")
	title := strings.TrimSpace(payload.Title)
	titleSource := strings.TrimSpace(payload.TitleSource)
	if title == "" {
		title = boundedRunTitle(targetName + ": " + truncate(payload.Task, 120))
		titleSource = "SERVER_DEFAULT"
	} else if titleSource == "" {
		titleSource = "SERVER_DEFAULT"
	}
	if !contains([]string{"SERVER_DEFAULT", "AGENT_PROPOSED", "USER_EDITED"}, titleSource) {
		return commandOutcome{}, errs.ErrInvalid
	}
	rawInput := asJSON(payload.Input)
	var runID string
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunInsertRunsRefProjectIdTargetType, pgx.StrictNamedArgs{
		"run_ref":             runRef,
		"organization_id":     scope.organizationID,
		"project_id":          projectID,
		"session_id":          sessionID,
		"workflow_version_id": workflowVersionID,
		"target_type":         payload.Target.Type,
		"target_ref":          payload.Target.Ref,
		"source":              source,
		"title":               title,
		"title_source":        titleSource,
		"task":                payload.Task,
		"input":               rawInput,
		"initiated_by":        scope.actorID,
		"concurrency_limit":   runConcurrency,
	}).Scan(&runID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert launched run: %w", mapWriteError(err))
	}
	if err := repository.attachSetToRun(ctx, tx, scope, projectID, attachmentSet, runID, attachmentKind); err != nil {
		return commandOutcome{}, fmt.Errorf("bind run attachment set: %w", err)
	}
	if _, err := tx.Exec(ctx, queryCommandsLaunchrunUpdateRunsRootRunId, runID); err != nil {
		return commandOutcome{}, fmt.Errorf("bind root run lineage: %w", errs.ErrUnavailable)
	}
	executionTask := payload.Task
	if payload.Target.Type == "WORKFLOW" {
		executionTask = workflowCoordinatorTask(payload.Task, workflowVersionRef, workflowVersionDigest, workflowVersion)
	}
	turnRef, _ := newRef("trn")
	var turnID string
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunInsertSessionTurnsRefSessionIdTurnNumber, turnRef, scope.organizationID, sessionID, runID, scope.actorRef, executionTask).Scan(&turnID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert initial session turn: %w", errs.ErrUnavailable)
	}
	if err := repository.attachSetToTurn(ctx, tx, scope, projectID, attachmentSet, turnID, "SESSION_TURN"); err != nil {
		return commandOutcome{}, fmt.Errorf("bind turn attachment set: %w", err)
	}
	if _, err := tx.Exec(ctx, queryCommandsLaunchrunUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return commandOutcome{}, fmt.Errorf("advance run session: %w", errs.ErrUnavailable)
	}
	rootNodeRef, _ := newRef("nod")
	var rootNodeID string
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunInsertRunNodesRefRootRunIdType, rootNodeRef, scope.organizationID, runID, title, turnID, truncate(payload.Task, 500)).Scan(&rootNodeID); err != nil {
		return commandOutcome{}, fmt.Errorf("insert root process node: %w", errs.ErrUnavailable)
	}
	if payload.Target.Type == "AGENT" {
		if _, _, err := repository.insertAgentNode(ctx, tx, scope, runID, runID, rootNodeID, payload.Target.Ref, targetName, turnID, payload.Task); err != nil {
			return commandOutcome{}, fmt.Errorf("insert direct agent execution node: %w", err)
		}
	} else {
		nodeID, _, err := repository.insertAgentNode(ctx, tx, scope, runID, runID, rootNodeID, coordinatorRef, coordinatorName, turnID, executionTask)
		if err != nil {
			return commandOutcome{}, fmt.Errorf("insert workflow coordinator execution node: %w", err)
		}
		initialGate := !workflowRequiresDelegation(workflowVersion) && workflowHasHumanGate(workflowVersion)
		if _, err := tx.Exec(ctx, queryCommandsLaunchrunUpdateRunNodesWorkflowStepKeyHumanGateAfter, nodeID, "workflow.coordinator.initial", initialGate); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := repository.insertPlannedWorkflowNodes(ctx, tx, scope, projectID, runID, nodeID, workflowVersion); err != nil {
			return commandOutcome{}, err
		}
	}
	if _, err := tx.Exec(ctx, queryCommandsLaunchrunUpdateRunsStateStartedAtVersion, runID); err != nil {
		return commandOutcome{}, fmt.Errorf("start launched run: %w", errs.ErrUnavailable)
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, runID, runRef, "RUN_CREATED", rootNodeRef, "", "", "", "i18n:RUN_CREATED", "RUNNING", "RUNNING"); err != nil {
		return commandOutcome{}, fmt.Errorf("emit launched run event: %w", err)
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, runRef)
	if err != nil {
		return commandOutcome{}, fmt.Errorf("read launched run graph: %w", err)
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph}, projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "RUN", resourceRef: runRef, summary: "i18n:RUN_CREATED"}, nil
}

// insertPlannedWorkflowNodes creates all immutable-version step placeholders, then ordered
// coordinator/dependency edges with server IDs. Missing dependency lookup is Conflict; it plans
// the graph without claiming work or treating a placeholder as executed.
// insertPlannedWorkflowNodes создаёт все step placeholders immutable-version, затем ordered
// coordinator/dependency edges с серверными IDs. Отсутствие dependency lookup даёт Conflict;
// планирует graph без claim work и без признания placeholder исполненным.
func (repository *Repository) insertPlannedWorkflowNodes(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, coordinatorNodeID string, workflow entity.WorkflowVersion) error {
	nodeIDs := make(map[string]string, len(workflow.Steps))
	for _, step := range workflow.Steps {
		nodeRef, err := newRef("nod")
		if err != nil {
			return err
		}
		var nodeID string
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunInsertPlannedWorkflowNode, pgx.StrictNamedArgs{
			"node_ref": nodeRef, "organization_id": scope.organizationID, "project_id": projectID,
			"root_run_id": rootRunID, "parent_node_id": coordinatorNodeID, "workflow_step_key": step.Key,
			"agent_ref": step.AgentRef, "human_gate_after": step.HumanGateAfter,
			"input_summary": truncate(step.Instructions, 1000),
		}).Scan(&nodeID); err != nil {
			return fmt.Errorf("insert planned workflow node: %w", errs.ErrUnavailable)
		}
		nodeIDs[step.Key] = nodeID
	}
	for _, step := range workflow.Steps {
		sources := step.DependsOn
		if len(sources) == 0 {
			sources = []string{""}
		}
		for _, dependency := range sources {
			sourceNodeID := coordinatorNodeID
			edgeType := "DELEGATED_TO"
			if dependency != "" {
				sourceNodeID = nodeIDs[dependency]
				edgeType = "WAITING_FOR"
				if sourceNodeID == "" {
					return errs.ErrConflict
				}
			}
			edgeRef, err := newRef("edg")
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, queryCommandsLaunchrunInsertPlannedWorkflowEdge, pgx.StrictNamedArgs{
				"edge_ref": edgeRef, "organization_id": scope.organizationID, "root_run_id": rootRunID,
				"source_node_id": sourceNodeID, "target_node_id": nodeIDs[step.Key], "edge_type": edgeType, "label": step.Key,
			}); err != nil {
				return fmt.Errorf("insert planned workflow edge: %w", errs.ErrUnavailable)
			}
		}
	}
	return nil
}

// workflowCoordinatorTask builds bounded coordinator guidance from the exact workflow version
// ref/digest, instructions, every step assignment/result and completion criteria. It requires
// separate keyed delegation/callback handling in text, but the prose itself grants no authority
// and is rune-truncated at the existing bound.
// workflowCoordinatorTask строит ограниченное coordinator guidance из точных workflow version
// ref/digest, instructions, каждого step assignment/result и completion criteria. Текст требует
// отдельное keyed delegation/callback handling, но сама проза не выдаёт authority и обрезается по
// прежней rune-границе.
func workflowCoordinatorTask(task, versionRef, versionDigest string, version entity.WorkflowVersion) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(task))
	builder.WriteString("\n\n# Workflow coordination contract\n")
	builder.WriteString("Execute the immutable workflow version ")
	builder.WriteString(versionRef)
	builder.WriteString(" (sha256:")
	builder.WriteString(versionDigest)
	builder.WriteString("). Use delegate_agent exactly once for every step, including steps assigned to the coordinator itself, with its exact workflow_step_key. Do not claim a step is complete until its delegated execution returns a successful callback. End this turn after all eligible delegations are accepted; child results arrive in a later callback turn.\n")
	builder.WriteString("\n")
	builder.WriteString(workflowExecutionGuidance)
	builder.WriteString("\n")
	if instructions := strings.TrimSpace(version.Instructions); instructions != "" {
		builder.WriteString("\nWorkflow instructions: ")
		builder.WriteString(instructions)
		builder.WriteString("\n")
	}
	for _, step := range version.Steps {
		builder.WriteString("\n- step ")
		builder.WriteString(step.Key)
		builder.WriteString("; agent ")
		builder.WriteString(step.AgentRef)
		builder.WriteString(" (delegate as a separate execution)")
		builder.WriteString("; task: ")
		builder.WriteString(step.Instructions)
		if expected := strings.TrimSpace(step.ExpectedResult); expected != "" {
			builder.WriteString("; expected result: ")
			builder.WriteString(expected)
		}
	}
	if criteria := strings.TrimSpace(version.CompletionCriteria); criteria != "" {
		builder.WriteString("\n\nCompletion criteria: ")
		builder.WriteString(criteria)
	}
	return truncate(builder.String(), 96<<10)
}

// workflowHasHumanGate reports whether any published step requests a human gate, without opening a
// gate or selecting its decisions.
// workflowHasHumanGate показывает, требует ли хотя бы один published step human gate, без открытия
// gate либо выбора decisions.
func workflowHasHumanGate(version entity.WorkflowVersion) bool {
	for _, step := range version.Steps {
		if step.HumanGateAfter {
			return true
		}
	}
	return false
}

// insertAgentNode resolves an eligible tenant agent, creates its execution node and parent edge,
// and returns node ID/ref inside the caller transaction. Target read errors retain Invalid
// mapping, while insert failures map to Unavailable.
// insertAgentNode разрешает допустимого tenant agent, создаёт execution node и parent edge,
// возвращая node ID/ref в транзакции caller. Target read errors сохраняют Invalid mapping, insert
// failures отображаются в Unavailable.
func (repository *Repository) insertAgentNode(ctx context.Context, tx pgx.Tx, scope scope, rootRunID, runID, parentNodeID, agentRef, displayName, turnID, summary string) (string, string, error) {
	var agentID, role string
	if err := tx.QueryRow(ctx, queryCommandsInsertagentnodeSelectAgentsOrganizationIdRefState, scope.organizationID, agentRef).Scan(&agentID, &role); err != nil {
		return "", "", fmt.Errorf("resolve agent execution target: %w", errs.ErrInvalid)
	}
	nodeRef, _ := newRef("nod")
	var nodeID string
	if err := tx.QueryRow(ctx, queryCommandsInsertagentnodeInsertRunNodesRefRootRunIdParentNodeId, nodeRef, scope.organizationID, rootRunID, runID, parentNodeID, displayName, role, agentID, turnID, truncate(summary, 1000)).Scan(&nodeID); err != nil {
		return "", "", fmt.Errorf("insert agent execution node: %w", errs.ErrUnavailable)
	}
	edgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryCommandsInsertagentnodeInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, scope.organizationID, rootRunID, parentNodeID, nodeID); err != nil {
		return "", "", fmt.Errorf("insert process-to-agent edge: %w", errs.ErrUnavailable)
	}
	return nodeID, nodeRef, nil
}

// truncate trims surrounding whitespace and returns at most the supplied rune count, reserving one
// rune for an ellipsis when truncated. Nonpositive limits yield empty and rune conversion
// preserves the historical replacement of invalid UTF-8 bytes.
// truncate обрезает окружающие пробелы и возвращает не более переданного rune count, резервируя
// одну rune под многоточие при обрезке. Неположительный limit даёт пустую строку, rune conversion
// сохраняет прежнюю замену invalid UTF-8 bytes.
func truncate(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if maximum <= 0 {
		return ""
	}
	if len(runes) <= maximum {
		return string(runes)
	}
	if maximum == 1 {
		return "…"
	}
	return string(runes[:maximum-1]) + "…"
}

// boundedRunTitle preserves titles within240 bytes and otherwise removes complete trailing UTF-8
// runes to fit an ellipsis within that byte cap; it does not generally trim short titles or
// validate their content.
// boundedRunTitle сохраняет titles в пределах240 bytes, иначе удаляет целые завершающие UTF-8
// runes, чтобы многоточие уместилось в byte cap; короткие titles в общем случае не обрезает по
// пробелам и не проверяет содержимое.
func boundedRunTitle(title string) string {
	const maximumBytes = 240
	if len(title) <= maximumBytes {
		return title
	}
	prefix := title
	for len(prefix) > maximumBytes-len("…") {
		_, size := utf8.DecodeLastRuneInString(prefix)
		prefix = prefix[:len(prefix)-size]
	}
	return strings.TrimSpace(prefix) + "…"
}

// emitPlatformEvent delegates a default version-one/stateless platform invalidation to the shared
// snapshot outbox writer in the caller transaction.
// emitPlatformEvent передаёт platform invalidation с default version-one/без state общему snapshot
// outbox writer в транзакции caller.
func (repository *Repository) emitPlatformEvent(ctx context.Context, tx pgx.Tx, scope scope, eventName, projectRef, aggregateRef, summary string) error {
	return repository.emitPlatformEventSnapshot(ctx, tx, scope, eventName, projectRef, aggregateRef, summary, 1, "")
}

// emitCommandOutcomePlatformEvent uses the command aggregate version or one as fallback; artifact
// revision creation additionally reads the exact revision pin before platform event emission. It
// does not commit or publish directly to a broker.
// emitCommandOutcomePlatformEvent использует command aggregate version либо one как fallback;
// artifact revision creation дополнительно читает exact revision pin до platform event emission.
// Не делает commit и не публикует напрямую в broker.
func (repository *Repository) emitCommandOutcomePlatformEvent(ctx context.Context, tx pgx.Tx, scope scope, outcome commandOutcome) error {
	version := outcome.platformAggregateVersion
	if version < 1 {
		version = 1
	}
	if outcome.summary == "i18n:ARTIFACT_REVISION_CREATED" && outcome.result.Artifact != nil {
		item := outcome.result.Artifact
		revision, err := artifactRevisionRow(tx.QueryRow(ctx, queryArtifactRevisionGet, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID, "artifact_ref": item.Ref, "revision_ref": item.CurrentRevisionRef}))
		if err != nil {
			return err
		}
		return repository.emitPlatformEventSnapshotWithRevision(ctx, tx, scope, outcome.platformEvent, outcome.projectRef, outcome.resourceRef, outcome.summary, item.Version, outcome.platformState, &revision)
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, scope, outcome.platformEvent, outcome.projectRef, outcome.resourceRef, outcome.summary, version, outcome.platformState)
}

// emitPlatformEventSnapshot forwards version/state platform invalidation without an
// artifact-revision pin to the canonical outbox writer.
// emitPlatformEventSnapshot передаёт version/state platform invalidation без artifact-revision pin
// каноническому outbox writer.
func (repository *Repository) emitPlatformEventSnapshot(ctx context.Context, tx pgx.Tx, scope scope, eventName, projectRef, aggregateRef, summary string, aggregateVersion int64, state string) error {
	return repository.emitPlatformEventSnapshotWithRevision(ctx, tx, scope, eventName, projectRef, aggregateRef, summary, aggregateVersion, state, nil)
}

// emitPlatformEventSnapshotWithRevision requires a supported closed event kind, increments
// installation sequence and appends the organization-ordered safe invalidation envelope to outbox.
// Optional artifact revision metadata must match exact aggregate/type/digest shape. SQL errors
// retain retry classification; no broker call or commit occurs.
// emitPlatformEventSnapshotWithRevision требует поддерживаемый закрытый event kind, увеличивает
// installation sequence и добавляет organization-ordered safe invalidation envelope в outbox.
// Необязательные artifact revision metadata должны совпасть по exact aggregate/type/digest shape.
// SQL errors сохраняют retry classification; broker call либо commit отсутствуют.
func (repository *Repository) emitPlatformEventSnapshotWithRevision(ctx context.Context, tx pgx.Tx, scope scope, eventName, projectRef, aggregateRef, summary string, aggregateVersion int64, state string, revision *entity.ArtifactRevision) error {
	kind := platformEventKind(eventName)
	if kind == "" {
		return fmt.Errorf("emit platform event: unsupported event name %q", eventName)
	}
	var sequence int64
	if err := tx.QueryRow(ctx, queryCommandsEmitplatformeventUpdateInstallationPlatformSequence).Scan(&sequence); err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	eventID := uuid.New()
	data := map[string]any{"kind": kind, "safeSummary": truncate(summary, 1000)}
	if revision != nil {
		if eventName != "ARTIFACT_CHANGED" || revision.ArtifactRef != aggregateRef || revision.Ref == "" || revision.Revision < 1 || !objectDigestValid(revision.Digest) {
			return errs.ErrInvalid
		}
		data["artifactRevision"] = map[string]any{"ref": revision.Ref, "revision": revision.Revision, "digest": revision.Digest}
	}
	if state != "" {
		data["state"] = state
	}
	payload := map[string]any{"eventId": eventID.String(), "eventName": eventName, "eventVersion": 1, "occurredAt": time.Now().UTC(), "organizationRef": scope.organizationRef, "aggregateRef": aggregateRef, "aggregateVersion": aggregateVersion, "sequence": sequence, "correlationRef": scope.correlationRef, "data": data}
	if projectRef != "" {
		payload["projectRef"] = projectRef
	}
	subject := "control_plane.platform." + scope.organizationRef + ".events"
	if _, err := tx.Exec(ctx, queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload, eventID, subject, "platform:"+scope.organizationRef, sequence, asJSON(payload)); err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	return nil
}

// emitRunEvent forwards an ordinary run event with no incident to the canonical activity/binding
// writer, preserving all caller locators and metadata.
// emitRunEvent передаёт обычный run event без incident каноническому activity/binding writer,
// сохраняя все caller locators и metadata.
func (repository *Repository) emitRunEvent(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef, summary, runState, nodeState string) (entity.RunEvent, error) {
	return repository.emitRunEventWithIncident(ctx, tx, scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef, nil, summary, runState, nodeState)
}

// emitRunEventWithIncident forwards an optional incident without a message to the shared run event
// writer; it does not persist the incident separately here.
// emitRunEventWithIncident передаёт optional incident без message общему run event writer;
// отдельно incident здесь не сохраняет.
func (repository *Repository) emitRunEventWithIncident(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef string, incident *entity.Incident, summary, runState, nodeState string) (entity.RunEvent, error) {
	return repository.emitRunEventWithActivity(ctx, tx, scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef, incident, nil, summary, runState, nodeState)
}

// emitRunEventWithActivity forwards optional incident/message activity without assigning an
// integration invocation binding; the shared writer owns authoritative readback and outbox
// shaping.
// emitRunEventWithActivity передаёт optional incident/message activity без назначения integration
// invocation binding; общий writer владеет authoritative readback и outbox shaping.
func (repository *Repository) emitRunEventWithActivity(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef string, incident *entity.Incident, message *entity.RunMessage, summary, runState, nodeState string) (entity.RunEvent, error) {
	return repository.emitRunEventWithBinding(ctx, tx, scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef, incident, message, summary, runState, nodeState, "")
}

// emitIntegrationActionEvent accepts only succeeded/failed/unknown terminal integration summaries
// bound to an exact invocation ref, then emits the dedicated progress event. The ref must
// originate in a verified locked owner row; syntax validation alone does not authorize it.
// emitIntegrationActionEvent принимает лишь succeeded/failed/unknown terminal integration
// summaries, связанные с точным invocation ref, затем создаёт специализированный progress event.
// Ref должен происходить из проверенной locked owner row; один syntax validation его не
// авторизует.
func (repository *Repository) emitIntegrationActionEvent(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, invocationRef, nodeRef, state string) (entity.RunEvent, error) {
	if !validIntegrationActionBinding("TURN_PROGRESS", integrationActionOutcomeMessage(state), invocationRef) ||
		(state != "SUCCEEDED" && state != "FAILED" && state != "UNKNOWN_OUTCOME") {
		return entity.RunEvent{}, errs.ErrInvalid
	}
	// Ref получен из заблокированной owner-строки после проверки lease/fence;
	// общий aggregateRef никогда не становится публичной привязкой автоматически.
	return repository.emitRunEventWithBinding(ctx, tx, scope, projectID, rootRunID, invocationRef, "TURN_PROGRESS", nodeRef, "", "", "", nil, nil, integrationActionOutcomeMessage(state), "RUNNING", "RUNNING", invocationRef)
}

// validIntegrationActionBinding checks TURN_PROGRESS, bounded inv_ reference grammar and one of
// three closed terminal integration outcome summaries without resolving the invocation itself.
// validIntegrationActionBinding проверяет TURN_PROGRESS, ограниченную inv_ reference grammar и
// одну из трёх закрытых terminal integration outcome summaries без разрешения самого invocation.
func validIntegrationActionBinding(eventType, summary, ref string) bool {
	if eventType != "TURN_PROGRESS" || len(ref) < 8 || len(ref) > 96 || !strings.HasPrefix(ref, "inv_") {
		return false
	}
	for _, char := range ref {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-') {
			return false
		}
	}
	return summary == "i18n:INTEGRATION_ACTION_SUCCEEDED" || summary == "i18n:INTEGRATION_ACTION_FAILED" || summary == "i18n:INTEGRATION_ACTION_OUTCOME_UNKNOWN"
}

// emitRunEventWithBinding validates explicit integration binding, closes terminal tool activity
// and advances root event/graph sequences before authoritative delta readback. It binds
// execution/actor/message provenance, derives current run/node state, stores the full run event
// and ordered run outbox envelope, then emits a limited organization RUN_CHANGED invalidation.
// Caller message source is forced to ORDINARY; no commit or direct broker publish occurs.
// emitRunEventWithBinding проверяет явную integration binding, закрывает terminal tool activity и
// продвигает root event/graph sequences до authoritative delta readback. Связывает
// execution/actor/message provenance, выводит текущие run/node state, сохраняет полный run event и
// ordered run outbox envelope, затем создаёт ограниченную organization RUN_CHANGED invalidation.
// Caller message source принудительно ORDINARY; commit либо direct broker publish отсутствуют.
func (repository *Repository) emitRunEventWithBinding(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef string, incident *entity.Incident, message *entity.RunMessage, summary, runState, nodeState, integrationInvocationRef string) (entity.RunEvent, error) {
	if integrationInvocationRef != "" && (integrationInvocationRef != aggregateRef || !validIntegrationActionBinding(eventType, summary, integrationInvocationRef)) {
		return entity.RunEvent{}, errs.ErrInvalid
	}
	if eventType != "TOOL_CALL_RECORDED" {
		if err := repository.closeTerminalToolActivity(ctx, tx, scope, projectID, rootRunID); err != nil {
			return entity.RunEvent{}, err
		}
	}
	var sequence, version int64
	var rootRef, projectRef string
	var projectValue any
	if err := tx.QueryRow(ctx, queryCommandsEmitruneventUpdateRunsEventSequenceGraphRevisionUpdatedAt, rootRunID).Scan(&rootRef, &sequence, &version); err != nil {
		return entity.RunEvent{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if projectID != "" {
		if err := tx.QueryRow(ctx, queryCommandsEmitruneventSelectProjectsId, projectID).Scan(&projectRef); err != nil {
			return entity.RunEvent{}, serializableTransactionError(err, errs.ErrUnavailable)
		}
		projectValue = projectID
	}
	ref, _ := newRef("evt")
	eventID := uuid.New()
	delta, err := repository.readRunEventDelta(ctx, tx, scope.organizationID, rootRunID, nodeRef, edgeRef, gateRef, artifactRef)
	if err != nil {
		return entity.RunEvent{}, err
	}
	delta.IntegrationInvocationRef = integrationInvocationRef
	var inputActor *entity.RunEventActor
	if delta.Node != nil && delta.Node.TurnRef != "" {
		execution, turnInput, bindingErr := readRuntimeActivityExecution(ctx, tx, scope.organizationID, rootRunID, nodeRef)
		if bindingErr != nil {
			return entity.RunEvent{}, bindingErr
		}
		delta.Execution = execution
		if turnInput.Source.Origin == "CALLBACK_CONTINUATION" {
			delta.Node.InputSummary = callbackContinuationPublicText
		}
		if delta.Node.Type == "AGENT_EXECUTION" && (eventType == "TURN_QUEUED" || eventType == "TURN_STARTED" || eventType == "RUN_CREATED" || eventType == "DELEGATION_CREATED") {
			// Полный пользовательский текст хранится в authoritative turn, а не
			// восстанавливается из усечённой подписи узла.
			var existingSequence int64
			var existingDelta, existingTool []byte
			existingErr := tx.QueryRow(ctx, queryRuntimeActivityLatest, pgx.StrictNamedArgs{
				"organization_id": scope.organizationID, "root_run_id": rootRunID, "node_ref": execution.NodeRef,
				"turn_ref": execution.TurnRef, "attempt": execution.Attempt, "activity_kind": "MESSAGE", "activity_ref": execution.TurnRef,
			}).Scan(&existingSequence, &existingDelta, &existingTool)
			if errors.Is(existingErr, pgx.ErrNoRows) {
				delta.Message = &entity.RunMessage{Ref: execution.TurnRef, Phase: "USER", Revision: 1, Text: turnInput.Content, Source: turnInput.Source}
				inputActor = &turnInput.Actor
			} else if existingErr != nil {
				return entity.RunEvent{}, errs.ErrUnavailable
			}
		}
	}
	if message != nil {
		// Provider не может назначить служебное происхождение сообщения.
		message.Source = entity.MessageSource{Origin: "ORDINARY"}
		if delta.Execution == nil || !validPublishedMessage(message) {
			return entity.RunEvent{}, errs.ErrInvalid
		}
		messageCopy := *message
		delta.Message = &messageCopy
	}
	runState = ""
	if delta.Run != nil {
		runState = delta.Run.State
	}
	nodeState = ""
	if delta.Node != nil {
		nodeState = delta.Node.State
	}
	incidentRef := ""
	if incident != nil {
		incidentCopy := *incident
		delta.Incident = &incidentCopy
		incidentRef = incident.Ref
	}
	safeSummary := truncate(summary, 2000)
	actor, messageKind := runEventPresentation(scope, eventType, delta)
	if inputActor != nil {
		actor = *inputActor
	}
	if delta.Message != nil && delta.Message.Phase == "USER" {
		messageKind = "USER_MESSAGE"
	}
	if message != nil && message.Phase == "FINAL" {
		messageKind = "FINAL_MESSAGE"
	}
	event := entity.RunEvent{Ref: ref, RunRef: rootRef, Sequence: sequence, GraphRevision: delta.Run.GraphRevision, Type: eventType, NodeRef: nodeRef, EdgeRef: edgeRef, GateRef: gateRef, ArtifactRef: artifactRef, IncidentRef: incidentRef, Summary: safeSummary, RunState: runState, NodeState: nodeState, MessageKind: messageKind, Actor: actor, OccurredAt: time.Now().UTC(), Delta: delta}
	if _, err := tx.Exec(ctx, queryCommandsEmitruneventInsertRunEventsEventIdOrganizationIdRootRunId, eventID, ref, scope.organizationID, projectValue, rootRunID, aggregateRef, version, sequence, eventType, nodeRef, edgeRef, gateRef, artifactRef, safeSummary, runState, nodeState, asJSON(delta), scope.actorRef, event.OccurredAt, actor.Kind, actor.Ref, actor.Name, messageKind, nil); err != nil {
		return entity.RunEvent{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	data := map[string]any{"kind": eventKind(eventType), "runRef": rootRef, "safeSummary": safeSummary,
		"actor": map[string]string{"kind": actor.Kind, "ref": actor.Ref, "name": actor.Name}, "messageKind": messageKind}
	if integrationInvocationRef != "" {
		data["integrationInvocationRef"] = integrationInvocationRef
	}
	if execution := delta.Execution; execution != nil {
		data["execution"] = map[string]any{"runRef": execution.RunRef, "nodeRef": execution.NodeRef, "sessionRef": execution.SessionRef,
			"turnRef": execution.TurnRef, "turnNumber": execution.TurnNumber, "attempt": execution.Attempt}
	}
	if message := delta.Message; message != nil {
		data["message"] = map[string]any{"ref": message.Ref, "phase": message.Phase, "revision": message.Revision, "text": message.Text}
	}
	for key, value := range map[string]string{"nodeRef": nodeRef, "edgeRef": edgeRef, "gateRef": gateRef, "artifactRef": artifactRef} {
		if value != "" {
			data[key] = value
		}
	}
	if incidentRef != "" {
		data["incidentRef"] = incidentRef
	}
	if state := eventRunState(runState); state != "" {
		data["state"] = state
	}
	payload := map[string]any{"eventId": eventID.String(), "eventName": eventType, "eventVersion": 1, "occurredAt": event.OccurredAt, "organizationRef": scope.organizationRef, "rootRunRef": rootRef, "aggregateRef": aggregateRef, "aggregateVersion": version, "sequence": sequence, "correlationRef": scope.correlationRef, "data": data}
	if projectRef != "" {
		payload["projectRef"] = projectRef
	}
	subject := "control_plane.run." + scope.organizationRef + "." + rootRef + ".events"
	if _, err := tx.Exec(ctx, queryCommandsEmitruneventInsertOutboxEventsEventIdOrderingKeyPayload, eventID, subject, "run:"+rootRef, sequence, asJSON(payload)); err != nil {
		return entity.RunEvent{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	// Org-wide stream получает только безопасный invalidation-сигнал. Полная
	// delta остаётся в авторитетном run event store и защищённом run stream.
	if err := repository.emitPlatformEventSnapshot(ctx, tx, scope, "RUN_CHANGED", projectRef, rootRef, safeSummary, version, runState); err != nil {
		return entity.RunEvent{}, err
	}
	return event, nil
}

// runEventPresentation selects agent/user/platform display identity from the authoritative
// delta/scope and a closed event-specific message kind. These presentation hints do not establish
// actor authority.
// runEventPresentation выбирает agent/user/platform display identity из authoritative delta/scope
// и закрытый event-specific message kind. Эти presentation hints не устанавливают actor authority.
func runEventPresentation(scope scope, eventType string, delta entity.RunEventDelta) (entity.RunEventActor, string) {
	actor := entity.RunEventActor{Kind: "PLATFORM", Ref: "platform", Name: "Kodex"}
	if delta.Node != nil && delta.Node.AgentRef != "" {
		actor = entity.RunEventActor{Kind: "AGENT", Ref: delta.Node.AgentRef, Name: delta.Node.DisplayName}
	} else if scope.actorRef != "" && scope.actorName != "" {
		actor = entity.RunEventActor{Kind: "USER", Ref: scope.actorRef, Name: scope.actorName}
	}
	switch eventType {
	case "TURN_QUEUED":
		return actor, "USER_MESSAGE"
	case "TURN_PROGRESS":
		return actor, "INTERMEDIATE_MESSAGE"
	case "TURN_COMPLETED", "CALLBACK_DELIVERED":
		return actor, "FINAL_MESSAGE"
	case "OWNER_GATE_OPENED", "OWNER_GATE_RESOLVED":
		return actor, "OWNER_GATE"
	case "ARTIFACT_AVAILABLE":
		return actor, "ARTIFACT"
	case "INCIDENT_LINKED":
		return actor, "INCIDENT"
	case "PLAN_UPDATED":
		return actor, "PLAN_UPDATE"
	case "TOOL_CALL_RECORDED":
		return actor, "TOOL_CALL"
	default:
		return actor, "STATE"
	}
}

// readRunEventDelta reads current run usage/state and requested node/edge/gate/artifact
// projections inside the owner transaction, bounding public text/ref lists and projecting gate
// intent. Any required read/decode failure closes with zero delta; it does not authorize a public
// read or replace the full event snapshot.
// readRunEventDelta читает текущие run usage/state и запрошенные node/edge/gate/artifact
// projections в owner transaction, ограничивает публичные text/ref lists и проецирует gate intent.
// Любой обязательный read/decode failure закрывается с zero delta; публичное чтение не авторизует
// и полный event snapshot не заменяет.
func (repository *Repository) readRunEventDelta(ctx context.Context, tx pgx.Tx, organizationID, rootRunID, nodeRef, edgeRef, gateRef, artifactRef string) (entity.RunEventDelta, error) {
	var run entity.RunDelta
	var usage []byte
	if err := tx.QueryRow(ctx, queryCommandsEmitruneventSelectRunDelta, organizationID, rootRunID).Scan(
		&run.Ref, &run.State, &run.ResultSummary, &run.SafeErrorCode, &run.SafeErrorMessage,
		&run.GraphRevision, &run.EventSequence, &run.Version, &usage, &run.ArtifactRefs, &run.GateRefs,
		&run.StartedAt, &run.FinishedAt,
	); err != nil {
		return entity.RunEventDelta{}, fmt.Errorf("read run event delta: %w", errs.ErrUnavailable)
	}
	var usageErr error
	run.Usage, usageErr = decodeRunUsage(usage)
	if usageErr != nil {
		return entity.RunEventDelta{}, fmt.Errorf("decode run event usage: %w", usageErr)
	}
	run.ResultSummary = truncate(run.ResultSummary, 4000)
	run.SafeErrorCode = truncate(run.SafeErrorCode, 80)
	run.SafeErrorMessage = truncate(run.SafeErrorMessage, 500)
	run.ArtifactRefs = boundedStrings(run.ArtifactRefs, 200)
	run.GateRefs = boundedStrings(run.GateRefs, 200)
	run.NextActions = runActions(run.State, true, true)
	delta := entity.RunEventDelta{Run: &run}
	if nodeRef != "" {
		node, err := scanRunNode(tx.QueryRow(ctx, queryCommandsEmitruneventSelectNodeDelta, organizationID, nodeRef))
		if err != nil {
			return entity.RunEventDelta{}, fmt.Errorf("read run node event delta: %w", err)
		}
		node.InputSummary = truncate(node.InputSummary, 2000)
		node.ProgressSummary = truncate(node.ProgressSummary, 500)
		node.CallbackSummary = truncate(node.CallbackSummary, 500)
		node.SafeErrorCode = truncate(node.SafeErrorCode, 80)
		node.SafeErrorMessage = truncate(node.SafeErrorMessage, 500)
		node.IntegrationNames = boundedStrings(node.IntegrationNames, 50)
		node.ArtifactRefs = boundedStrings(node.ArtifactRefs, 200)
		node.ChildRunRefs = boundedStrings(node.ChildRunRefs, 200)
		node.NextActions = boundedStrings(node.NextActions, 12)
		delta.Node = &node
	}
	if edgeRef != "" {
		var edge entity.RunEdge
		if err := tx.QueryRow(ctx, queryCommandsEmitruneventSelectEdgeDelta, organizationID, edgeRef).Scan(&edge.Ref, &edge.RunRef, &edge.SourceNodeRef, &edge.TargetNodeRef, &edge.Type, &edge.Label); err != nil {
			return entity.RunEventDelta{}, fmt.Errorf("read run edge event delta: %w", errs.ErrUnavailable)
		}
		edge.Label = truncate(edge.Label, 120)
		delta.Edge = &edge
	}
	if gateRef != "" {
		gate, err := scanGate(tx.QueryRow(ctx, queryCommandsEmitruneventSelectGateDelta, organizationID, gateRef), false)
		if err != nil {
			return entity.RunEventDelta{}, fmt.Errorf("read owner gate event delta: %w", err)
		}
		gate.Title = truncate(gate.Title, 240)
		gate.ContextSummary = truncate(gate.ContextSummary, 4000)
		gate.Prompt = truncate(gate.Prompt, 2000)
		gate.DecisionComment = truncate(gate.DecisionComment, 4000)
		gate.AllowedDecisions = boundedStrings(gate.AllowedDecisions, 4)
		gate.NextActions = boundedStrings(gate.NextActions, 12)
		if err := repository.projectGateIntent(ctx, tx, scope{organizationID: organizationID}, &gate, false); err != nil {
			return entity.RunEventDelta{}, err
		}
		delta.Gate = &gate
	}
	if artifactRef != "" {
		artifact, err := scanArtifact(tx.QueryRow(ctx, queryCommandsEmitruneventSelectArtifactDelta, organizationID, artifactRef))
		if err != nil {
			return entity.RunEventDelta{}, fmt.Errorf("read artifact event delta: %w", err)
		}
		artifact.FileName = truncate(artifact.FileName, 255)
		artifact.MediaType = truncate(artifact.MediaType, 160)
		artifact.Bindings = boundedStrings(artifact.Bindings, 200)
		artifact.NextActions = boundedStrings(artifact.NextActions, 12)
		delta.Artifact = &artifact
	}
	return delta, nil
}

// boundedStrings returns an independent append-to-nil copy of at most the nonnegative maximum
// prefix, preserving order and empty/nil shape under the historical copy semantics.
// boundedStrings возвращает независимую append-to-nil копию не более nonnegative maximum элементов
// prefix, сохраняя порядок и empty/nil shape по прежним copy semantics.
func boundedStrings(values []string, maximum int) []string {
	if len(values) > maximum {
		values = values[:maximum]
	}
	return append([]string(nil), values...)
}

// platformEventKind maps only registered platform event names to closed public resource kinds and
// returns empty for unsupported events so the writer can reject them.
// platformEventKind отображает лишь зарегистрированные platform event names в закрытые публичные
// resource kinds, возвращая пустое для неподдерживаемых events, чтобы writer их отклонил.
func platformEventKind(eventName string) string {
	switch eventName {
	case "PROJECT_CHANGED":
		return "PROJECT"
	case "AGENT_CHANGED":
		return "AGENT"
	case "RUNTIME_ENVIRONMENT_CHANGED":
		return "RUNTIME_ENVIRONMENT"
	case "ARTIFACT_CHANGED":
		return "ARTIFACT"
	case "INSTRUCTIONS_PUBLISHED":
		return "INSTRUCTIONS"
	case "WORKFLOW_CHANGED":
		return "WORKFLOW"
	case "SCHEDULE_CHANGED":
		return "SCHEDULE"
	case "PROVIDER_ACCOUNT_CHANGED":
		return "PROVIDER_ACCOUNT"
	case "RUNTIME_SECRET_CHANGED":
		return "RUNTIME_SECRET"
	case "MANAGED_CONFIGURATION_CHANGED":
		return "MANAGED_CONFIGURATION"
	case "INTEGRATION_CONNECTION_CHANGED":
		return "INTEGRATION_CONNECTION"
	case "INTEGRATION_GRANT_CHANGED":
		return "INTEGRATION_GRANT"
	case "MEMBERSHIP_CHANGED":
		return "MEMBERSHIP"
	case "PLATFORM_MEMBERSHIP_CHANGED":
		return "PLATFORM_MEMBERSHIP"
	case "SYSTEM_ASSISTANT_CHANGED":
		return "SYSTEM_ASSISTANT"
	case "ROLE_IMAGE_RECIPE_CHANGED", "ROLE_IMAGE_PROMOTION_REQUESTED", "ROLE_IMAGE_PROMOTED":
		return "ROLE_IMAGE_RECIPE"
	case "RUN_CHANGED":
		return "RUN"
	default:
		return ""
	}
}

// eventRunState maps internal WAITING_HUMAN to public WAITING_OWNER, preserves the five supported
// ordinary run states and omits unknown states.
// eventRunState отображает внутреннее WAITING_HUMAN в публичное WAITING_OWNER, сохраняет пять
// поддерживаемых обычных run states и пропускает неизвестные states.
func eventRunState(value string) string {
	switch value {
	case "WAITING_HUMAN":
		return "WAITING_OWNER"
	case "QUEUED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED":
		return value
	default:
		return ""
	}
}

// eventKind maps known run event types to closed delta categories and uses RUN for all remaining
// types, keeping public category cardinality bounded.
// eventKind отображает известные run event types в закрытые delta categories, используя RUN для
// остальных types и сохраняя ограниченную публичную category cardinality.
func eventKind(eventType string) string {
	switch eventType {
	case "NODE_ADDED", "NODE_STATE_CHANGED":
		return "NODE"
	case "EDGE_ADDED":
		return "EDGE"
	case "TURN_QUEUED", "TURN_STARTED", "TURN_PROGRESS", "TURN_COMPLETED":
		return "TURN"
	case "DELEGATION_CREATED":
		return "DELEGATION"
	case "CALLBACK_DELIVERED":
		return "CALLBACK"
	case "OWNER_GATE_OPENED", "OWNER_GATE_RESOLVED":
		return "OWNER_GATE"
	case "ARTIFACT_AVAILABLE":
		return "ARTIFACT"
	case "INCIDENT_LINKED":
		return "INCIDENT"
	case "TOOL_CALL_RECORDED":
		return "TOOL_CALL"
	case "PLAN_UPDATED":
		return "PLAN"
	default:
		return "RUN"
	}
}

// readRunGraphTx reads the internal run snapshot, assistant pin and root cursor, then all
// node/edge rows with explicit closure/error checks. It returns zero values on failure and relies
// on the caller authorization/transaction boundary rather than granting public access itself.
// readRunGraphTx читает внутренние run snapshot, assistant pin и root cursor, затем все node/edge
// rows с явными closure/error checks. Возвращает zero values при ошибке и полагается на caller
// authorization/transaction boundary, сам публичный доступ не выдаёт.
func (repository *Repository) readRunGraphTx(ctx context.Context, tx pgx.Tx, scope scope, runRef string) (entity.Run, entity.RunGraph, error) {
	run, err := scanRun(tx.QueryRow(ctx, queryCommandsGetrunforgraphSelectRunsOrganizationIdRef, scope.organizationID, runRef), false)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, fmt.Errorf("read run snapshot: %w", err)
	}
	if err := attachRunAssistantPin(ctx, tx, scope, &run); err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	graph, err := readRootRunGraphCursor(ctx, tx, scope, run)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, err
	}
	rows, err := tx.Query(ctx, queryCommandsReadrungraphtxSelectRunNodesOrganizationIdRootRunIdRef, scope.organizationID, runRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, fmt.Errorf("query run graph nodes: %w", errs.ErrUnavailable)
	}
	for rows.Next() {
		var n entity.RunNode
		if err := rows.Scan(&n.Ref, &n.RunRef, &n.ParentNodeRef, &n.Type, &n.State, &n.DisplayName, &n.Role, &n.AgentRef, &n.TurnRef, &n.Attempt, &n.InputSummary, &n.ProgressSummary, &n.IntegrationNames, &n.CallbackSummary, &n.SafeErrorCode, &n.SafeErrorMessage, &n.NextActions, &n.MaterializationState, &n.CreatedAt, &n.StartedAt, &n.FinishedAt, &n.ArtifactRefs, &n.ChildRunRefs); err != nil {
			rows.Close()
			return entity.Run{}, entity.RunGraph{}, fmt.Errorf("scan run graph node: %w", errs.ErrUnavailable)
		}
		graph.Nodes = append(graph.Nodes, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return entity.Run{}, entity.RunGraph{}, fmt.Errorf("iterate run graph nodes: %w", errs.ErrUnavailable)
	}
	rows.Close()
	edgeRows, err := tx.Query(ctx, queryCommandsReadrungraphtxSelectRunEdgesOrganizationIdRootRunIdRef, scope.organizationID, runRef)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, fmt.Errorf("query run graph edges: %w", errs.ErrUnavailable)
	}
	for edgeRows.Next() {
		var e entity.RunEdge
		if err := edgeRows.Scan(&e.Ref, &e.RunRef, &e.SourceNodeRef, &e.TargetNodeRef, &e.Type, &e.Label); err != nil {
			edgeRows.Close()
			return entity.Run{}, entity.RunGraph{}, fmt.Errorf("scan run graph edge: %w", errs.ErrUnavailable)
		}
		graph.Edges = append(graph.Edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		edgeRows.Close()
		return entity.Run{}, entity.RunGraph{}, fmt.Errorf("iterate run graph edges: %w", errs.ErrUnavailable)
	}
	edgeRows.Close()
	return run, graph, nil
}

// addSessionTurn resolves an active session target and exact continuation preview pin,
// reauthorizes the nested launch and preserves workflow input. It pins the new turn dependencies
// and, when requested, records the server-owned previous/new root CONTINUES edge and fresh graph
// readback in the caller transaction.
// addSessionTurn разрешает active session target и exact continuation preview pin, повторно
// авторизует nested launch и сохраняет workflow input. Закрепляет dependencies нового turn и при
// запросе записывает server-owned previous/new root CONTINUES edge со свежим graph readback в
// транзакции caller.
func (repository *Repository) addSessionTurn(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SessionTurnInput)
	if !ok || payload.SessionRef == "" || strings.TrimSpace(payload.Task) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID, projectRef, targetType, targetRef string
	if err := tx.QueryRow(ctx, queryCommandsAddsessionturnSelectSessionsOrganizationIdRefState, scope.organizationID, payload.SessionRef).Scan(&projectID, &projectRef, &targetType, &targetRef); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	promptPin, err := repository.checkContinuationPreviewPinTx(ctx, tx, scope, payload)
	if err != nil {
		return commandOutcome{}, err
	}
	launch := command.LaunchRunInput{ProjectRef: projectRef, Title: "i18n:SESSION_CONTINUATION", Task: payload.Task, SessionRef: payload.SessionRef, Source: "CONTROL_CENTER", Target: entity.RunTarget{Type: targetType, Ref: targetRef}, AttachmentSetRef: payload.AttachmentSetRef, AttachmentPurpose: "SESSION_TURN"}
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = launch
	if err := repository.authorizeCommand(ctx, tx, scope, nested); err != nil {
		return commandOutcome{}, err
	}
	if targetType == "WORKFLOW" {
		launch.Input, err = readSessionContinuationInput(ctx, tx, scope, payload.SessionRef, payload.RunRef)
		if err != nil {
			return commandOutcome{}, err
		}
		nested.Payload = launch
	}
	outcome, err := repository.launchRun(ctx, tx, scope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	if promptPin.Digest != "" {
		if outcome.result.Run == nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		updated, err := tx.Exec(ctx, queryPromptContinuationPinTurn, pgx.StrictNamedArgs{"organization_id": scope.organizationID, "run_ref": outcome.result.Run.Ref, "context_digest": promptPin.Digest, "dependency_digest": promptPin.DependencyDigest})
		if err != nil || updated.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if outcome.result.Run != nil && payload.RunRef != "" {
		var previousRootID, newRootID, previousNodeID, newNodeID string
		if err := tx.QueryRow(ctx, queryCommandsAddsessionturnSelectRunsOrganizationIdRef, scope.organizationID, payload.RunRef, payload.SessionRef).Scan(&previousRootID); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err := tx.QueryRow(ctx, queryCommandsAddsessionturnSelectRunsRef, outcome.result.Run.Ref).Scan(&newRootID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := tx.QueryRow(ctx, queryCommandsAddsessionturnSelectPreviousRootNode, previousRootID).Scan(&previousNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := tx.QueryRow(ctx, queryCommandsAddsessionturnSelectContinuationRootNode, newRootID).Scan(&newNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		edgeRef, _ := newRef("edg")
		if _, err := tx.Exec(ctx, queryCommandsAddsessionturnInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, scope.organizationID, newRootID, previousNodeID, newNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, newRootID, edgeRef, "EDGE_ADDED", "", edgeRef, "", "", "i18n:SESSION_CONTINUED", "QUEUED", ""); err != nil {
			return commandOutcome{}, err
		}
		continuedRun, graph, err := repository.readRunGraphTx(ctx, tx, scope, outcome.result.Run.Ref)
		if err != nil {
			return commandOutcome{}, err
		}
		outcome.result.Run = &continuedRun
		outcome.result.Graph = &graph
	}
	outcome.summary = "i18n:SESSION_CONTINUED"
	return outcome, nil
}

// changeRun handles cancel/retry with exact run OCC. Cancellation locks nodes before runs to match
// claim order, closes root/nodes/leases/occurrences/turns/gates and integration effects while
// retaining uncertain WRITE outcomes, then emits authoritative transitions. Retry creates a new
// launch/session attempt with preserved attachment policy and explicit retry lineage; a child
// retry becomes an independent root. It does not retry provider calls or commit here.
// changeRun обрабатывает cancel/retry с точным run OCC. Cancellation блокирует nodes до runs в
// порядке claim, закрывает root/nodes/leases/occurrences/turns/gates и integration effects,
// сохраняя uncertain WRITE outcomes, затем создаёт authoritative transitions. Retry создаёт новый
// launch/session attempt с прежней attachment policy и явной retry lineage; child retry становится
// независимым root. Provider calls не повторяет и commit здесь не делает.
func (repository *Repository) changeRun(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RunCommandInput)
	if !ok || payload.RunRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	// Runtime claim сначала блокирует run_nodes, а затем через FK каталога
	// обращается к runs. Cancel обязан соблюдать тот же порядок, иначе
	// одновременный capture runtime file catalog образует deadlock.
	if input.Kind == command.CancelRun {
		var rootRunID string
		if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRootRunID, scope.organizationID, payload.RunRef).Scan(&rootRunID); err != nil {
			return commandOutcome{}, errs.ErrNotFound
		}
		rows, err := tx.Query(ctx, queryCommandsChangerunLockRunNodes, rootRunID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		for rows.Next() {
			var ignored string
			if err := rows.Scan(&ignored); err != nil {
				rows.Close()
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		rows.Close()
	}
	var runID, rootRunID, projectID, projectRef, state, targetType string
	var version int64
	var attempt int32
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRunsOrganizationIdRef, scope.organizationID, payload.RunRef).Scan(&runID, &rootRunID, &projectID, &projectRef, &state, &version, &attempt, &targetType); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if input.Kind == command.CancelRun {
		if !contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"}, state) {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, queryCommandsChangerunUpdateRunsStateSafeErrorCodeSafeErrorMessage, rootRunID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		nodeRows, err := tx.Query(ctx, queryCommandsChangerunUpdateRunNodesStateNextActionsFinishedAt, rootRunID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var cancelledNodeRefs []string
		for nodeRows.Next() {
			var ref string
			if err := nodeRows.Scan(&ref); err != nil {
				nodeRows.Close()
				return commandOutcome{}, errs.ErrUnavailable
			}
			cancelledNodeRefs = append(cancelledNodeRefs, ref)
		}
		if err := nodeRows.Err(); err != nil {
			nodeRows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		nodeRows.Close()
		if _, err := tx.Exec(ctx, queryCommandsChangerunUpdateRuntimeLeasesStateUpdatedAt, rootRunID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryCommandsChangerunUpdateScheduleOccurrencesStateCancelled, rootRunID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, queryCommandsChangerunUpdateSessionTurnsStateCompletedAt, rootRunID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		// Отмена закрывает также уже подготовленные эффекты. Начатый WRITE
		// нельзя считать отменённым без подтверждения внешнего результата.
		effectRows, err := tx.Query(ctx, queryCommandsChangerunCloseIntegrationEffects, scope.organizationID, rootRunID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		// cancelledEffect holds one local invocation/node/outcome tuple returned by cancellation for
		// subsequent progress events; UNKNOWN_OUTCOME is preserved rather than declared externally
		// cancelled.
		// cancelledEffect хранит локальный invocation/node/outcome tuple результата cancellation для
		// последующих progress events; UNKNOWN_OUTCOME сохраняется, не объявляясь внешне отменённым.
		type cancelledEffect struct{ invocationRef, nodeRef, state string }
		var cancelledEffects []cancelledEffect
		for effectRows.Next() {
			var effect cancelledEffect
			if err := effectRows.Scan(&effect.invocationRef, &effect.nodeRef, &effect.state); err != nil {
				effectRows.Close()
				return commandOutcome{}, errs.ErrUnavailable
			}
			cancelledEffects = append(cancelledEffects, effect)
		}
		if err := effectRows.Err(); err != nil {
			effectRows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		effectRows.Close()
		gateRows, err := tx.Query(ctx, queryCommandsChangerunUpdateOwnerGatesStateDecisionDecisionComment, rootRunID, scope.actorID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		// cancelledGate holds one local cancelled gate/node tuple for exact OWNER_GATE_RESOLVED emission
		// after the cancellation transaction changes state.
		// cancelledGate хранит локальный cancelled gate/node tuple для точного OWNER_GATE_RESOLVED после
		// изменения state в cancellation transaction.
		type cancelledGate struct{ gateRef, nodeRef string }
		var cancelledGates []cancelledGate
		for gateRows.Next() {
			var gate cancelledGate
			if err := gateRows.Scan(&gate.gateRef, &gate.nodeRef); err != nil {
				gateRows.Close()
				return commandOutcome{}, errs.ErrUnavailable
			}
			cancelledGates = append(cancelledGates, gate)
		}
		if err := gateRows.Err(); err != nil {
			gateRows.Close()
			return commandOutcome{}, errs.ErrUnavailable
		}
		gateRows.Close()
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.RunRef, "RUN_STATE_CHANGED", "", "", "", "", "i18n:RUN_CANCELLED", "CANCELLED", ""); err != nil {
			return commandOutcome{}, err
		}
		for _, nodeRef := range cancelledNodeRefs {
			if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, nodeRef, "NODE_STATE_CHANGED", nodeRef, "", "", "", "i18n:RUN_NODE_CANCELLED", "CANCELLED", "CANCELLED"); err != nil {
				return commandOutcome{}, err
			}
		}
		for _, gate := range cancelledGates {
			if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, gate.gateRef, "OWNER_GATE_RESOLVED", gate.nodeRef, "", gate.gateRef, "", "i18n:OWNER_GATE_CANCELLED", "CANCELLED", "CANCELLED"); err != nil {
				return commandOutcome{}, err
			}
		}
		for _, effect := range cancelledEffects {
			summary := "i18n:RUN_CANCELLED"
			if effect.state == "UNKNOWN_OUTCOME" {
				summary = integrationActionOutcomeMessage(effect.state)
			}
			if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, effect.invocationRef, "TURN_PROGRESS", effect.nodeRef, "", "", "", summary, "CANCELLED", "CANCELLED"); err != nil {
				return commandOutcome{}, err
			}
		}
		run, graph, err := repository.readRunGraphTx(ctx, tx, scope, payload.RunRef)
		if err != nil {
			return commandOutcome{}, err
		}
		platformEvent := ""
		if targetType == "SYSTEM_ASSISTANT" {
			platformEvent = "SYSTEM_ASSISTANT_CHANGED"
		}
		return commandOutcome{result: command.Result{Run: &run, Graph: &graph}, projectID: projectID, projectRef: projectRef, resourceKind: "RUN", resourceRef: payload.RunRef, summary: "i18n:RUN_CANCELLED", platformEvent: platformEvent}, nil
	}
	if !contains([]string{"FAILED", "CANCELLED"}, state) {
		return commandOutcome{}, errs.ErrConflict
	}
	var targetRef, title, titleSource, task, sessionRef, source string
	var raw []byte
	var attachmentSetRef, attachmentPurpose string
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRunsId, runID).Scan(&targetType, &targetRef, &title, &titleSource, &task, &sessionRef, &source, &raw, &attachmentSetRef, &attachmentPurpose); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var launchInput map[string]any
	_ = json.Unmarshal(raw, &launchInput)
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: boundedRunTitle(title), TitleSource: titleSource, Task: task, SessionRef: sessionRef, Source: source, Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: launchInput, AttachmentSetRef: attachmentSetRef, AttachmentPurpose: attachmentPurpose}
	var outcome commandOutcome
	var err error
	if targetType == "SYSTEM_ASSISTANT" {
		outcome, err = repository.retryAssistantRun(ctx, tx, scope, input, runID)
	} else {
		outcome, err = repository.launchRunWithAttachmentPolicy(ctx, tx, scope, nested, true)
	}
	if err != nil {
		return commandOutcome{}, err
	}
	var newRunID, newRootID, newRootNodeID, oldRootNodeID string
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRunsRef, outcome.result.Run.Ref).Scan(&newRunID, &newRootID); err != nil {
		return commandOutcome{}, fmt.Errorf("read retry run identity: %w", errs.ErrUnavailable)
	}
	if _, err := tx.Exec(ctx, queryCommandsChangerunUpdateRunsRetryOfRunIdAttempt, newRunID, runID, attempt+1); err != nil {
		return commandOutcome{}, fmt.Errorf("link retry run attempt: %w", errs.ErrUnavailable)
	}
	// Повтор дочернего запуска становится самостоятельным корнем. Его прежний
	// процесс не является повторяемым запуском: связь доступна через RetryOfRunRef.
	if runID == rootRunID {
		if targetType == "SYSTEM_ASSISTANT" {
			if err := tx.QueryRow(ctx, queryAssistantRetryRootNode, scope.organizationID, rootRunID).Scan(&oldRootNodeID); err != nil {
				return commandOutcome{}, fmt.Errorf("read prior assistant retry node: %w", errs.ErrUnavailable)
			}
			if err := tx.QueryRow(ctx, queryAssistantRetryRootNode, scope.organizationID, newRootID).Scan(&newRootNodeID); err != nil {
				return commandOutcome{}, fmt.Errorf("read new assistant retry node: %w", errs.ErrUnavailable)
			}
		} else {
			if err := tx.QueryRow(ctx, queryCommandsChangerunSelectPreviousRootNode, rootRunID).Scan(&oldRootNodeID); err != nil {
				return commandOutcome{}, fmt.Errorf("read prior retry root node: %w", errs.ErrUnavailable)
			}
			if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRetryRootNode, newRootID).Scan(&newRootNodeID); err != nil {
				return commandOutcome{}, fmt.Errorf("read new retry root node: %w", errs.ErrUnavailable)
			}
		}
		edgeRef, _ := newRef("edg")
		if _, err := tx.Exec(ctx, queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, scope.organizationID, newRootID, oldRootNodeID, newRootNodeID); err != nil {
			return commandOutcome{}, fmt.Errorf("insert retry lineage edge: %w", errs.ErrUnavailable)
		}
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, newRootID, edgeRef, "EDGE_ADDED", "", edgeRef, "", "", "i18n:RUN_RETRY_CREATED", "QUEUED", ""); err != nil {
			return commandOutcome{}, err
		}
	}

	retryRun, graph, err := repository.readRunGraphTx(ctx, tx, scope, outcome.result.Run.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.result.Run = &retryRun
	outcome.result.Graph = &graph
	outcome.summary = "i18n:RUN_RETRY_CREATED"
	return outcome, nil
}

// resolveGate routes delivery gates separately, otherwise locks the owner gate, checks OCC/allowed
// decision and finalized attachments, and applies specialized integration or ordinary workflow
// effects. REQUEST_CHANGES creates a new turn/requeues the canonical predecessor; terminal
// decisions update root/deliveries, emit gate/node events and return gate/run/graph readback.
// Source actor authority remains the verified command scope and all effects belong to the caller
// transaction.
// resolveGate отдельно направляет delivery gates, иначе блокирует owner gate, проверяет
// OCC/allowed decision и finalized attachments, применяет специализированные integration либо
// обычные workflow effects. REQUEST_CHANGES создаёт новый turn/повторно ставит канонический
// predecessor; terminal decisions обновляют root/deliveries, создают gate/node events и возвращают
// gate/run/graph readback. Source actor authority остаётся проверенным command scope, все effects
// принадлежат транзакции caller.
func (repository *Repository) resolveGate(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.GateResolutionInput)
	if !ok || payload.GateRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	stateMap := map[string]string{"APPROVE": "APPROVED", "REJECT": "REJECTED", "REQUEST_CHANGES": "CHANGES_REQUESTED", "CANCEL": "CANCELLED"}
	nextState := stateMap[payload.Decision]
	if nextState == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	if outcome, handled, err := repository.resolveInteractionDeliveryGate(ctx, tx, scope, input); handled {
		return outcome, err
	}
	var gateID, nodeID, rootRunID, projectID, projectRef, gateNodeRef string
	var predecessorNodeID, predecessorNodeRef, predecessorRunID, sessionID, integrationInvocationID, integrationApprovalPolicy string
	var version int64
	var allowed []string
	err := tx.QueryRow(ctx, queryCommandsResolvegateSelectOwnerGatesOrganizationIdRefState, scope.organizationID, payload.GateRef).Scan(
		&gateID, &nodeID, &rootRunID, &projectID, &projectRef, &version, &allowed, &gateNodeRef,
		&predecessorNodeID, &predecessorNodeRef, &predecessorRunID, &sessionID,
		&integrationInvocationID, &integrationApprovalPolicy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrAlreadyResolved
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if !contains(allowed, payload.Decision) {
		return commandOutcome{}, errs.ErrForbidden
	}
	attachmentSet, err := repository.resolveFinalizedAttachmentSet(ctx, tx, scope, projectID, payload.AttachmentSetRef, "OWNER_GATE_MESSAGE", false)
	if err != nil {
		return commandOutcome{}, err
	}
	if integrationInvocationID != "" {
		if integrationApprovalPolicy == "HUMAN_SCOPED" && payload.Decision == "APPROVE" {
			if err := repository.approveIntegrationScope(ctx, tx, scope, gateID, integrationInvocationID, rootRunID, projectID); err != nil {
				return commandOutcome{}, err
			}
		}
		invocationState, safeErrorCode := "READY", ""
		if payload.Decision == "REJECT" {
			invocationState, safeErrorCode = "REJECTED", "INTEGRATION_REJECTED_BY_OWNER"
		} else if payload.Decision == "CANCEL" {
			invocationState, safeErrorCode = "CANCELLED", "INTEGRATION_CANCELLED_BY_OWNER"
		}
		tag, err := tx.Exec(ctx, queryCommandsResolvegateUpdateIntegrationInvocation,
			integrationInvocationID, invocationState, safeErrorCode,
		)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateOwnerGatesStateDecisionDecisionComment, gateID, nextState, payload.Decision, truncate(payload.Comment, 2000), scope.actorID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if integrationInvocationID != "" && (payload.Decision == "REJECT" || payload.Decision == "CANCEL") {
		if _, err := repository.scheduleIntegrationContinuation(ctx, tx, scope, integrationInvocationID, projectID); err != nil {
			return commandOutcome{}, err
		}
	}
	if attachmentSet.ID != "" {
		tag, err := tx.Exec(ctx, queryAttachmentSetsBindGateResolution, pgx.StrictNamedArgs{
			"attachment_set_id": attachmentSet.ID,
			"gate_id":           gateID,
		})
		if err != nil || tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.bindAttachmentSet(ctx, tx, scope, projectID, attachmentSet, "OWNER_GATE_MESSAGE", gateID); err != nil {
			return commandOutcome{}, err
		}
	}
	if _, err := tx.Exec(ctx, queryInteractionCancelPendingGateDeliveries, pgx.StrictNamedArgs{"gate_id": gateID}); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	nodeState := "SUCCEEDED"
	runState := "RUNNING"
	if payload.Decision == "REJECT" {
		nodeState = "FAILED"
		if integrationInvocationID == "" {
			runState = "FAILED"
		}
	} else if payload.Decision == "CANCEL" {
		nodeState = "CANCELLED"
		if integrationInvocationID == "" {
			runState = "CANCELLED"
		}
	}
	if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunNodesStateFinishedAtVersion, nodeID, nodeState); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if payload.Decision == "REQUEST_CHANGES" {
		comment := strings.TrimSpace(payload.Comment)
		if comment == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		turnRef, err := newRef("trn")
		if err != nil {
			return commandOutcome{}, err
		}
		var turnID string
		if err := tx.QueryRow(ctx, queryCommandsResolvegateInsertChangeRequestTurn, turnRef, scope.organizationID,
			sessionID, predecessorRunID, scope.actorRef, truncate(comment, 2000)).Scan(&turnID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := repository.attachSetToTurn(ctx, tx, scope, projectID, attachmentSet, turnID, "SESSION_TURN"); err != nil {
			return commandOutcome{}, err
		}
		if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateChangeRequestSession, sessionID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		tag, err := tx.Exec(ctx, queryCommandsResolvegateRequeuePredecessorNode, predecessorNodeID, turnID, truncate(comment, 1000))
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if payload.Decision == "APPROVE" {
		var active int
		if err := tx.QueryRow(ctx, queryCommandsResolvegateSelectActiveAgentNodes, rootRunID).Scan(&active); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if active == 0 {
			if integrationInvocationID != "" {
				// Исполнение ожидает внешний effect и следующий ход той же Session.
				runState = "WAITING_HUMAN"
			} else {
				runState = "SUCCEEDED"
				var requiredFailed bool
				if tx.QueryRow(ctx, queryWorkflowLaunchFailed, pgx.StrictNamedArgs{"root_run_id": rootRunID}).Scan(&requiredFailed) != nil {
					return commandOutcome{}, errs.ErrUnavailable
				}
				if requiredFailed {
					runState = "FAILED"
				}
			}
		}
	}
	if integrationInvocationID != "" {
		var openGates int64
		if err := tx.QueryRow(ctx, queryCommandsResolvegateCountOpenGates, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID, "root_run_id": rootRunID,
		}).Scan(&openGates); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if openGates > 0 {
			runState = "WAITING_HUMAN"
		}
	}
	terminalRootNodeRef := ""
	if runState == "SUCCEEDED" {
		if _, err := tx.Exec(ctx, queryCommandsResolvegateCompleteRootRun, rootRunID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else if _, err := tx.Exec(ctx, queryCommandsResolvegateUpdateRunsStateVersionUpdatedAt, rootRunID, runState); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if contains([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, runState) {
		if err := tx.QueryRow(ctx, queryCommandsResolvegateUpdateRootNodeState, rootRunID, runState).Scan(&terminalRootNodeRef); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if err := repository.enqueueTerminalInteractionDeliveries(ctx, tx, scope, projectID, rootRunID); err != nil {
			return commandOutcome{}, err
		}
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.GateRef, "OWNER_GATE_RESOLVED", gateNodeRef, "", payload.GateRef, "", "i18n:OWNER_GATE_RESOLVED", runState, nodeState)
	if err != nil {
		return commandOutcome{}, err
	}
	if payload.Decision == "REQUEST_CHANGES" {
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, predecessorNodeRef, "NODE_STATE_CHANGED", predecessorNodeRef, "", "", "", "i18n:OWNER_CHANGES_QUEUED", "RUNNING", "QUEUED"); err != nil {
			return commandOutcome{}, err
		}
	}
	if terminalRootNodeRef != "" {
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, terminalRootNodeRef, "NODE_STATE_CHANGED", terminalRootNodeRef, "", "", "", "i18n:ROOT_PROCESS_COMPLETED", runState, runState); err != nil {
			return commandOutcome{}, err
		}
	}
	runRef, err := mustRunRef(ctx, tx, rootRunID)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, runRef)
	if err != nil {
		return commandOutcome{}, err
	}
	gate, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId,
		scope.organizationID, payload.GateRef, scope.role, scope.actorID), true)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.projectGateIntent(ctx, tx, scope, &gate, true); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Gate: &gate, Run: &run, Graph: &graph, Event: &event}, projectID: projectID, projectRef: projectRef, resourceKind: "OWNER_GATE", resourceRef: payload.GateRef, summary: "i18n:OWNER_GATE_RESOLVED"}, nil
}

// mustRunRef reads a run reference by an already resolved internal ID, mapping any lookup error to
// empty/Unavailable without resolving tenant authority itself.
// mustRunRef читает run reference по уже разрешённому внутреннему ID, отображая любую lookup error
// в empty/Unavailable без самостоятельного разрешения tenant authority.
func mustRunRef(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var ref string
	if err := tx.QueryRow(ctx, queryCommandsMustrunrefSelectRunsId, id).Scan(&ref); err != nil {
		return "", errs.ErrUnavailable
	}
	return ref, nil
}

// lockProjectMembership reads the exact tenant/project/binding through the existing locking SQL
// and eleven destinations. Missing rows, including wrapped ErrNoRows, map to NotFound; other scan
// failures map to Unavailable with empty internal IDs. Callers discard partial item data on error
// and retain OCC, self-mutation/grant policy, write SQL, action hints and transaction ownership.
// lockProjectMembership читает точные tenant/project/binding прежним locking SQL и одиннадцатью
// destinations. Отсутствие строки, включая wrapped ErrNoRows, даёт NotFound; прочие scan failures
// дают Unavailable с пустыми внутренними IDs. Callers отбрасывают частичный item при ошибке и
// сохраняют OCC, self-mutation/grant policy, write SQL, action hints и владение transaction.
func lockProjectMembership(ctx context.Context, tx pgx.Tx, organizationID, projectID, ref string, item *entity.Membership) (string, string, error) {
	var membershipID, subjectID string
	err := tx.QueryRow(ctx, queryProjectMembershipResolveForUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "membership_ref": ref,
	}).Scan(&membershipID, &subjectID, &item.Ref, &item.User.Ref, &item.User.DisplayName,
		&item.User.EmailMasked, &item.User.Active, &item.Role, &item.Permissions, &item.Active, &item.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errs.ErrNotFound
	}
	if err != nil {
		return "", "", errs.ErrUnavailable
	}
	return membershipID, subjectID, nil
}

// lockPlatformMembership reads the exact organization/platform binding with its distinct ten-field
// projection, leaving project permissions untouched. It preserves NotFound/Unavailable mapping
// and returns empty IDs on failure; the caller owns partial-data discard, OCC, owner/self/last-owner
// guards, project revocation, write SQL and commit. It never substitutes project binding authority.
// lockPlatformMembership читает точные organization/platform binding отдельной проекцией десяти
// полей, не меняя project permissions. Сохраняет NotFound/Unavailable mapping и возвращает пустые
// IDs при ошибке; caller владеет отбрасыванием partial data, OCC, owner/self/last-owner guards,
// project revocation, write SQL и commit. Project binding authority не подставляется.
func lockPlatformMembership(ctx context.Context, tx pgx.Tx, organizationID, ref string, item *entity.Membership) (string, string, error) {
	var membershipID, subjectID string
	err := tx.QueryRow(ctx, queryPlatformMembershipResolveForUpdate, pgx.StrictNamedArgs{
		"organization_id": organizationID, "membership_ref": ref,
	}).Scan(&membershipID, &subjectID, &item.Ref, &item.User.Ref, &item.User.DisplayName,
		&item.User.EmailMasked, &item.User.Active, &item.Role, &item.Active, &item.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errs.ErrNotFound
	}
	if err != nil {
		return "", "", errs.ErrUnavailable
	}
	return membershipID, subjectID, nil
}
