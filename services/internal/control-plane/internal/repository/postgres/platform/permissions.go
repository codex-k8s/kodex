package platform

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) authorizeCommand(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) error {
	if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
		return nil
	}
	switch input.Kind {
	case command.CompleteOnboarding, command.CreateProject,
		command.AddPlatformMembership, command.ChangePlatformMembership, command.RemovePlatformMembership,
		command.CreateConnection, command.TestConnection, command.SetConnectionEnabled, command.UpdateAssistantInstructions, command.RecoverAssistant:
		return errs.ErrForbidden
	case command.ClaimExecution, command.RenewExecution, command.ReportExecutionProgress, command.CompleteExecution,
		command.DelegateExecution, command.ProposeAssistantPlan, command.MaterializeOccurrence,
		command.CompleteConnectionTest, command.CompleteIntegrationInvocation,
		command.CompleteInteractionDelivery, command.AcceptInteractionMessage:
		return nil
	}
	projectID, permission, err := repository.commandProjectPermission(ctx, tx, scope, input)
	if err != nil {
		return err
	}
	if projectID == "" {
		return nil
	}
	var permitted bool
	err = tx.QueryRow(ctx, queryPermissionsAuthorizecommandSelectMembershipsOrganizationIdProjectIdSubjectId, scope.organizationID, projectID, scope.actorID, permission).Scan(&permitted)
	if err != nil {
		return errs.ErrUnavailable
	}
	if !permitted {
		return errs.ErrNotFound
	}
	return nil
}

func (repository *Repository) commandProjectPermission(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (string, string, error) {
	var ref, table, permission string
	switch payload := input.Payload.(type) {
	case command.ProjectInput:
		ref, table, permission = payload.Ref, "projects", "MANAGE"
	case command.MembershipInput:
		ref, table, permission = payload.ProjectRef, "projects", "MANAGE_MEMBERS"
	case command.AgentInput:
		if payload.ProjectRef != "" {
			ref, table = payload.ProjectRef, "projects"
		} else {
			ref, table = payload.Ref, "agents"
		}
		permission = "MANAGE_AGENTS"
	case command.AgentBindingInput:
		ref, table, permission = payload.AgentRef, "agents", "MANAGE_AGENTS"
	case command.WorkflowInput:
		if payload.ProjectRef != "" {
			ref, table = payload.ProjectRef, "projects"
		} else {
			ref, table = payload.Ref, "workflows"
		}
		permission = "MANAGE_WORKFLOWS"
	case command.LaunchRunInput:
		ref, table, permission = payload.ProjectRef, "projects", "LAUNCH_RUNS"
	case command.SessionTurnInput:
		ref, table, permission = payload.SessionRef, "sessions", "LAUNCH_RUNS"
	case command.RunCommandInput:
		ref, table, permission = payload.RunRef, "runs", "CANCEL_RUNS"
	case command.GateResolutionInput:
		ref, table, permission = payload.GateRef, "owner_gates", "RESOLVE_GATES"
	case command.ArtifactBindingInput:
		ref, table, permission = payload.ArtifactRef, "artifacts", "MANAGE_ARTIFACTS"
	case command.ScheduleInput:
		if payload.ProjectRef != "" {
			ref, table = payload.ProjectRef, "projects"
		} else {
			ref, table = payload.Ref, "schedules"
		}
		permission = "MANAGE_SCHEDULES"
	case command.IntegrationGrantInput:
		if payload.AgentRef != "" {
			ref, table = payload.AgentRef, "agents"
		} else {
			ref, table = payload.WorkflowRef, "workflows"
		}
		permission = "MANAGE_INTEGRATIONS"
	case command.AssistantConversationInput:
		if payload.ProjectRef == "" {
			return "", "", nil
		}
		ref, table, permission = payload.ProjectRef, "projects", "VIEW"
	case command.AssistantTurnInput:
		ref, table, permission = payload.ConversationRef, "assistant_conversations", "VIEW"
	case command.AssistantPlanInput:
		return "", "", nil
	default:
		return "", "", nil
	}
	if ref == "" {
		return "", "", errs.ErrInvalid
	}
	projectID, err := projectIDByResource(ctx, tx, scope.organizationID, table, ref)
	if err != nil {
		return "", "", err
	}
	return projectID, permission, nil
}

func requireProjectPermission(ctx context.Context, tx pgx.Tx, scope scope, projectID, permission string) error {
	if scope.role == "OWNER" || scope.role == "ADMINISTRATOR" {
		return nil
	}
	var allowed bool
	if err := tx.QueryRow(ctx, queryPermissionsRequireprojectpermissionSelectMembershipsOrganizationIdProjectIdSubjectId, scope.organizationID, projectID, scope.actorID, permission).Scan(&allowed); err != nil {
		return errs.ErrUnavailable
	}
	if !allowed {
		return errs.ErrNotFound
	}
	return nil
}

func projectIDByResource(ctx context.Context, tx pgx.Tx, organizationID, table, ref string) (string, error) {
	queries := map[string]string{
		"projects":                queryPermissionsProjectidbyresourceSelectProjectsOrganizationIdRef,
		"agents":                  queryPermissionsProjectidbyresourceSelectAgentsOrganizationIdRef,
		"workflows":               queryPermissionsProjectidbyresourceSelectWorkflowsOrganizationIdRef,
		"sessions":                queryPermissionsProjectidbyresourceSelectSessionsOrganizationIdRef,
		"runs":                    queryPermissionsProjectidbyresourceSelectRunsOrganizationIdRef,
		"owner_gates":             queryPermissionsProjectidbyresourceSelectOwnerGatesOrganizationIdRef,
		"artifacts":               queryPermissionsProjectidbyresourceSelectArtifactsOrganizationIdRef,
		"schedules":               queryPermissionsProjectidbyresourceSelectSchedulesOrganizationIdRef,
		"assistant_conversations": queryPermissionsProjectidbyresourceSelectAssistantConversationsOrganizationIdRef,
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
