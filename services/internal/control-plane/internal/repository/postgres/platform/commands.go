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

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type commandOutcome struct {
	result                                                                   command.Result
	projectID, projectRef, resourceKind, resourceRef, summary, platformEvent string
	platformAggregateVersion                                                 int64
	platformState                                                            string
}

const defaultAgentRunConcurrency = 8

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
		return result, nil
	}
	if err := repository.authorizeCommand(ctx, tx, scope, input); err != nil {
		return command.Result{}, err
	}
	var storedDigest string
	var storedPayload []byte
	err = tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation, scope.organizationID, scope.actorID, input.Mutation.Operation, input.Mutation.IdempotencyKey).Scan(&storedDigest, &storedPayload)
	if err == nil {
		if storedDigest != input.Mutation.IntentDigest {
			return command.Result{}, errs.ErrIdempotencyReuse
		}
		var result command.Result
		if json.Unmarshal(storedPayload, &result) != nil {
			return command.Result{}, errs.ErrConflict
		}
		if exposesActorActions(input.Principal.CallerWorkload) {
			if err := repository.applyResultActionPermissions(ctx, tx, scope, &result, ""); err != nil {
				return command.Result{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return command.Result{}, errs.ErrConflict
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return command.Result{}, fmt.Errorf("read idempotency receipt: %w", errs.ErrUnavailable)
	}
	outcome, err := repository.applyCommand(ctx, tx, scope, input)
	if err != nil {
		return command.Result{}, err
	}
	// Пустой опрос runtime является наблюдением, а не устойчивым доменным действием.
	// Receipt и аудит для него превращали бы исправный простой в постоянную запись.
	if input.Kind == command.ClaimExecution && len(outcome.result.RuntimeItems) == 0 {
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
	encoded, err := json.Marshal(outcome.result)
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
	return outcome.result, nil
}

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
	if input.Mutation.ExpectedVersion == nil {
		return command.Result{}, false, nil
	}
	var storedDigest string
	var storedPayload []byte
	err := tx.QueryRow(ctx, queryCommandsExecuteSelectIdempotencyReceiptsOrganizationIdActorIdOperation,
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
	return result, true, nil
}

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

func exposesActorActions(workload string) bool {
	return workload == "control-api-gateway"
}

func (repository *Repository) applyCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.CompleteOnboarding:
		return repository.completeOnboarding(ctx, tx, scope)
	case command.CreateProject:
		return repository.createProject(ctx, tx, scope, input.Payload)
	case command.UpdateProject:
		return repository.updateProject(ctx, tx, scope, input.Mutation, input.Payload)
	case command.AddPlatformMembership, command.ChangePlatformMembership, command.RemovePlatformMembership:
		return repository.changePlatformMembership(ctx, tx, scope, input)
	case command.AddMembership, command.ChangeMembership, command.RemoveMembership:
		return repository.changeMembership(ctx, tx, scope, input)
	case command.CreateAgent:
		return repository.createAgent(ctx, tx, scope, input.Payload)
	case command.UpdateAgent, command.SetAgentEnabled, command.ArchiveAgent:
		return repository.changeAgent(ctx, tx, scope, input)
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
		command.RefreshProviderAuthorization, command.RevokeProviderAccount, command.SetProviderAccountEnabled:
		return repository.changeProviderAccount(ctx, tx, scope, input)
	case command.CreateConnection, command.UpdateConnection, command.DeleteConnection, command.ConfigureConnectionCredential,
		command.TestConnection, command.SetConnectionEnabled, command.ChangeIntegrationGrant:
		return repository.changeConnection(ctx, tx, scope, input)
	case command.CreateAssistantConversation, command.UpdateAssistantConversation, command.AddAssistantTurn,
		command.UpdateAssistantPlan, command.ValidateAssistantPlan, command.ApplyAssistantPlan, command.RejectAssistantPlan,
		command.UpdateAssistantInstructions, command.RecoverAssistant:
		return repository.changeAssistant(ctx, tx, scope, input)
	case command.ClaimExecution, command.RenewExecution, command.ReportExecutionProgress, command.CommitProviderCredentialRefresh,
		command.CompleteExecution,
		command.DelegateExecution, command.ProposeAssistantPlan, command.ProposeAssistantMetadata,
		command.ProposeRunMetadata, command.RecordRunToolCall:
		return repository.changeExecution(ctx, tx, scope, input)
	case command.CompleteSessionSnapshot, command.CompleteSessionRestore,
		command.CompleteSessionPVCDeletion, command.CompleteSessionObjectDeletion,
		command.FailSessionArchiveTask:
		return repository.changeSessionArchive(ctx, tx, scope, input)
	case command.MaterializeOccurrence:
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
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

func (repository *Repository) completeOnboarding(ctx context.Context, tx pgx.Tx, scope scope) (commandOutcome, error) {
	if _, err := tx.Exec(ctx, queryCommandsCompleteonboardingUpdateInstallationOnboardingCompletedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{CreatedRefs: []string{scope.organizationRef}}, resourceKind: "INSTALLATION", resourceRef: scope.organizationRef, summary: "i18n:ONBOARDING_COMPLETED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}

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
		var membershipID, subjectID string
		err := tx.QueryRow(ctx, queryProjectMembershipResolveForUpdate, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"project_id":      projectID,
			"membership_ref":  payload.MembershipRef,
		}).Scan(
			&membershipID, &subjectID, &item.Ref, &item.User.Ref, &item.User.DisplayName,
			&item.User.EmailMasked, &item.User.Active, &item.Role, &item.Permissions,
			&item.Active, &item.Version,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if item.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if subjectID == scope.actorID && scope.role != "OWNER" && scope.role != "ADMINISTRATOR" {
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
		var membershipID, subjectID string
		err := tx.QueryRow(ctx, queryProjectMembershipResolveForUpdate, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"project_id":      projectID,
			"membership_ref":  payload.MembershipRef,
		}).Scan(
			&membershipID, &subjectID, &item.Ref, &item.User.Ref, &item.User.DisplayName,
			&item.User.EmailMasked, &item.User.Active, &item.Role, &item.Permissions,
			&item.Active, &item.Version,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
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
		err := tx.QueryRow(ctx, queryPlatformMembershipResolveForUpdate, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"membership_ref":  payload.MembershipRef,
		}).Scan(
			&membershipID, &subjectID, &item.Ref, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked,
			&item.User.Active, &item.Role, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if item.Version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if scope.role != "OWNER" && item.Role == "OWNER" {
			return commandOutcome{}, errs.ErrForbidden
		}
		if subjectID == scope.actorID && !payload.Active {
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
		err := tx.QueryRow(ctx, queryPlatformMembershipResolveForUpdate, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"membership_ref":  payload.MembershipRef,
		}).Scan(
			&membershipID, &subjectID, &item.Ref, &item.User.Ref, &item.User.DisplayName, &item.User.EmailMasked,
			&item.User.Active, &item.Role, &item.Active, &item.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		}
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
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

func validPlatformRole(role string) bool {
	switch role {
	case "OWNER", "ADMINISTRATOR", "OPERATOR", "MEMBER", "AUDITOR":
		return true
	default:
		return false
	}
}

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

func (repository *Repository) createAgent(ctx context.Context, tx pgx.Tx, scope scope, payload any) (commandOutcome, error) {
	input, ok := payload.(command.AgentInput)
	if !ok || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Instructions) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	projectID := mustProjectID(ctx, tx, scope.organizationID, input.ProjectRef)
	if projectID == "" {
		return commandOutcome{}, errs.ErrNotFound
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
	err = tx.QueryRow(ctx, queryCommandsCreateagentInsertAgentsRefProjectIdPurpose, ref, scope.organizationID, projectID, roleID, strings.TrimSpace(input.Name), strings.TrimSpace(input.Purpose), strings.TrimSpace(input.RoleDescription), strings.TrimSpace(input.AvatarURL), runtimeKey, scope.actorID).Scan(&agentID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
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
	item.ProjectRef = input.ProjectRef
	item.RoleDefinitionRef = roleRef
	item.RoleDefinitionName = roleName
	item.RuntimeKey = runtimeKey
	item.RuntimeName = runtime.Name
	item.Provider = runtime.Provider
	item.Model = runtime.Model
	item.RuntimeRevision = runtime.RuntimeRevision
	item.PublishedInstructions = &entity.InstructionVersion{Ref: instructionRef, VersionNumber: 1, State: "PUBLISHED", Content: input.Instructions, Digest: hex.EncodeToString(digest[:]), CreatedAt: publishedAt, PublishedAt: &publishedAt}
	item.NextActions = agentActions(item, true, true)
	return commandOutcome{result: command.Result{Agent: &item}, projectID: projectID, projectRef: input.ProjectRef, resourceKind: "AGENT", resourceRef: ref, summary: "i18n:AGENT_CREATED_READY", platformEvent: "AGENT_CHANGED"}, nil
}

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

func mapWriteError(err error) error {
	if err == nil {
		return nil
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
func mustProjectID(ctx context.Context, tx pgx.Tx, organizationID, ref string) string {
	var id string
	if tx.QueryRow(ctx, queryCommandsMustprojectidSelectProjectsOrganizationIdRefLifecycle, organizationID, ref).Scan(&id) != nil {
		return ""
	}
	return id
}

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
		err := tx.QueryRow(ctx, queryCommandsChangeagentArchiveAgent, scope.organizationID, payload.Ref, *input.Mutation.ExpectedVersion).Scan(&projectID, &item.Ref, &item.Name, &item.Purpose, &item.RoleDescription, &item.AvatarURL, &item.State, &item.Enabled, &item.Version, &item.CreatedAt, &item.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrConflict
		}
		if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	if err := tx.QueryRow(ctx, queryCommandsChangeagentSelectAgentsRef, item.Ref).Scan(&item.ProjectRef, &item.RoleDefinitionRef, &item.RoleDefinitionName, &item.RuntimeKey, &item.RuntimeName, &item.Provider, &item.Model, &item.RuntimeRevision, &item.Capabilities, &item.KnowledgeArtifactRefs); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if input.Kind == command.ArchiveAgent || input.Kind == command.SetAgentEnabled && !payload.Enabled {
		if err := repository.suspendTargetSchedules(ctx, tx, scope, projectID, item.ProjectRef, "AGENT", item.Ref); err != nil {
			return commandOutcome{}, err
		}
	}
	item.NextActions = agentActions(item, true, true)
	return commandOutcome{result: command.Result{Agent: &item}, projectID: projectID, projectRef: item.ProjectRef, resourceKind: "AGENT", resourceRef: item.Ref, summary: "i18n:AGENT_UPDATED", platformEvent: "AGENT_CHANGED"}, nil
}

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
		if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateInstructionVersionsStateValidationProblems, ref, state, asJSON(problems)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.PublishInstructions:
		tag, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateInstructionVersionsStatePublishedAt, agentID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	case command.RollbackInstructions:
		var content string
		if err := tx.QueryRow(ctx, queryCommandsChangeinstructionsSelectInstructionVersionsAgentIdRefState, agentID, payload.Instructions).Scan(&content); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
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
	}
	if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt, agentID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	agent := entity.Agent{Ref: payload.Ref, ProjectRef: projectRef, Version: agentVersion + 1}
	return commandOutcome{result: command.Result{Agent: &agent}, projectID: projectID, projectRef: projectRef, resourceKind: "INSTRUCTIONS", resourceRef: payload.Ref, summary: "i18n:AGENT_INSTRUCTIONS_UPDATED", platformEvent: "INSTRUCTIONS_PUBLISHED"}, nil
}

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
		if !validCapabilityKey(payload.BindingRef) {
			return commandOutcome{}, errs.ErrInvalid
		}
		var capabilityKey string
		if err := tx.QueryRow(ctx, queryCommandsChangeagentbindingSelectEnabledCapability, payload.BindingRef).Scan(&capabilityKey); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
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
		&agent.State, &agent.Enabled, &agent.Version, &agent.RuntimeKey,
		&agent.RuntimeName, &agent.Provider, &agent.Model, &agent.RuntimeRevision,
		&agent.Capabilities, &agent.KnowledgeArtifactRefs, &agent.CreatedAt, &agent.UpdatedAt,
	); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	agent.NextActions = agentActions(agent, true, true)
	return commandOutcome{result: command.Result{Agent: &agent}, projectID: projectID, projectRef: projectRef, resourceKind: "AGENT", resourceRef: payload.AgentRef, summary: "i18n:AGENT_PERMISSIONS_UPDATED", platformEvent: "AGENT_CHANGED"}, nil
}

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
		if payload.Draft == nil || !validWorkflowVersion(*payload.Draft) {
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

func validWorkflowVersion(version entity.WorkflowVersion) bool {
	if strings.TrimSpace(version.Name) == "" || len(version.Name) > 160 || len(version.Purpose) > 2000 || strings.TrimSpace(version.CoordinatorAgentRef) == "" || version.Concurrency < 1 || version.Concurrency > 100 || version.TimeoutSeconds < 1 || version.TimeoutSeconds > 7*24*60*60 || len(version.Inputs) > 100 || len(version.Steps) < 1 || len(version.Steps) > 200 || !validWorkflowInputFields(version.Inputs) {
		return false
	}
	knownSteps := make(map[string]struct{}, len(version.Steps))
	totalInstructions := len(version.Instructions) + len(version.CompletionCriteria)
	for index, step := range version.Steps {
		if step.Key == "" || len(step.Key) > 96 || step.Position != int32(index+1) || strings.TrimSpace(step.Name) == "" || len(step.Name) > 160 || strings.TrimSpace(step.AgentRef) == "" || strings.TrimSpace(step.Instructions) == "" || len(step.Instructions) > 1000 || step.TimeoutSeconds < 1 || step.TimeoutSeconds > 24*60*60 || step.ParallelGroup < 0 || step.ParallelGroup > 50 || len(step.ExpectedResult) > 1000 || len(step.GateDecisions) > 4 || len(step.RequiredCapabilityKeys) > 50 {
			return false
		}
		if _, duplicate := knownSteps[step.Key]; duplicate {
			return false
		}
		for _, dependency := range step.DependsOn {
			if _, exists := knownSteps[dependency]; !exists {
				return false
			}
		}
		for _, decision := range step.GateDecisions {
			if !contains([]string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}, decision) {
				return false
			}
		}
		if step.HumanGateAfter && len(step.GateDecisions) == 0 {
			return false
		}
		for _, capability := range step.RequiredCapabilityKeys {
			if !validCapabilityKey(capability) {
				return false
			}
		}
		knownSteps[step.Key] = struct{}{}
		totalInstructions += len(step.Name) + len(step.Instructions) + len(step.ExpectedResult)
	}
	return totalInstructions <= 64<<10
}

func validWorkflowInputFields(fields []entity.WorkflowInputField) bool {
	known := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		if !validWorkflowInputKey(field.Key) || strings.TrimSpace(field.Label) == "" || len(field.Label) > 160 || len(field.Help) > 500 || !contains([]string{"TEXT", "LONG_TEXT", "NUMBER", "BOOLEAN", "DATE", "SELECT"}, field.Type) || len(field.Options) > 50 {
			return false
		}
		if _, duplicate := known[field.Key]; duplicate {
			return false
		}
		known[field.Key] = struct{}{}
		options := make(map[string]struct{}, len(field.Options))
		for _, option := range field.Options {
			if strings.TrimSpace(option) == "" || len(option) > 160 {
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

func emptyWorkflowInputValue(value any) bool {
	text, isText := value.(string)
	return value == nil || isText && strings.TrimSpace(text) == ""
}

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

func validBoundedRunInput(input map[string]any) bool {
	if len(input) > 100 {
		return false
	}
	raw, err := json.Marshal(input)
	return err == nil && len(raw) <= 64<<10
}

func workflowRequiresDelegation(version entity.WorkflowVersion) bool {
	for _, step := range version.Steps {
		if step.AgentRef != version.CoordinatorAgentRef {
			return true
		}
	}
	return false
}

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

func (repository *Repository) launchRun(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	return repository.launchRunWithAttachmentPolicy(ctx, tx, scope, input, false)
}

func (repository *Repository) launchRunWithAttachmentPolicy(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, reuseAttachmentSnapshot bool) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LaunchRunInput)
	if !ok || payload.ProjectRef == "" || strings.TrimSpace(payload.Task) == "" || len(payload.Task) > 32768 || len(payload.Title) > 240 || payload.Target.Ref == "" || !validBoundedRunInput(payload.Input) {
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
		allowed, err := repository.agentsHaveCapabilities(ctx, tx, scope.organizationID, projectID, targetAgentRefs, []string{runtimecontract.ArtifactCapability})
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
	} else if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectSessionsOrganizationIdProjectIdRef, scope.organizationID, projectID, sessionRef, payload.Target.Type, payload.Target.Ref).Scan(&sessionID); err != nil {
		return commandOutcome{}, fmt.Errorf("resolve continuation session: %w", errs.ErrConflict)
	}
	runRef, _ := newRef("run")
	title := strings.TrimSpace(payload.Title)
	titleSource := strings.TrimSpace(payload.TitleSource)
	if title == "" {
		title = targetName + ": " + truncate(payload.Task, 120)
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

func workflowCoordinatorTask(task, versionRef, versionDigest string, version entity.WorkflowVersion) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(task))
	builder.WriteString("\n\n# Workflow coordination contract\n")
	builder.WriteString("Execute the immutable workflow version ")
	builder.WriteString(versionRef)
	builder.WriteString(" (sha256:")
	builder.WriteString(versionDigest)
	builder.WriteString("). Use delegate_agent exactly once for every step assigned to another agent. Execute coordinator-owned steps locally. End this turn after all required delegations are accepted; child results arrive in a later callback turn.\n")
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
		if step.AgentRef == version.CoordinatorAgentRef {
			builder.WriteString(" (execute locally)")
		} else {
			builder.WriteString(" (delegate)")
		}
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

func workflowHasHumanGate(version entity.WorkflowVersion) bool {
	for _, step := range version.Steps {
		if step.HumanGateAfter {
			return true
		}
	}
	return false
}

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

func truncate(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	return string(runes[:maximum]) + "…"
}

func (repository *Repository) emitPlatformEvent(ctx context.Context, tx pgx.Tx, scope scope, eventName, projectRef, aggregateRef, summary string) error {
	return repository.emitPlatformEventSnapshot(ctx, tx, scope, eventName, projectRef, aggregateRef, summary, 1, "")
}

func (repository *Repository) emitCommandOutcomePlatformEvent(ctx context.Context, tx pgx.Tx, scope scope, outcome commandOutcome) error {
	version := outcome.platformAggregateVersion
	if version < 1 {
		version = 1
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, scope, outcome.platformEvent, outcome.projectRef, outcome.resourceRef, outcome.summary, version, outcome.platformState)
}

func (repository *Repository) emitPlatformEventSnapshot(ctx context.Context, tx pgx.Tx, scope scope, eventName, projectRef, aggregateRef, summary string, aggregateVersion int64, state string) error {
	var sequence int64
	if err := tx.QueryRow(ctx, queryCommandsEmitplatformeventUpdateInstallationPlatformSequence).Scan(&sequence); err != nil {
		return errs.ErrUnavailable
	}
	eventID := uuid.New()
	data := map[string]any{"kind": platformEventKind(eventName), "safeSummary": summary}
	if state != "" {
		data["state"] = state
	}
	payload := map[string]any{"eventId": eventID.String(), "eventName": eventName, "eventVersion": 1, "occurredAt": time.Now().UTC(), "organizationRef": scope.organizationRef, "aggregateRef": aggregateRef, "aggregateVersion": aggregateVersion, "sequence": sequence, "correlationRef": scope.correlationRef, "data": data}
	if projectRef != "" {
		payload["projectRef"] = projectRef
	}
	subject := "control_plane.platform." + scope.organizationRef + ".events"
	if _, err := tx.Exec(ctx, queryCommandsEmitplatformeventInsertOutboxEventsEventIdOrderingKeyPayload, eventID, subject, "platform:"+scope.organizationRef, sequence, asJSON(payload)); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func (repository *Repository) emitRunEvent(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef, summary, runState, nodeState string) (entity.RunEvent, error) {
	return repository.emitRunEventWithIncident(ctx, tx, scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef, nil, summary, runState, nodeState)
}

func (repository *Repository) emitRunEventWithIncident(ctx context.Context, tx pgx.Tx, scope scope, projectID, rootRunID, aggregateRef, eventType, nodeRef, edgeRef, gateRef, artifactRef string, incident *entity.Incident, summary, runState, nodeState string) (entity.RunEvent, error) {
	var sequence, version int64
	var rootRef, projectRef string
	var projectValue any
	if err := tx.QueryRow(ctx, queryCommandsEmitruneventUpdateRunsEventSequenceGraphRevisionUpdatedAt, rootRunID).Scan(&rootRef, &sequence, &version); err != nil {
		return entity.RunEvent{}, errs.ErrUnavailable
	}
	if projectID != "" {
		if err := tx.QueryRow(ctx, queryCommandsEmitruneventSelectProjectsId, projectID).Scan(&projectRef); err != nil {
			return entity.RunEvent{}, errs.ErrUnavailable
		}
		projectValue = projectID
	}
	ref, _ := newRef("evt")
	eventID := uuid.New()
	delta, err := repository.readRunEventDelta(ctx, tx, scope.organizationID, rootRunID, nodeRef, edgeRef, gateRef, artifactRef)
	if err != nil {
		return entity.RunEvent{}, err
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
	event := entity.RunEvent{Ref: ref, RunRef: rootRef, Sequence: sequence, GraphRevision: delta.Run.GraphRevision, Type: eventType, NodeRef: nodeRef, EdgeRef: edgeRef, GateRef: gateRef, ArtifactRef: artifactRef, IncidentRef: incidentRef, Summary: safeSummary, RunState: runState, NodeState: nodeState, MessageKind: messageKind, Actor: actor, OccurredAt: time.Now().UTC(), Delta: delta}
	if _, err := tx.Exec(ctx, queryCommandsEmitruneventInsertRunEventsEventIdOrganizationIdRootRunId, eventID, ref, scope.organizationID, projectValue, rootRunID, aggregateRef, version, sequence, eventType, nodeRef, edgeRef, gateRef, artifactRef, safeSummary, runState, nodeState, asJSON(delta), scope.actorRef, event.OccurredAt, actor.Kind, actor.Ref, actor.Name, messageKind, nil); err != nil {
		return entity.RunEvent{}, errs.ErrUnavailable
	}
	data := map[string]any{"kind": eventKind(eventType), "runRef": rootRef, "safeSummary": safeSummary,
		"actor": map[string]string{"kind": actor.Kind, "ref": actor.Ref, "name": actor.Name}, "messageKind": messageKind}
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
		return entity.RunEvent{}, errs.ErrUnavailable
	}
	// Org-wide stream получает только безопасный invalidation-сигнал. Полная
	// delta остаётся в авторитетном run event store и защищённом run stream.
	if err := repository.emitPlatformEventSnapshot(ctx, tx, scope, "RUN_CHANGED", projectRef, rootRef, safeSummary, version, runState); err != nil {
		return entity.RunEvent{}, err
	}
	return event, nil
}

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

func boundedStrings(values []string, maximum int) []string {
	if len(values) > maximum {
		values = values[:maximum]
	}
	return append([]string(nil), values...)
}

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
	case "ROLE_IMAGE_RECIPE_CHANGED":
		return "ROLE_IMAGE_RECIPE"
	case "RUN_CHANGED":
		return "RUN"
	default:
		return "SYSTEM_ASSISTANT"
	}
}

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

func (repository *Repository) readRunGraphTx(ctx context.Context, tx pgx.Tx, scope scope, runRef string) (entity.Run, entity.RunGraph, error) {
	run, err := scanRun(tx.QueryRow(ctx, queryCommandsGetrunforgraphSelectRunsOrganizationIdRef, scope.organizationID, runRef), false)
	if err != nil {
		return entity.Run{}, entity.RunGraph{}, fmt.Errorf("read run snapshot: %w", err)
	}
	graph := entity.RunGraph{RunRef: run.RootRunRef, Revision: run.GraphRevision, Sequence: run.EventSequence}
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

func (repository *Repository) addSessionTurn(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SessionTurnInput)
	if !ok || payload.SessionRef == "" || strings.TrimSpace(payload.Task) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID, projectRef, targetType, targetRef string
	if err := tx.QueryRow(ctx, queryCommandsAddsessionturnSelectSessionsOrganizationIdRefState, scope.organizationID, payload.SessionRef).Scan(&projectID, &projectRef, &targetType, &targetRef); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	launch := command.LaunchRunInput{ProjectRef: projectRef, Title: "i18n:SESSION_CONTINUATION", Task: payload.Task, SessionRef: payload.SessionRef, Source: "CONTROL_CENTER", Target: entity.RunTarget{Type: targetType, Ref: targetRef}, AttachmentSetRef: payload.AttachmentSetRef, AttachmentPurpose: "SESSION_TURN"}
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = launch
	outcome, err := repository.launchRun(ctx, tx, scope, nested)
	if err != nil {
		return commandOutcome{}, err
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

func (repository *Repository) changeRun(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RunCommandInput)
	if !ok || payload.RunRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var runID, rootRunID, projectID, projectRef, state string
	var version int64
	var attempt int32
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRunsOrganizationIdRef, scope.organizationID, payload.RunRef).Scan(&runID, &rootRunID, &projectID, &projectRef, &state, &version, &attempt); err != nil {
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
		gateRows, err := tx.Query(ctx, queryCommandsChangerunUpdateOwnerGatesStateDecisionDecisionComment, rootRunID, scope.actorID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
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
		run, graph, err := repository.readRunGraphTx(ctx, tx, scope, payload.RunRef)
		if err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{Run: &run, Graph: &graph}, projectID: projectID, projectRef: projectRef, resourceKind: "RUN", resourceRef: payload.RunRef, summary: "i18n:RUN_CANCELLED"}, nil
	}
	if !contains([]string{"FAILED", "CANCELLED"}, state) {
		return commandOutcome{}, errs.ErrConflict
	}
	var targetType, targetRef, title, task, sessionRef, source string
	var raw []byte
	var attachmentSetRef, attachmentPurpose string
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRunsId, runID).Scan(&targetType, &targetRef, &title, &task, &sessionRef, &source, &raw, &attachmentSetRef, &attachmentPurpose); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var launchInput map[string]any
	_ = json.Unmarshal(raw, &launchInput)
	nested := input
	nested.Kind = command.LaunchRun
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: title, Task: task, SessionRef: sessionRef, Source: source, Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: launchInput, AttachmentSetRef: attachmentSetRef, AttachmentPurpose: attachmentPurpose}
	outcome, err := repository.launchRunWithAttachmentPolicy(ctx, tx, scope, nested, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var newRunID, newRootID, newRootNodeID, oldRootNodeID string
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRunsRef, outcome.result.Run.Ref).Scan(&newRunID, &newRootID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsChangerunUpdateRunsRetryOfRunIdAttempt, newRunID, runID, attempt+1); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectPreviousRootNode, rootRunID).Scan(&oldRootNodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := tx.QueryRow(ctx, queryCommandsChangerunSelectRetryRootNode, newRootID).Scan(&newRootNodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	edgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, queryCommandsChangerunInsertRunEdgesRefRootRunIdTargetNodeId, edgeRef, scope.organizationID, newRootID, oldRootNodeID, newRootNodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, newRootID, edgeRef, "EDGE_ADDED", "", edgeRef, "", "", "i18n:RUN_RETRY_CREATED", "QUEUED", ""); err != nil {
		return commandOutcome{}, err
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
	var gateID, nodeID, rootRunID, projectID, projectRef, gateNodeRef string
	var predecessorNodeID, predecessorNodeRef, predecessorRunID, sessionID, integrationInvocationID string
	var version int64
	var allowed []string
	err := tx.QueryRow(ctx, queryCommandsResolvegateSelectOwnerGatesOrganizationIdRefState, scope.organizationID, payload.GateRef).Scan(
		&gateID, &nodeID, &rootRunID, &projectID, &projectRef, &version, &allowed, &gateNodeRef,
		&predecessorNodeID, &predecessorNodeRef, &predecessorRunID, &sessionID,
		&integrationInvocationID,
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
		runState = "FAILED"
	} else if payload.Decision == "CANCEL" {
		nodeState = "CANCELLED"
		runState = "CANCELLED"
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
			runState = "SUCCEEDED"
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
	return commandOutcome{result: command.Result{Gate: &gate, Run: &run, Graph: &graph, Event: &event}, projectID: projectID, projectRef: projectRef, resourceKind: "OWNER_GATE", resourceRef: payload.GateRef, summary: "i18n:OWNER_GATE_RESOLVED"}, nil
}

func mustRunRef(ctx context.Context, tx pgx.Tx, id string) (string, error) {
	var ref string
	if err := tx.QueryRow(ctx, queryCommandsMustrunrefSelectRunsId, id).Scan(&ref); err != nil {
		return "", errs.ErrUnavailable
	}
	return ref, nil
}
