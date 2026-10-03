package platform

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/assistant_retry__authority.sql
var queryAssistantRetryAuthority string

//go:embed sql/assistant_retry__source.sql
var queryAssistantRetrySource string

//go:embed sql/assistant_retry__root_node.sql
var queryAssistantRetryRootNode string

func (repository *Repository) authorizeAssistantRetry(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (bool, error) {
	payload, ok := input.Payload.(command.RunCommandInput)
	if !ok || payload.RunRef == "" {
		return true, errs.ErrInvalid
	}
	var targetType, conversationRef string
	err := tx.QueryRow(ctx, queryAssistantRetryAuthority, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID,
		"run_ref": payload.RunRef, "authority_project": current.authorityProjectID,
	}).Scan(&targetType, &conversationRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, errs.ErrNotFound
	}
	if err != nil {
		return true, errs.ErrUnavailable
	}
	if targetType != "SYSTEM_ASSISTANT" {
		return false, nil
	}
	if conversationRef == "" {
		return true, errs.ErrNotFound
	}
	permission, target, err := repository.assistantCommandTarget(ctx, tx, current, conversationRef, "", "agent.launch")
	if err != nil {
		return true, err
	}
	if err := repository.requireAccess(ctx, tx, current, permission, target); err != nil {
		return true, errs.ErrNotFound
	}
	return true, nil
}

func (repository *Repository) retryAssistantRun(ctx context.Context, tx pgx.Tx, current scope, input command.Command, runID string) (commandOutcome, error) {
	var conversationRef, content, attachmentSetRef string
	var descriptor entity.AssistantContextDescriptor
	err := tx.QueryRow(ctx, queryAssistantRetrySource, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "actor_id": current.actorID, "run_id": runID,
	}).Scan(&conversationRef, &content, &attachmentSetRef, &descriptor.Route, &descriptor.EntityKind, &descriptor.EntityRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, fmt.Errorf("read assistant retry source: %w", errs.ErrUnavailable)
	}
	nested := input
	nested.Kind = command.AddAssistantTurn
	nested.Mutation.ExpectedVersion = nil
	nested.Payload = command.AssistantTurnInput{ConversationRef: conversationRef, Content: content,
		AttachmentSetRef: attachmentSetRef, DeliveryMode: "QUEUE", Context: &descriptor}
	outcome, err := repository.addAssistantTurnWithAttachmentPolicy(ctx, tx, current, nested, true)
	if err != nil {
		return commandOutcome{}, err
	}
	if outcome.result.Conversation == nil || len(outcome.result.Conversation.Turns) != 1 {
		return commandOutcome{}, errs.ErrUnavailable
	}
	runRef := outcome.result.Conversation.Turns[0].RunRef
	run, graph, err := repository.readRunGraphTx(ctx, tx, current, runRef)
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.result.Run, outcome.result.Graph = &run, &graph
	outcome.resourceKind, outcome.resourceRef = "RUN", runRef
	return outcome, nil
}
