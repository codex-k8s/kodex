package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_context_projection.sql
var queryAssistantContextProjection string

//go:embed sql/assistant_conversation_context.sql
var queryAssistantConversationContext string

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

func (repository *Repository) authorizeAssistantContextCommand(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	var conversationRef, planRef string
	switch payload := input.Payload.(type) {
	case command.AssistantConversationInput:
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
	return err
}
