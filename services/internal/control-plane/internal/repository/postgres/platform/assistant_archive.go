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

//go:embed sql/assistant_archive_lock.sql
var queryAssistantArchiveLock string

//go:embed sql/assistant_archive_update.sql
var queryAssistantArchiveUpdate string

type assistantArchiveOwner struct {
	conversation  entity.AssistantConversation
	id, projectID string
	busy          bool
}

func (repository *Repository) authorizeAssistantArchive(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (assistantArchiveOwner, error) {
	var owner assistantArchiveOwner
	payload, ok := input.Payload.(command.AssistantConversationArchiveInput)
	if !ok || payload.ConversationRef == "" || input.Mutation.ExpectedVersion == nil {
		return owner, errs.ErrInvalid
	}
	c := &owner.conversation
	err := tx.QueryRow(ctx, queryAssistantArchiveLock, current.organizationID, payload.ConversationRef, current.actorID).Scan(
		&c.Ref, &c.Title, &c.TitleSource, &c.TitleRevision, &c.ProjectRef, &c.SessionRef, &c.State, &c.Version,
		&c.Context.Route, &c.Context.EntityKind, &c.Context.EntityRef, &c.Context.EntityName, &c.Context.EntityVersion,
		&c.Context.AllowedOperations, &c.CreatedAt, &c.UpdatedAt, &owner.id, &owner.projectID, &owner.busy)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, errs.ErrNotFound
	}
	if err != nil {
		return owner, errs.ErrUnavailable
	}
	if current.authorityProjectID != "" && current.authorityProjectID != owner.projectID {
		return owner, errs.ErrNotFound
	}
	permission, target := "organization.view", organizationTarget(current.organizationRef)
	if c.ProjectRef != "" {
		permission, target = "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: c.ProjectRef}
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return owner, errs.ErrNotFound
	}
	return owner, nil
}

func (repository *Repository) archiveAssistantConversation(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	owner, err := repository.authorizeAssistantArchive(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	c := owner.conversation
	if c.Version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if owner.busy || c.State == "ARCHIVED" {
		return commandOutcome{}, errs.ErrConflict
	}
	if err := tx.QueryRow(ctx, queryAssistantArchiveUpdate, current.organizationID, c.Ref, c.Version).Scan(&c.Version, &c.UpdatedAt); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	c.State = "ARCHIVED"
	return commandOutcome{result: command.Result{Conversation: &c}, projectID: owner.projectID, projectRef: c.ProjectRef,
		resourceKind: "ASSISTANT_CONVERSATION", resourceRef: c.Ref, summary: "i18n:ASSISTANT_CONVERSATION_ARCHIVED", platformEvent: "SYSTEM_ASSISTANT_CHANGED"}, nil
}
