package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_project_move_pending_plans.sql
var queryAssistantProjectMovePendingPlans string

func (repository *Repository) authorizeAssistantProjectMove(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (assistantArchiveOwner, string, error) {
	payload, ok := input.Payload.(command.AssistantConversationProjectInput)
	if !ok || payload.ConversationRef == "" || payload.ProjectRef == "" || input.Mutation.ExpectedVersion == nil {
		return assistantArchiveOwner{}, "", errs.ErrInvalid
	}
	owner, err := repository.authorizeAssistantArchive(ctx, tx, current, command.Command{
		Mutation: input.Mutation,
		Payload:  command.AssistantConversationArchiveInput{ConversationRef: payload.ConversationRef},
	})
	if err != nil {
		return assistantArchiveOwner{}, "", err
	}
	assistant, err := repository.conversationAssistantTx(ctx, tx, current, payload.ConversationRef)
	if err != nil {
		return assistantArchiveOwner{}, "", err
	}
	if assistant.Scope != "SYSTEM" {
		return assistantArchiveOwner{}, "", errs.ErrConflict
	}
	if owner.conversation.ProjectRef != "" && owner.conversation.ProjectRef != payload.ProjectRef {
		return assistantArchiveOwner{}, "", errs.ErrConflict
	}
	if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: payload.ProjectRef,
	}); err != nil {
		return assistantArchiveOwner{}, "", errs.ErrNotFound
	}
	projectID := mustProjectID(ctx, tx, current.organizationID, payload.ProjectRef)
	if projectID == "" {
		return assistantArchiveOwner{}, "", errs.ErrNotFound
	}
	return owner, projectID, nil
}

func (repository *Repository) moveAssistantConversationToProject(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	owner, projectID, err := repository.authorizeAssistantProjectMove(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	payload := input.Payload.(command.AssistantConversationProjectInput)
	conversation := owner.conversation
	if conversation.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if conversation.State != "ACTIVE" || owner.busy || conversation.ProjectRef != "" {
		return commandOutcome{}, errs.ErrConflict
	}
	var pending bool
	if err := tx.QueryRow(ctx, queryAssistantProjectMovePendingPlans, current.organizationID, conversation.Ref).Scan(&pending); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if pending {
		return commandOutcome{}, errs.ErrConflict
	}
	if _, err := repository.resolveAssistantContext(ctx, tx, current, entity.AssistantContextDescriptor{
		Route: "/projects/" + payload.ProjectRef, EntityKind: "PROJECT", EntityRef: payload.ProjectRef,
	}, payload.ProjectRef); err != nil {
		return commandOutcome{}, err
	}
	if err := repository.promoteAssistantConversationProject(ctx, tx, current, conversation.Ref, projectID, payload.ProjectRef); err != nil {
		return commandOutcome{}, err
	}
	updated, _, err := repository.authorizeAssistantProjectMove(ctx, tx, current, input)
	if err != nil || updated.conversation.ProjectRef != payload.ProjectRef || updated.conversation.Version != conversation.Version+1 {
		return commandOutcome{}, errs.ErrConflict
	}
	return commandOutcome{
		result: command.Result{Conversation: &updated.conversation}, projectID: projectID, projectRef: payload.ProjectRef,
		resourceKind: "ASSISTANT_CONVERSATION", resourceRef: conversation.Ref,
		summary: "i18n:ASSISTANT_CONVERSATION_PROJECT_CHANGED", platformEvent: "SYSTEM_ASSISTANT_CHANGED",
	}, nil
}
