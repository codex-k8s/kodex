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

//go:embed sql/assistant_context_projection.sql
var queryAssistantContextProjection string

//go:embed sql/assistant_conversation_context.sql
var queryAssistantConversationContext string

//go:embed sql/assistant_configuration__plan_owner.sql
var queryAssistantConfigurationPlanOwner string

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
			if !assistantProjectConfigurationOperation(operation) && operation.Type != "CREATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" && operation.Type != "UPDATE_SYSTEM_ASSISTANT_ROLE_IMAGE_RECIPE" && operation.Type != "PREPARE_ASSISTANT_RUNTIME_CONFIGURATION" {
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
