package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/project_assistant__get.sql
var queryProjectAssistantGet string

//go:embed sql/project_assistant__lock_project.sql
var queryProjectAssistantLockProject string

//go:embed sql/project_assistant__create.sql
var queryProjectAssistantCreate string

//go:embed sql/project_assistant__resolve_candidate.sql
var queryProjectAssistantResolveCandidate string

//go:embed sql/project_assistant__resolve_conversation.sql
var queryProjectAssistantResolveConversation string

//go:embed sql/project_assistant__plan_conversation.sql
var queryProjectAssistantPlanConversation string

//go:embed sql/project_assistant__protected_agent.sql
var queryProjectAssistantProtectedAgent string

//go:embed sql/project_assistant__context_creation_allowed.sql
var queryProjectAssistantContextCreationAllowed string

func (repository *Repository) rejectAssistantAgentArchive(ctx context.Context, tx pgx.Tx, current scope, agentRef string) error {
	var protected bool
	if err := tx.QueryRow(ctx, queryProjectAssistantProtectedAgent, current.organizationID, agentRef).Scan(&protected); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	} else if err != nil {
		return errs.ErrUnavailable
	}
	if protected {
		return errs.ErrConflict
	}
	return nil
}

func (repository *Repository) assistantCommandTarget(ctx context.Context, tx pgx.Tx, current scope, conversationRef, planRef, permission string) (string, resolvedAccessTarget, error) {
	if conversationRef == "" && planRef != "" {
		if err := tx.QueryRow(ctx, queryProjectAssistantPlanConversation, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "actor_id": current.actorID, "plan_ref": planRef,
		}).Scan(&conversationRef); errors.Is(err, pgx.ErrNoRows) {
			return "", resolvedAccessTarget{}, errs.ErrNotFound
		} else if err != nil {
			return "", resolvedAccessTarget{}, errs.ErrUnavailable
		}
	}
	assistant, err := repository.conversationAssistantTx(ctx, tx, current, conversationRef)
	if err != nil {
		return "", resolvedAccessTarget{}, err
	}
	if assistant.Scope == "SYSTEM" {
		return "organization.manage", resolvedAccessTarget{scope: organizationTarget(current.organizationRef)}, nil
	}
	if permission == "project.view" || permission == "project.manage" {
		return repository.resolveCommandTarget(ctx, tx, current, permission, "PROJECT", assistant.ProjectRef, assistant.ProjectRef)
	}
	return repository.resolveCommandTarget(ctx, tx, current, permission, "AGENT", assistant.Ref, assistant.ProjectRef)
}

type conversationAssistant struct {
	ID, Ref, Scope, ProfileRef, ProjectRef string
}

func (repository *Repository) assistantForConversationCreation(ctx context.Context, tx pgx.Tx, current scope, input command.AssistantConversationInput) (conversationAssistant, error) {
	var assistant conversationAssistant
	if input.AssistantScope != "SYSTEM" && input.AssistantScope != "PROJECT" || input.AssistantScope == "PROJECT" && input.ProjectRef == "" {
		return assistant, errs.ErrInvalid
	}
	err := tx.QueryRow(ctx, queryProjectAssistantResolveCandidate, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": input.ProjectRef,
		"assistant_scope": input.AssistantScope, "authority_project": current.authorityProjectID,
	}).Scan(&assistant.ID, &assistant.Ref, &assistant.Scope, &assistant.ProfileRef, &assistant.ProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return assistant, errs.ErrNotFound
	}
	if err != nil {
		return assistant, errs.ErrUnavailable
	}
	return assistant, nil
}

func (repository *Repository) conversationAssistantTx(ctx context.Context, tx pgx.Tx, current scope, conversationRef string) (conversationAssistant, error) {
	var assistant conversationAssistant
	err := tx.QueryRow(ctx, queryProjectAssistantResolveConversation, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"conversation_ref": conversationRef, "authority_project": current.authorityProjectID,
	}).Scan(&assistant.ID, &assistant.Ref, &assistant.Scope, &assistant.ProfileRef, &assistant.ProjectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return assistant, errs.ErrNotFound
	}
	if err != nil {
		return assistant, errs.ErrUnavailable
	}
	return assistant, nil
}

func (assistant conversationAssistant) projectConversation(conversation *entity.AssistantConversation) {
	conversation.AssistantScope, conversation.AssistantRef, conversation.AssistantProfileRef = assistant.Scope, assistant.Ref, assistant.ProfileRef
}

func (repository *Repository) expandAssistantContext(ctx context.Context, tx pgx.Tx, current scope, assistant conversationAssistant, projectRef string, descriptor *entity.AssistantContextDescriptor) error {
	var allowed bool
	if err := tx.QueryRow(ctx, queryProjectAssistantContextCreationAllowed, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"project_ref": projectRef, "assistant_scope": assistant.Scope, "context_kind": descriptor.EntityKind,
	}).Scan(&allowed); err != nil {
		return errs.ErrUnavailable
	}
	if allowed && !contains(descriptor.AllowedOperations, "CREATE_PROJECT_ASSISTANT") {
		descriptor.AllowedOperations = append(descriptor.AllowedOperations, "CREATE_PROJECT_ASSISTANT")
	}
	return nil
}

func (repository *Repository) constrainAssistantPlanScope(ctx context.Context, tx pgx.Tx, current *scope, conversationRef string, operations []entity.AssistantPlanOperation) error {
	assistant, err := repository.conversationAssistantTx(ctx, tx, *current, conversationRef)
	if err != nil {
		return err
	}
	if assistant.Scope != "PROJECT" {
		return nil
	}
	current.authorityProjectID = mustProjectID(ctx, tx, current.organizationID, assistant.ProjectRef)
	if current.authorityProjectID == "" {
		return errs.ErrNotFound
	}
	for _, operation := range operations {
		if assistantProjectConfigurationOperation(operation) && assistantString(operation.Parameters, "projectAssistantRef") != assistant.Ref {
			return errs.ErrForbidden
		}
		if !projectAssistantOperation(operation.Type) {
			return errs.ErrForbidden
		}
		if operation.Type == "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" && assistantString(operation.Parameters, "agentRef") != assistant.Ref {
			return errs.ErrForbidden
		}
	}
	return nil
}

func scanProjectAssistant(row pgx.Row) (entity.ProjectAssistantProfile, error) {
	var profile entity.ProjectAssistantProfile
	if err := row.Scan(&profile.Ref, &profile.ProjectRef, &profile.AgentRef, &profile.Name,
		&profile.State, &profile.Version, &profile.CreatedAt, &profile.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
		return entity.ProjectAssistantProfile{}, errs.ErrNotFound
	} else if err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrUnavailable
	}
	return profile, nil
}

func (repository *Repository) GetProjectAssistant(ctx context.Context, principal value.Principal, projectRef string) (entity.ProjectAssistantProfile, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ProjectAssistantProfile{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: projectRef,
	}); err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrNotFound
	}
	profile, err := scanProjectAssistant(tx.QueryRow(ctx, queryProjectAssistantGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": projectRef,
		"authority_project": current.authorityProjectID,
	}))
	if err != nil {
		return entity.ProjectAssistantProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ProjectAssistantProfile{}, errs.ErrUnavailable
	}
	return profile, nil
}

func (repository *Repository) createProjectAssistant(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ProjectAssistantInput)
	if !ok || payload.ProjectRef == "" || strings.TrimSpace(payload.Name) == "" ||
		utf8.RuneCountInString(payload.Name) > 160 || strings.TrimSpace(payload.Purpose) == "" || utf8.RuneCountInString(payload.Purpose) > 2000 ||
		strings.TrimSpace(payload.Instructions) == "" || utf8.RuneCountInString(payload.Instructions) > 32768 {
		return commandOutcome{}, errs.ErrInvalid
	}
	var projectID string
	if err := tx.QueryRow(ctx, queryProjectAssistantLockProject, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": payload.ProjectRef,
		"authority_project": current.authorityProjectID,
	}).Scan(&projectID); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, err := scanProjectAssistant(tx.QueryRow(ctx, queryProjectAssistantGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_ref": payload.ProjectRef,
		"authority_project": current.authorityProjectID,
	}))
	if err == nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return commandOutcome{}, err
	}
	// Создание использует только project-owned baseline. Системные привязки
	// окружения, секретов и grants не входят во вход нового агента.
	created, err := repository.createAgent(ctx, tx, current, command.AgentInput{
		ProjectRef: payload.ProjectRef, Name: payload.Name, Purpose: payload.Purpose,
		RoleDescription: "i18n:PROJECT_ASSISTANT_ROLE", Instructions: payload.Instructions,
		InitialCapabilities: []string{},
	})
	if err != nil {
		return commandOutcome{}, err
	}
	ref, err := newRef("asstp")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	profile, err := scanProjectAssistant(tx.QueryRow(ctx, queryProjectAssistantCreate, pgx.StrictNamedArgs{
		"profile_ref": ref, "organization_id": current.organizationID, "project_id": projectID,
		"agent_ref": created.result.Agent.Ref, "created_by": current.actorID,
	}))
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.emitPlatformEvent(ctx, tx, current, "AGENT_CHANGED", payload.ProjectRef,
		created.result.Agent.Ref, "i18n:PROJECT_ASSISTANT_CREATED"); err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{ProjectAssistant: &profile},
		projectID: projectID, projectRef: payload.ProjectRef, resourceKind: "PROJECT_ASSISTANT",
		resourceRef: ref, summary: "i18n:PROJECT_ASSISTANT_CREATED"}, nil
}
