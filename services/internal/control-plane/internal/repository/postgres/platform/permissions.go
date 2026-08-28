package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) authorizeCommand(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	switch input.Kind {
	case command.ClaimExecution, command.RenewExecution, command.ReportExecutionProgress, command.CompleteExecution,
		command.DelegateExecution, command.ProposeAssistantPlan, command.ProposeAssistantMetadata,
		command.ProposeRunMetadata, command.RecordRunToolCall, command.MaterializeOccurrence,
		command.CompleteSessionSnapshot, command.CompleteSessionRestore,
		command.CompleteSessionPVCDeletion, command.CompleteSessionObjectDeletion,
		command.FailSessionArchiveTask,
		command.CompleteConnectionTest, command.CompleteIntegrationInvocation,
		command.CompleteInteractionDelivery, command.AcceptInteractionMessage:
		return nil
	case command.CreateAccessRole, command.CreateAccessRoleVersion, command.ArchiveAccessRole,
		command.CreateAccessBinding, command.ChangeAccessBinding, command.RevokeAccessBinding:
		return repository.requireAccess(ctx, tx, current, "access.manage", organizationTarget(current.organizationRef))
	}
	permission, target, err := repository.commandAccessTarget(ctx, tx, current, input)
	if err != nil {
		return err
	}
	if permission == "" {
		return errs.ErrNotFound
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return errs.ErrNotFound
	}
	return nil
}

func (repository *Repository) commandAccessTarget(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (string, resolvedAccessTarget, error) {
	organization := resolvedAccessTarget{scope: organizationTarget(current.organizationRef)}
	switch payload := input.Payload.(type) {
	case command.ProjectInput:
		if input.Kind == command.CreateProject {
			return "project.create", organization, nil
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", payload.Ref, payload.Ref)
	case command.PlatformMembershipInput:
		return "access.manage", organization, nil
	case command.MembershipInput:
		return repository.resolveCommandTarget(ctx, tx, current, "access.manage", "PROJECT", payload.ProjectRef, payload.ProjectRef)
	case command.AgentInput:
		if input.Kind == command.CreateAgent {
			return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", payload.ProjectRef, payload.ProjectRef)
		}
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.Ref, payload.ProjectRef)
	case command.AgentBindingInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.AgentRuntimeConfigurationInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.ConfigOverlayInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.RuntimeEnvironmentBindingInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.RuntimeEnvironmentInput:
		if input.Kind == command.CreateRuntimeEnvironment {
			return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", payload.ProjectRef, payload.ProjectRef)
		}
		if payload.Ref == "" {
			return "", resolvedAccessTarget{}, errs.ErrInvalid
		}
		lookupScope := current
		lookupScope.role = "OWNER"
		environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, lookupScope, payload.Ref)
		if err != nil {
			return "", resolvedAccessTarget{}, err
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", environment.ProjectRef, environment.ProjectRef)
	case command.WorkflowInput:
		if input.Kind == command.CreateWorkflow {
			return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", payload.ProjectRef, payload.ProjectRef)
		}
		return repository.resolveCommandTarget(ctx, tx, current, "workflow.manage", "WORKFLOW", payload.Ref, payload.ProjectRef)
	case command.LaunchRunInput:
		permission := "agent.launch"
		if payload.Target.Type == "WORKFLOW" {
			permission = "workflow.launch"
		} else if payload.Target.Type != "AGENT" {
			return "", resolvedAccessTarget{}, errs.ErrInvalid
		}
		return repository.resolveCommandTarget(ctx, tx, current, permission, payload.Target.Type, payload.Target.Ref, payload.ProjectRef)
	case command.SessionTurnInput:
		if payload.RunRef == "" {
			return "organization.view", organization, nil
		}
		return repository.resolveCommandTarget(ctx, tx, current, "run.view", "RUN", payload.RunRef, "")
	case command.RunCommandInput:
		permission := "run.cancel.any"
		if input.Kind == command.RetryRun {
			permission = "run.view"
		}
		return repository.resolveCommandTarget(ctx, tx, current, permission, "RUN", payload.RunRef, "")
	case command.GateResolutionInput:
		return repository.resolveCommandTarget(ctx, tx, current, "gate.resolve", "OWNER_GATE", payload.GateRef, "")
	case command.ArtifactBindingInput:
		return repository.resolveCommandTarget(ctx, tx, current, "artifact.manage", "ARTIFACT", payload.ArtifactRef, "")
	case command.ScheduleInput:
		if input.Kind == command.CreateSchedule {
			return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", payload.ProjectRef, payload.ProjectRef)
		}
		return repository.resolveCommandTarget(ctx, tx, current, "schedule.manage", "SCHEDULE", payload.Ref, payload.ProjectRef)
	case command.ConnectionInput:
		if payload.Ref == "" {
			return "organization.manage", organization, nil
		}
		return repository.resolveCommandTarget(ctx, tx, current, "integration.manage", "INTEGRATION", payload.Ref, "")
	case command.IntegrationGrantInput:
		if payload.AgentRef != "" {
			return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
		}
		return repository.resolveCommandTarget(ctx, tx, current, "workflow.manage", "WORKFLOW", payload.WorkflowRef, "")
	case command.AssistantConversationInput:
		if payload.ProjectRef == "" {
			return "organization.view", organization, nil
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.view", "PROJECT", payload.ProjectRef, payload.ProjectRef)
	case command.AssistantTurnInput, command.AssistantConversationTitleInput,
		command.AssistantPlanInput, command.AssistantPlanDraftInput, command.AssistantInstructionsInput:
		return "organization.manage", organization, nil
	default:
		if input.Kind == command.CompleteOnboarding {
			return "organization.manage", organization, nil
		}
		return "", resolvedAccessTarget{}, nil
	}
}

func (repository *Repository) resolveCommandTarget(ctx context.Context, tx pgx.Tx, current scope, permission, resourceKind, resourceRef, projectRef string) (string, resolvedAccessTarget, error) {
	resolved, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
		ProjectRef: projectRef, ResourceKind: resourceKind, ResourceRef: resourceRef,
	})
	return permission, resolved, err
}

// Legacy query endpoints use this helper only for compatibility filtering.
// Policy decisions for new commands and access APIs do not use memberships.
func requireProjectPermission(ctx context.Context, tx pgx.Tx, current scope, projectID, permission string) error {
	var allowed bool
	if err := tx.QueryRow(ctx, queryPermissionsRequireprojectpermissionSelectMembershipsOrganizationIdProjectIdSubjectId,
		current.organizationID, projectID, current.actorID, permission).Scan(&allowed); err != nil {
		return errs.ErrUnavailable
	}
	if !allowed {
		return errs.ErrNotFound
	}
	return nil
}

func projectIDByResource(ctx context.Context, tx pgx.Tx, organizationID, table, ref string) (string, error) {
	queries := map[string]string{
		"projects":                 queryPermissionsProjectidbyresourceSelectProjectsOrganizationIdRef,
		"agents":                   queryPermissionsProjectidbyresourceSelectAgentsOrganizationIdRef,
		"workflows":                queryPermissionsProjectidbyresourceSelectWorkflowsOrganizationIdRef,
		"sessions":                 queryPermissionsProjectidbyresourceSelectSessionsOrganizationIdRef,
		"runs":                     queryPermissionsProjectidbyresourceSelectRunsOrganizationIdRef,
		"owner_gates":              queryPermissionsProjectidbyresourceSelectOwnerGatesOrganizationIdRef,
		"artifacts":                queryPermissionsProjectidbyresourceSelectArtifactsOrganizationIdRef,
		"schedules":                queryPermissionsProjectidbyresourceSelectSchedulesOrganizationIdRef,
		"assistant_conversations":  queryPermissionsProjectidbyresourceSelectAssistantConversationsOrganizationIdRef,
		"runtime_environment_sets": queryPermissionsProjectidbyresourceSelectRuntimeEnvironmentSetsOrganizationIdRef,
	}
	query := queries[table]
	if query == "" {
		return "", errs.ErrInvalid
	}
	var projectID string
	if err := tx.QueryRow(ctx, query, organizationID, ref).Scan(&projectID); errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrNotFound
	} else if err != nil {
		return "", errs.ErrUnavailable
	}
	return projectID, nil
}
