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
	case command.ReportEmailEffect:
		_, _, _, err := repository.authorizeEmailReport(ctx, tx, current, input)
		return err
	case command.ArchiveAssistantConversation:
		_, err := repository.authorizeAssistantArchive(ctx, tx, current, input)
		return err
	case command.ReconcileEmailEffect:
		_, err := repository.authorizeEmailReconciliation(ctx, tx, current, input)
		return err
	case command.ClaimExecution, command.RenewExecution, command.ReportExecutionProgress, command.CommitProviderCredentialRefresh,
		command.CompleteExecution,
		command.DelegateExecution, command.ProposeAssistantPlan, command.ProposeAssistantMetadata,
		command.ProposeRunMetadata, command.RecordRunToolCall, command.MaterializeOccurrence, command.FailScheduleOccurrence,
		command.CompleteSessionSnapshot, command.CompleteSessionRestore,
		command.CompleteSessionPVCDeletion, command.CompleteSessionObjectDeletion,
		command.FailSessionArchiveTask,
		command.CompleteInteractionDelivery:
		return nil
	case command.CompleteIntegrationInvocation:
		return repository.authorizeIntegrationCompletion(ctx, tx, current, input)
	case command.CompleteConnectionTest:
		return repository.authorizeIntegrationTestCompletion(ctx, tx, current, input)
	case command.AcceptInteractionMessage:
		payload, ok := input.Payload.(command.InteractionMessageInput)
		if !ok {
			return errs.ErrInvalid
		}
		human, err := repository.resolveInteractionIdentity(ctx, tx, current, payload)
		if err != nil {
			return err
		}
		if payload.Decision != "" {
			return repository.requireAccess(ctx, tx, human, "gate.resolve", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "OWNER_GATE", ResourceRef: payload.GateRef})
		}
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
	case command.InteractionIdentityInput:
		if current.authorityProjectID != "" {
			return "", resolvedAccessTarget{}, errs.ErrForbidden
		}
		connectionRef := payload.ConnectionRef
		if input.Kind == command.RevokeInteractionIdentity {
			identity, err := scanInteractionIdentity(tx.QueryRow(ctx, queryInteractionIdentityGet, current.organizationID, payload.IdentityRef))
			if err != nil {
				return "", resolvedAccessTarget{}, err
			}
			connectionRef = identity.ConnectionRef
		}
		if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}); err != nil {
			return "", resolvedAccessTarget{}, err
		}
		return "access.manage", organization, nil
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
	case command.AgentAvatarInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.AgentBindingInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.AgentRuntimeConfigurationInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.ConfigOverlayInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.RuntimeEnvironmentBindingInput:
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.RuntimeEnvironmentRebindInput:
		lookup := current
		lookup.role = "OWNER"
		environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, lookup, payload.EnvironmentRef)
		if err != nil {
			return "", resolvedAccessTarget{}, err
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", environment.ProjectRef, environment.ProjectRef)
	case command.RuntimeSecretRebindInput:
		for _, selection := range payload.Selections {
			if _, _, err := repository.environmentImpactTarget(ctx, tx, current, selection.EnvironmentRef, selection.SourceVersionRef); err != nil {
				return "", resolvedAccessTarget{}, err
			}
			for _, consumer := range selection.Consumers {
				if err := repository.requireAccess(ctx, tx, current, "agent.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: consumer.AgentRef}); err != nil {
					return "", resolvedAccessTarget{}, err
				}
			}
		}
		return repository.resolveCommandTarget(ctx, tx, current, "secret.rotate", "SECRET", payload.SecretRef, "")
	case command.RuntimeEnvironmentLifecycleInput:
		permission := "runtime.environment.disable"
		if input.Kind == command.DeleteRuntimeEnvironment {
			permission = "runtime.environment.delete"
		}
		return repository.resolveCommandTarget(ctx, tx, current, permission, "RUNTIME_ENVIRONMENT", payload.EnvironmentRef, "")
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
	case command.RuntimeEnvironmentDraftInput:
		projectRef := payload.ProjectRef
		if input.Kind != command.CreateRuntimeEnvironmentDraft {
			draft, err := scanEnvironmentDraft(tx.QueryRow(ctx, queryEnvironmentDraftGet, current.organizationID, payload.DraftRef))
			if err != nil {
				return "", resolvedAccessTarget{}, err
			}
			projectRef = draft.ProjectRef
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", projectRef, projectRef)
	case command.MemoryRecordInput:
		projectRef, agentRef := payload.ProjectRef, payload.AgentRef
		if input.Kind != command.CreateMemoryRecord {
			record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, payload.RecordRef))
			if err != nil {
				return "", resolvedAccessTarget{}, err
			}
			projectRef, agentRef = record.ProjectRef, record.AgentRef
		}
		if payload.Specification.SourceRunRef != "" {
			if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: payload.Specification.SourceRunRef}); err != nil {
				return "", resolvedAccessTarget{}, err
			}
		}
		if agentRef != "" {
			return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", agentRef, projectRef)
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", projectRef, projectRef)
	case command.AgentContextBindingInput:
		if err := repository.authorizeContextResource(ctx, tx, current, input, payload); err != nil {
			return "", resolvedAccessTarget{}, err
		}
		return repository.resolveCommandTarget(ctx, tx, current, "agent.manage", "AGENT", payload.AgentRef, "")
	case command.SkillBundleInput:
		projectRef := payload.ProjectRef
		if payload.BundleRef != "" {
			bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, payload.BundleRef))
			if err != nil {
				return "", resolvedAccessTarget{}, err
			}
			if projectRef != "" && projectRef != bundle.ProjectRef {
				return "", resolvedAccessTarget{}, errs.ErrForbidden
			}
			projectRef = bundle.ProjectRef
		}
		for _, file := range payload.Specification.Files {
			if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
				return "", resolvedAccessTarget{}, err
			}
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", projectRef, projectRef)
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
		permission := "run.cancel"
		if input.Kind == command.RetryRun {
			permission = "run.view"
		}
		return repository.resolveCommandTarget(ctx, tx, current, permission, "RUN", payload.RunRef, "")
	case command.GateResolutionInput:
		return repository.resolveCommandTarget(ctx, tx, current, "gate.resolve", "OWNER_GATE", payload.GateRef, "")
	case command.ArtifactBindingInput:
		return repository.resolveCommandTarget(ctx, tx, current, "artifact.bind", "ARTIFACT", payload.ArtifactRef, "")
	case command.ArtifactLifecycleInput:
		permission := ""
		switch input.Kind {
		case command.DeleteArtifact:
			permission = "artifact.delete"
		case command.RestoreArtifact:
			permission = "artifact.restore"
		case command.PurgeArtifact:
			permission = "artifact.purge"
		default:
			return "", resolvedAccessTarget{}, errs.ErrInvalid
		}
		return repository.resolveCommandTarget(ctx, tx, current, permission, "ARTIFACT", payload.ArtifactRef, "")
	case command.AttachmentSetDraftInput:
		projectRef := payload.ProjectRef
		if input.Kind != command.CreateAttachmentSetDraft {
			if err := tx.QueryRow(ctx, queryAttachmentSetsProjectByRef, current.organizationID, payload.AttachmentSetRef).Scan(&projectRef); err != nil {
				return "", resolvedAccessTarget{}, errs.ErrNotFound
			}
		}
		if projectRef == "" {
			return "organization.view", organization, nil
		}
		return repository.resolveCommandTarget(ctx, tx, current, "project.view", "PROJECT", projectRef, projectRef)
	case command.ScheduleInput:
		if input.Kind == command.CreateSchedule {
			return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", payload.ProjectRef, payload.ProjectRef)
		}
		return repository.resolveCommandTarget(ctx, tx, current, "schedule.manage", "SCHEDULE", payload.Ref, payload.ProjectRef)
	case command.ProviderAccountInput:
		if input.Kind == command.CreateProviderAccount {
			return "provider.account.manage", resolvedAccessTarget{scope: providerAccountCollectionTarget()}, nil
		}
		permission := "provider.account.manage"
		switch input.Kind {
		case command.StartProviderDeviceAuth, command.AuthorizeProviderAPIKey, command.RefreshProviderAuthorization:
			permission = "provider.account.authorize"
		case command.RevokeProviderAccount, command.DeleteProviderAccount:
			permission = "provider.account.revoke"
		}
		return repository.resolveCommandTarget(ctx, tx, current, permission, "PROVIDER_ACCOUNT", payload.AccountRef, "")
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
	case command.ManagedConfigurationInput:
		if input.Kind == command.CreateSystemSTTDraft {
			return "organization.manage", organization, nil
		}
		if input.Kind == command.CreateIntegrationDefinition && payload.ConfigurationRef == "" {
			return "organization.manage", organization, nil
		}
		if (input.Kind == command.CreatePromptTemplateDraft || input.Kind == command.CreateRoleImageRevisionDraft) && payload.ConfigurationRef == "" {
			return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", payload.ProjectRef, payload.ProjectRef)
		}
		if payload.ConfigurationRef != "" {
			var projectRef string
			if err := tx.QueryRow(ctx, queryManagedConfigurationAccessTarget, pgx.StrictNamedArgs{
				"organization_id": current.organizationID, "configuration_ref": payload.ConfigurationRef,
			}).Scan(&projectRef); errors.Is(err, pgx.ErrNoRows) {
				return "", resolvedAccessTarget{}, errs.ErrNotFound
			} else if err != nil {
				return "", resolvedAccessTarget{}, errs.ErrUnavailable
			}
			if projectRef != "" {
				return repository.resolveCommandTarget(ctx, tx, current, "project.manage", "PROJECT", projectRef, projectRef)
			}
		}
		// Точный set и его tenant повторно разрешаются под блокировкой внутри
		// owner-транзакции; непривилегированному actor ресурс не раскрывается.
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
